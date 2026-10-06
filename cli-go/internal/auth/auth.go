// Package auth obtains Google access tokens for every supported credential
// type: browser OAuth (built-in, env-provided, or bring-your-own client),
// service accounts, Application Default Credentials, impersonation, and raw
// access tokens.
package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/secrets"
	"golang.org/x/oauth2"
)

// Built-in OAuth client, injected into release builds with -ldflags -X.
// Empty in source: Google forbids committing client credentials to public
// repositories. Desktop-app secrets are not confidential (PKCE protects the
// flow), so shipping them in the binary follows Google's installed-app model.
var (
	BuiltinClientID     string
	BuiltinClientSecret string
)

const (
	ScopeFull     = "https://www.googleapis.com/auth/webmasters"
	ScopeReadonly = "https://www.googleapis.com/auth/webmasters.readonly"
	scopeCloud    = "https://www.googleapis.com/auth/cloud-platform"
)

// Provider holds Google's endpoints and the HTTP client used to reach them.
// Tests replace the endpoints with local fakes.
type Provider struct {
	Endpoint  oauth2.Endpoint
	RevokeURL string
	IAMBase   string
	HTTP      *http.Client
}

func DefaultProvider(h *http.Client) *Provider {
	return &Provider{
		// AuthStyleInParams: x/oauth2 otherwise probes header auth first and
		// silently retries with a second token request.
		Endpoint: oauth2.Endpoint{
			AuthURL:   "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL:  "https://oauth2.googleapis.com/token",
			AuthStyle: oauth2.AuthStyleInParams,
		},
		RevokeURL: "https://oauth2.googleapis.com/revoke",
		IAMBase:   "https://iamcredentials.googleapis.com",
		HTTP:      h,
	}
}

func (p *Provider) ctx(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, p.HTTP)
}

// Client is an OAuth client identity and where it came from.
type Client struct {
	ID, Secret, Source string
}

// ErrNoClient means neither a built-in client nor GSC_CLIENT_ID is available.
var ErrNoClient = errors.New("this build has no built-in OAuth client. Set GSC_CLIENT_ID and GSC_CLIENT_SECRET to a Desktop OAuth client, use --client-secret-file, or see docs/auth.md")

// ResolveClient picks the env override, else the built-in client.
func ResolveClient() (Client, error) {
	id, secret := os.Getenv("GSC_CLIENT_ID"), os.Getenv("GSC_CLIENT_SECRET")
	if id != "" || secret != "" {
		if id == "" || secret == "" {
			return Client{}, errors.New("set both GSC_CLIENT_ID and GSC_CLIENT_SECRET, or neither")
		}
		return Client{ID: id, Secret: secret, Source: "env"}, nil
	}
	if BuiltinClientID != "" && BuiltinClientSecret != "" {
		return Client{ID: BuiltinClientID, Secret: BuiltinClientSecret, Source: "builtin"}, nil
	}
	return Client{}, ErrNoClient
}

// ClientFromSecretFile reads the JSON Google Cloud Console downloads for a
// Desktop ("installed") OAuth client.
func ClientFromSecretFile(path string) (Client, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Client{}, err
	}
	var f struct {
		Installed *struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		} `json:"installed"`
		Web *json.RawMessage `json:"web"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return Client{}, fmt.Errorf("%s is not a Google OAuth client JSON file", path)
	}
	if f.Installed == nil {
		if f.Web != nil {
			return Client{}, errors.New("that is a Web application client; create a Desktop app OAuth client instead")
		}
		return Client{}, fmt.Errorf("%s has no \"installed\" client section", path)
	}
	if f.Installed.ClientID == "" || f.Installed.ClientSecret == "" {
		return Client{}, fmt.Errorf("%s is missing client_id or client_secret", path)
	}
	return Client{ID: f.Installed.ClientID, Secret: f.Installed.ClientSecret, Source: "custom"}, nil
}

// Blob is what a profile keeps in the secret store.
type Blob struct {
	RefreshToken string    `json:"refresh_token,omitempty"`
	AccessToken  string    `json:"access_token,omitempty"`
	Expiry       time.Time `json:"expiry,omitzero"`
	ClientSecret string    `json:"client_secret,omitempty"`
	Generation   string    `json:"generation,omitempty"`
}

func (b Blob) valid(now time.Time) bool {
	return b.AccessToken != "" && b.Expiry.After(now.Add(60*time.Second))
}

func LoadBlob(s secrets.Store, backend secrets.Backend, key string) (Blob, error) {
	raw, err := s.Load(backend, key)
	if err != nil {
		return Blob{}, err
	}
	var b Blob
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		return Blob{}, fmt.Errorf("stored credentials for profile %q are unreadable; run gsc auth login --profile %s", key, key)
	}
	return b, nil
}

func SaveBlob(ctx context.Context, s secrets.Store, backend secrets.Backend, key string, b Blob) (secrets.Saved, error) {
	raw, err := json.Marshal(b)
	if err != nil {
		return secrets.Saved{}, err
	}
	return s.Save(ctx, key, string(raw), backend, false)
}

// DescribeStoreError turns a keychain failure into a user-facing message that
// distinguishes a missing service, a locked or unresponsive keychain, and a
// missing item.
func DescribeStoreError(err error, profile string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, secrets.ErrNotFound):
		return fmt.Errorf("no saved credentials for profile %q; run gsc auth login --profile %s", profile, profile)
	case strings.Contains(err.Error(), "did not respond"):
		return fmt.Errorf("%w; unlock the keychain and retry, or use GSC_ACCESS_TOKEN", err)
	default:
		return fmt.Errorf("the OS keychain is unavailable (%v). Log in again with --insecure-storage to keep the token in a 0600 file, or set GSC_ACCESS_TOKEN / GSC_CREDENTIALS", err)
	}
}

// IDClaims are the identity fields Login reads from Google's ID token.
type IDClaims struct {
	Email string `json:"email"`
	Sub   string `json:"sub"`
}

// ParseIDToken decodes an ID token payload. The token came straight from
// Google's token endpoint over TLS, so OpenID Connect allows skipping the
// signature check; the claims are only used for display and grant tracking.
func ParseIDToken(tok string) (IDClaims, error) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return IDClaims{}, errors.New("malformed ID token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return IDClaims{}, errors.New("malformed ID token payload")
	}
	var c IDClaims
	if err := json.Unmarshal(payload, &c); err != nil {
		return IDClaims{}, errors.New("malformed ID token claims")
	}
	return c, nil
}

// TokenSource adapts an oauth2.TokenSource to client.TokenSource.
type TokenSource struct{ TS oauth2.TokenSource }

func (t TokenSource) AccessToken(context.Context) (string, error) {
	tok, err := t.TS.Token()
	if err != nil {
		return "", friendlyTokenError(err)
	}
	return tok.AccessToken, nil
}

// Static is a fixed access token from GSC_ACCESS_TOKEN or --access-token.
type Static string

func (s Static) AccessToken(context.Context) (string, error) { return string(s), nil }

// credentialLike matches token-shaped text: JWTs, Google access and refresh
// tokens, and long opaque strings.
var credentialLike = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]*)?|ya29\.[A-Za-z0-9_.-]+|1//[A-Za-z0-9_-]+|[A-Za-z0-9_+/=-]{40,}`)

// scrubCredentials removes token-shaped text and caps length, so errors from
// credential sources (for example an external-account helper echoing its
// response) cannot print a token.
func scrubCredentials(s string) string {
	s = credentialLike.ReplaceAllString(s, "[REDACTED]")
	if r := []rune(s); len(r) > 400 {
		s = string(r[:400]) + "..."
	}
	return s
}

// CredentialError means no token could be obtained: a revoked or expired grant,
// an unusable key, or a failed impersonation. Callers report it as an auth failure.
type CredentialError struct{ msg string }

func (e *CredentialError) Error() string { return e.msg }

func friendlyTokenError(err error) error {
	var re *oauth2.RetrieveError
	if errors.As(err, &re) {
		desc := re.ErrorCode
		if re.ErrorDescription != "" {
			desc += ": " + scrubCredentials(re.ErrorDescription)
		}
		if re.ErrorCode == "invalid_grant" {
			return &CredentialError{fmt.Sprintf("Google rejected the saved credentials (%s). The grant was revoked or expired; log in again", desc)}
		}
		if desc == "" && re.Response != nil {
			desc = re.Response.Status
		}
		return &CredentialError{"token request failed: " + desc}
	}
	// Return bare context errors: a wrapped one could carry credential text.
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return &CredentialError{"credential error: " + scrubCredentials(err.Error())}
}
