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

var (
	BuiltinClientID     string
	BuiltinClientSecret string
)

const (
	ScopeFull     = "https://www.googleapis.com/auth/webmasters"
	ScopeReadonly = "https://www.googleapis.com/auth/webmasters.readonly"
	scopeCloud    = "https://www.googleapis.com/auth/cloud-platform"
)

type Provider struct {
	Endpoint  oauth2.Endpoint
	RevokeURL string
	IAMBase   string
	HTTP      *http.Client
}

func DefaultProvider(h *http.Client) *Provider {
	return &Provider{
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

type Client struct {
	ID, Secret, Source string
}

var ErrNoClient = errors.New("this build has no built-in OAuth client. Set GSC_CLIENT_ID and GSC_CLIENT_SECRET to a Desktop OAuth client, use --client-secret-file, or see docs/auth.md")

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

type IDClaims struct {
	Email string `json:"email"`
	Sub   string `json:"sub"`
}

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

type TokenSource struct{ TS oauth2.TokenSource }

func (t TokenSource) AccessToken(context.Context) (string, error) {
	tok, err := t.TS.Token()
	if err != nil {
		return "", friendlyTokenError(err)
	}
	return tok.AccessToken, nil
}

type Static string

func (s Static) AccessToken(context.Context) (string, error) { return string(s), nil }

var credentialLike = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]*)?|ya29\.[A-Za-z0-9_.-]+|1//[A-Za-z0-9_-]+|[A-Za-z0-9_+/=-]{40,}`)

func scrubCredentials(s string) string {
	s = credentialLike.ReplaceAllString(s, "[REDACTED]")
	if r := []rune(s); len(r) > 400 {
		s = string(r[:400]) + "..."
	}
	return s
}

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
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return &CredentialError{"credential error: " + scrubCredentials(err.Error())}
}
