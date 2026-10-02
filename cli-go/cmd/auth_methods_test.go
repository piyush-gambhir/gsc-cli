package cmd

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func serviceAccountKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	body, _ := json.Marshal(map[string]string{"type": "service_account", "client_email": "reporter@proj.iam.gserviceaccount.com",
		"private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), "private_key_id": "k1", "token_uri": "https://oauth2.googleapis.com/token"})
	path := filepath.Join(t.TempDir(), "key.json")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestServiceAccountADCAndCredentialsFile(t *testing.T) {
	isolate(t)
	f := newFake(t)
	key := serviceAccountKey(t)
	r := cli(t, f, now, "", "auth", "login", "--profile", "ci", "--service-account", key, "-o", "json")
	if r.code != 0 {
		t.Fatal(r.errOut)
	}
	if m := decode(t, r.out); m["account"] != "reporter@proj.iam.gserviceaccount.com" || m["site"] != "sc-domain:example.com" {
		t.Fatalf("%v", m)
	}
	if !strings.Contains(r.errOut, "Users and permissions") {
		t.Fatalf("missing property-access hint: %s", r.errOut)
	}
	if r = cli(t, f, now, "", "--profile", "ci", "sites", "list", "-o", "json"); r.code != 0 || !strings.Contains(r.out, "sc-domain:example.com") {
		t.Fatalf("service account request: %s", r.errOut)
	}
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", key)
	r = cli(t, f, now, "", "auth", "login", "--profile", "keyless", "--adc", "--impersonate", "target@proj.iam.gserviceaccount.com", "-o", "json")
	if r.code != 0 || f.count("POST iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/target@proj.iam.gserviceaccount.com:generateAccessToken") == 0 {
		t.Fatalf("adc impersonation: %s", r.errOut)
	}
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	r = cli(t, f, now, "", "--credentials", key, "auth", "token", "-o", "json")
	if r.code != 0 || decode(t, r.out)["source"] != "flag:service_account" {
		t.Fatalf("credentials file: %s %s", r.out, r.errOut)
	}
	if r = cli(t, f, now, "", "auth", "login", "--adc", "--service-account", key); r.code == 0 {
		t.Fatal("accepted conflicting login modes")
	}
	if r = cli(t, f, now, "", "auth", "list", "-o", "json"); r.code != 0 || !strings.Contains(r.out, "keyless") || strings.Contains(r.out, "PRIVATE KEY") {
		t.Fatalf("list: %s", r.out)
	}
}

// pasteReader answers the paste prompt with the redirect URL for the state
// printed on stderr, like a user copying it from the browser.
type pasteReader struct {
	errOut *bytes.Buffer
	r      *strings.Reader
}

var stateRE = regexp.MustCompile(`state=([^&\s]+)`)

func (p *pasteReader) Read(b []byte) (int, error) {
	if p.r == nil {
		m := stateRE.FindStringSubmatch(p.errOut.String())
		if m == nil {
			return 0, os.ErrClosed
		}
		state, _ := url.QueryUnescape(m[1])
		p.r = strings.NewReader("http://127.0.0.1:8085/?code=c1&state=" + url.QueryEscape(state) + "\n")
	}
	return p.r.Read(b)
}

func TestHeadlessPasteLogin(t *testing.T) {
	isolate(t)
	f := newFake(t)
	var out, errb bytes.Buffer
	a := newApp(&pasteReader{errOut: &errb}, &out, &errb)
	a.transport, a.now = f, func() time.Time { return now }
	a.terminal = func() bool { return false }
	a.openBrowser = func(string) error { t.Fatal("headless login opened a browser"); return nil }
	if code := a.run(context.Background(), []string{"auth", "login", "--no-browser", "--profile", "ssh", "-o", "json"}); code != 0 {
		t.Fatalf("%s", errb.String())
	}
	if !strings.Contains(errb.String(), "Paste the full URL") || !strings.Contains(out.String(), `"account": "me@example.com"`) {
		t.Fatalf("%s %s", out.String(), errb.String())
	}
}
