package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/config"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/secrets"
	"github.com/zalando/go-keyring"
	"golang.org/x/oauth2"
)

func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}

func idToken(email, sub string) string {
	enc := base64.RawURLEncoding
	payload, _ := json.Marshal(map[string]string{"email": email, "sub": sub})
	return enc.EncodeToString([]byte(`{"alg":"RS256"}`)) + "." + enc.EncodeToString(payload) + ".sig"
}

// fakeGoogle is a token, revoke, and IAM endpoint with call counting.
type fakeGoogle struct {
	*httptest.Server
	tokenCalls, revokeCalls atomic.Int32
	refreshStatus           int
	lastForm                url.Values
	mu                      sync.Mutex
	delay                   time.Duration
}

func newFake(t *testing.T) *fakeGoogle {
	f := &fakeGoogle{refreshStatus: 200}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.lastForm = r.PostForm
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			f.tokenCalls.Add(1)
			time.Sleep(f.delay)
			if r.Header.Get("Authorization") != "" {
				t.Error("client credentials sent in a header; AuthStyleInParams expected")
			}
			switch r.PostForm.Get("grant_type") {
			case "authorization_code":
				if r.PostForm.Get("code_verifier") == "" || r.PostForm.Get("client_secret") == "" {
					w.WriteHeader(400)
					fmt.Fprint(w, `{"error":"invalid_request"}`)
					return
				}
				fmt.Fprintf(w, `{"access_token":"at-1","refresh_token":"rt-1","expires_in":3600,"token_type":"Bearer","scope":"openid email %s","id_token":%q}`, ScopeFull, idToken("me@example.com", "sub-1"))
			case "refresh_token":
				if f.refreshStatus != 200 {
					w.WriteHeader(f.refreshStatus)
					fmt.Fprint(w, `{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`)
					return
				}
				fmt.Fprintf(w, `{"access_token":"at-%d","expires_in":3600,"token_type":"Bearer"}`, f.tokenCalls.Load()+1)
			case "urn:ietf:params:oauth:grant-type:jwt-bearer":
				fmt.Fprint(w, `{"access_token":"sa-token","expires_in":3600,"token_type":"Bearer"}`)
			default:
				w.WriteHeader(400)
			}
		case "/revoke":
			f.revokeCalls.Add(1)
		case "/v1/projects/-/serviceAccounts/target@p.iam.gserviceaccount.com:generateAccessToken":
			if r.Header.Get("Authorization") != "Bearer base-token" {
				w.WriteHeader(403)
				fmt.Fprint(w, `{"error":{"message":"denied"}}`)
				return
			}
			fmt.Fprint(w, `{"accessToken":"impersonated","expireTime":"2099-01-01T00:00:00Z"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeGoogle) provider() *Provider {
	return &Provider{Endpoint: oauth2.Endpoint{AuthURL: f.URL + "/auth", TokenURL: f.URL + "/token", AuthStyle: oauth2.AuthStyleInParams},
		RevokeURL: f.URL + "/revoke", IAMBase: f.URL, HTTP: f.Client()}
}

// browser follows the consent URL straight to the loopback redirect.
func browser(t *testing.T) func(string) error {
	return func(u string) error {
		p, _ := url.Parse(u)
		q := p.Query()
		if q.Get("code_challenge_method") != "S256" || q.Get("access_type") != "offline" || q.Get("prompt") != "consent" || !strings.Contains(q.Get("scope"), "openid") {
			t.Errorf("consent URL missing parameters: %s", u)
		}
		go func() {
			res, err := http.Get(q.Get("redirect_uri") + "?code=c1&state=" + url.QueryEscape(q.Get("state")))
			if err == nil {
				res.Body.Close()
			}
		}()
		return nil
	}
}

func TestLoginFlow(t *testing.T) {
	f := newFake(t)
	res, err := f.provider().Login(context.Background(), LoginOptions{Client: Client{ID: "cid", Secret: "csecret", Source: "builtin"}, Scopes: []string{ScopeFull}, OpenBrowser: browser(t), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if res.Token.RefreshToken != "rt-1" || res.Email != "me@example.com" || res.Subject != "sub-1" || !HasWriteScope(res.Scopes) {
		t.Fatalf("%+v", res)
	}
	if f.lastForm.Get("redirect_uri") == "" || !strings.HasPrefix(f.lastForm.Get("redirect_uri"), "http://127.0.0.1:") {
		t.Fatalf("redirect_uri not sent: %v", f.lastForm)
	}
}

func TestFailedTokenCallIsSingleRequest(t *testing.T) {
	f := newFake(t)
	f.refreshStatus = 400
	cfg := oauth2.Config{ClientID: "cid", ClientSecret: "s", Endpoint: f.provider().Endpoint}
	_, err := cfg.TokenSource(f.provider().ctx(context.Background()), &oauth2.Token{RefreshToken: "rt"}).Token()
	if err == nil || f.tokenCalls.Load() != 1 {
		t.Fatalf("calls=%d err=%v", f.tokenCalls.Load(), err)
	}
	if got := friendlyTokenError(err).Error(); !strings.Contains(got, "revoked or expired") {
		t.Fatal(got)
	}
}

type env struct {
	store secrets.Store
	path  string
}

func newEnv(t *testing.T, p config.Profile, blob Blob) env {
	dir := t.TempDir()
	e := env{store: secrets.Store{Service: "gsc-cli-test-" + t.Name(), FilePath: filepath.Join(dir, "secrets.yaml")}, path: filepath.Join(dir, "config.yaml")}
	if err := config.Update(context.Background(), e.path, func(c *config.Config) error { c.Profiles["default"] = p; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveBlob(context.Background(), e.store, secrets.Keychain, "default", blob); err != nil {
		t.Fatal(err)
	}
	return e
}

func withBuiltin(t *testing.T, id, secret string) {
	oldID, oldSecret := BuiltinClientID, BuiltinClientSecret
	BuiltinClientID, BuiltinClientSecret = id, secret
	t.Cleanup(func() { BuiltinClientID, BuiltinClientSecret = oldID, oldSecret })
}

func TestSessionRefreshPersistsAndRespectsReadOnly(t *testing.T) {
	withBuiltin(t, "cid", "csecret")
	f := newFake(t)
	p := config.Profile{Auth: config.AuthOAuth, Client: config.ClientBuiltin, ClientID: "cid", TokenStore: config.StoreKeychain, Generation: "g1"}
	e := newEnv(t, p, Blob{RefreshToken: "rt-1", AccessToken: "old", Expiry: time.Now().Add(-time.Hour), Generation: "g1"})
	ro := &Session{Provider: f.provider(), Store: e.store, ConfigPath: e.path, ProfileName: "default", Profile: p, ReadOnly: true}
	tok, err := ro.AccessToken(context.Background())
	if err != nil || tok == "old" {
		t.Fatalf("%q %v", tok, err)
	}
	if b, _ := LoadBlob(e.store, secrets.Keychain, "default"); b.AccessToken != "old" {
		t.Fatal("read-only session persisted a refreshed token")
	}
	s := &Session{Provider: f.provider(), Store: e.store, ConfigPath: e.path, ProfileName: "default", Profile: p}
	tok, err = s.AccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := LoadBlob(e.store, secrets.Keychain, "default")
	if b.AccessToken != tok || b.RefreshToken != "rt-1" {
		t.Fatalf("refresh not persisted or refresh token lost: %+v", b)
	}
	calls := f.tokenCalls.Load()
	if _, err := s.AccessToken(context.Background()); err != nil || f.tokenCalls.Load() != calls {
		t.Fatal("cached token was not reused")
	}
}

func TestConcurrentRefreshHappensOnce(t *testing.T) {
	withBuiltin(t, "cid", "csecret")
	f := newFake(t)
	f.delay = 150 * time.Millisecond
	p := config.Profile{Auth: config.AuthOAuth, Client: config.ClientBuiltin, ClientID: "cid", Generation: "g1"}
	e := newEnv(t, p, Blob{RefreshToken: "rt-1", Generation: "g1"})
	var wg sync.WaitGroup
	tokens := make([]string, 2)
	for i := range tokens {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := &Session{Provider: f.provider(), Store: e.store, ConfigPath: e.path, ProfileName: "default", Profile: p}
			tok, err := s.AccessToken(context.Background())
			if err != nil {
				t.Error(err)
			}
			tokens[i] = tok
		}(i)
	}
	wg.Wait()
	if f.tokenCalls.Load() != 1 || tokens[0] != tokens[1] {
		t.Fatalf("refreshes=%d tokens=%v", f.tokenCalls.Load(), tokens)
	}
}

func TestSessionGuards(t *testing.T) {
	f := newFake(t)
	p := config.Profile{Auth: config.AuthOAuth, Client: config.ClientBuiltin, ClientID: "old-client", Generation: "g1"}
	e := newEnv(t, p, Blob{RefreshToken: "rt", Generation: "g1"})
	withBuiltin(t, "new-client", "secret")
	s := &Session{Provider: f.provider(), Store: e.store, ConfigPath: e.path, ProfileName: "default", Profile: p}
	if _, err := s.AccessToken(context.Background()); err == nil || !strings.Contains(err.Error(), "gsc auth login --profile default") {
		t.Fatalf("client mismatch: %v", err)
	}
	withBuiltin(t, "old-client", "secret")
	f.refreshStatus = 400
	if _, err := s.AccessToken(context.Background()); err == nil || !strings.Contains(err.Error(), "revoked or expired") || !strings.Contains(err.Error(), "gsc auth login") {
		t.Fatalf("invalid_grant: %v", err)
	}
	f.refreshStatus = 200
	// A newer login replaced the profile while this session refreshed: nothing is written.
	_ = config.Update(context.Background(), e.path, func(c *config.Config) error {
		q := c.Profiles["default"]
		q.Generation = "g2"
		c.Profiles["default"] = q
		return nil
	})
	if _, err := s.AccessToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b, _ := LoadBlob(e.store, secrets.Keychain, "default"); b.AccessToken != "" {
		t.Fatal("stale session overwrote a newer login")
	}
	custom := config.Profile{Auth: config.AuthOAuth, Client: config.ClientCustom, ClientID: "byo", Generation: "g1"}
	e2 := newEnv(t, custom, Blob{RefreshToken: "rt", ClientSecret: "byo-secret", Generation: "g1"})
	s2 := &Session{Provider: f.provider(), Store: e2.store, ConfigPath: e2.path, ProfileName: "default", Profile: custom}
	if _, err := s2.AccessToken(context.Background()); err != nil || f.lastForm.Get("client_secret") != "byo-secret" || f.lastForm.Get("client_id") != "byo" {
		t.Fatalf("custom client binding: %v %v", err, f.lastForm)
	}
}

func TestClientResolution(t *testing.T) {
	withBuiltin(t, "", "")
	t.Setenv("GSC_CLIENT_ID", "")
	t.Setenv("GSC_CLIENT_SECRET", "")
	if _, err := ResolveClient(); err != ErrNoClient {
		t.Fatalf("expected ErrNoClient, got %v", err)
	}
	withBuiltin(t, "b", "bs")
	if c, _ := ResolveClient(); c.Source != "builtin" {
		t.Fatal(c)
	}
	t.Setenv("GSC_CLIENT_ID", "e")
	if _, err := ResolveClient(); err == nil {
		t.Fatal("accepted an id without a secret")
	}
	t.Setenv("GSC_CLIENT_SECRET", "es")
	if c, _ := ResolveClient(); c.Source != "env" || c.ID != "e" {
		t.Fatal(c)
	}
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		_ = os.WriteFile(p, []byte(body), 0600)
		return p
	}
	if c, err := ClientFromSecretFile(write("ok.json", `{"installed":{"client_id":"i","client_secret":"s"}}`)); err != nil || c.Source != "custom" {
		t.Fatal(c, err)
	}
	if _, err := ClientFromSecretFile(write("web.json", `{"web":{"client_id":"i"}}`)); err == nil || !strings.Contains(err.Error(), "Desktop") {
		t.Fatal(err)
	}
	if _, err := ClientFromSecretFile(write("bad.json", `nope`)); err == nil {
		t.Fatal("accepted garbage")
	}
}

func TestServiceAccountJWT(t *testing.T) {
	f := newFake(t)
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: must(x509.MarshalPKCS8PrivateKey(key))})
	keyJSON, _ := json.Marshal(map[string]string{"type": "service_account", "client_email": "sa@p.iam.gserviceaccount.com", "private_key": string(pemKey), "private_key_id": "k1", "token_uri": f.URL + "/token"})
	path := filepath.Join(t.TempDir(), "key.json")
	_ = os.WriteFile(path, keyJSON, 0600)
	ts, email, err := f.provider().ServiceAccount(context.Background(), path, "user@corp.example", []string{ScopeReadonly})
	if err != nil || email != "sa@p.iam.gserviceaccount.com" {
		t.Fatal(email, err)
	}
	if tok, err := ts.AccessToken(context.Background()); err != nil || tok != "sa-token" {
		t.Fatal(tok, err)
	}
	parts := strings.Split(f.lastForm.Get("assertion"), ".")
	if len(parts) != 3 {
		t.Fatal("no JWT assertion")
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		t.Fatal("assertion not signed by the key:", err)
	}
	claims, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if !strings.Contains(string(claims), `"sub":"user@corp.example"`) || !strings.Contains(string(claims), "webmasters.readonly") {
		t.Fatalf("claims: %s", claims)
	}
	if _, _, err := f.provider().CredentialsFile(context.Background(), path, []string{ScopeReadonly}); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	_ = os.WriteFile(bad, []byte(`{"type":"gdch_service_account"}`), 0600)
	if _, _, err := f.provider().CredentialsFile(context.Background(), bad, nil); err == nil || !strings.Contains(err.Error(), "unsupported credential type") {
		t.Fatal(err)
	}
}

func must(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}

func TestImpersonateAndRevoke(t *testing.T) {
	f := newFake(t)
	p := f.provider()
	ts := p.Impersonate(context.Background(), oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "base-token"}), "target@p.iam.gserviceaccount.com", []string{ScopeFull})
	if tok, err := ts.AccessToken(context.Background()); err != nil || tok != "impersonated" {
		t.Fatal(tok, err)
	}
	denied := p.Impersonate(context.Background(), oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "wrong"}), "target@p.iam.gserviceaccount.com", nil)
	if _, err := denied.AccessToken(context.Background()); err == nil || !strings.Contains(err.Error(), "serviceAccountTokenCreator") {
		t.Fatal(err)
	}
	if err := p.Revoke(context.Background(), "rt-1"); err != nil || f.revokeCalls.Load() != 1 || f.lastForm.Get("token") != "rt-1" {
		t.Fatal(err)
	}
	if c, err := ParseIDToken(idToken("a@b.c", "s")); err != nil || c.Email != "a@b.c" {
		t.Fatal(c, err)
	}
	if _, err := ParseIDToken("nope"); err == nil {
		t.Fatal("accepted malformed token")
	}
}

func TestSessionRejectsTokenFromAnotherLogin(t *testing.T) {
	f := newFake(t)
	p := config.Profile{Auth: config.AuthOAuth, Client: config.ClientBuiltin, ClientID: "cid", Generation: "g1"}
	// The stored blob belongs to a newer login (g2) than the profile this command loaded (g1).
	e := newEnv(t, p, Blob{RefreshToken: "rt-b", AccessToken: "access-b", Expiry: time.Now().Add(time.Hour), Generation: "g2"})
	withBuiltin(t, "cid", "secret")
	s := &Session{Provider: f.provider(), Store: e.store, ConfigPath: e.path, ProfileName: "default", Profile: p}
	tok, err := s.AccessToken(context.Background())
	if err == nil || tok != "" || !strings.Contains(err.Error(), "replaced or logged out") {
		t.Fatalf("token from another login was used: %q %v", tok, err)
	}
}
