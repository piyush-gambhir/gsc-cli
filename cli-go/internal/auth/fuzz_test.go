package auth

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// Fuzz targets for the JSON auth parses: ID token claims from Google's token
// endpoint and OAuth client files. `go test` runs the seeds;
// `go test -run=^$ -fuzz=FuzzParseIDToken ./internal/auth` explores further.

func FuzzParseIDToken(f *testing.F) {
	enc := base64.RawURLEncoding
	f.Add(enc.EncodeToString([]byte(`{"alg":"RS256"}`))+"."+enc.EncodeToString([]byte(`{"email":"me@example.com","sub":"1"}`))+".sig", "me@example.com", "1")
	f.Add("a.b", "", "")
	f.Add("..", "x", "y")
	f.Add("a."+enc.EncodeToString([]byte(`{"email":5}`))+".c", "", "")
	f.Add("a.eyJ9.c", "", "")
	f.Fuzz(func(t *testing.T, tok, email, sub string) {
		_, _ = ParseIDToken(tok) // arbitrary input must not panic

		// A well-formed token round-trips its claims.
		if !utf8.ValidString(email) || !utf8.ValidString(sub) {
			return
		}
		payload, _ := json.Marshal(IDClaims{Email: email, Sub: sub})
		got, err := ParseIDToken("h." + base64.RawURLEncoding.EncodeToString(payload) + ".s")
		if err != nil || got.Email != email || got.Sub != sub {
			t.Fatalf("claims %q %q became %+v (%v)", email, sub, got, err)
		}
	})
}

// FuzzClientFromSecretFile checks the client_secret.json reader: it accepts
// only an "installed" client with both fields, and its errors never echo the
// file's contents (which may hold the secret).
func FuzzClientFromSecretFile(f *testing.F) {
	f.Add([]byte(`{"installed":{"client_id":"id.apps.googleusercontent.com","client_secret":"GOCSPX-secret"}}`))
	f.Add([]byte(`{"web":{"client_id":"id","client_secret":"GOCSPX-secret"}}`))
	f.Add([]byte(`{"installed":{"client_id":"","client_secret":"GOCSPX-secret"}}`))
	f.Add([]byte(`{"installed":null}`))
	f.Add([]byte(`GOCSPX-secret`))
	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "client.json")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		c, err := ClientFromSecretFile(path)
		if err != nil {
			if msg := strings.ReplaceAll(err.Error(), path, ""); len(data) >= 8 && strings.Contains(msg, string(data)) {
				t.Fatalf("error echoes the file: %v", err)
			}
			return
		}
		if c.ID == "" || c.Secret == "" || c.Source != "custom" {
			t.Fatalf("accepted an incomplete client %+v", c)
		}
	})
}
