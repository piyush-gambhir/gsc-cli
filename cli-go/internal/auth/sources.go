package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// ServiceAccount builds a token source from a service account JSON key file.
// subject, when set, impersonates a Workspace user through domain-wide
// delegation.
func (p *Provider) ServiceAccount(ctx context.Context, keyFile, subject string, scopes []string) (TokenSource, string, error) {
	b, err := os.ReadFile(keyFile)
	if err != nil {
		return TokenSource{}, "", fmt.Errorf("read service account key: %w", err)
	}
	cfg, err := google.JWTConfigFromJSON(b, scopes...)
	if err != nil {
		return TokenSource{}, "", fmt.Errorf("%s is not a service account JSON key", keyFile)
	}
	cfg.Subject = subject
	return TokenSource{oauth2.ReuseTokenSource(nil, cfg.TokenSource(p.ctx(ctx)))}, cfg.Email, nil
}

// allowedCredentialTypes are the Google credential JSON types accepted from
// --credentials / GSC_CREDENTIALS. The type is passed explicitly, as Google
// requires for safe loading of credential configurations.
var allowedCredentialTypes = map[string]google.CredentialsType{
	"service_account":                  google.ServiceAccount,
	"authorized_user":                  google.AuthorizedUser,
	"external_account":                 google.ExternalAccount,
	"external_account_authorized_user": google.ExternalAccountAuthorizedUser,
	"impersonated_service_account":     google.ImpersonatedServiceAccount,
}

// CredentialsFile loads any supported Google credential JSON file.
func (p *Provider) CredentialsFile(ctx context.Context, path string, scopes []string) (TokenSource, string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return TokenSource{}, "", fmt.Errorf("read credentials file: %w", err)
	}
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return TokenSource{}, "", fmt.Errorf("%s is not a Google credentials JSON file", path)
	}
	t, ok := allowedCredentialTypes[head.Type]
	if !ok {
		return TokenSource{}, "", fmt.Errorf("unsupported credential type %q in %s", head.Type, path)
	}
	creds, err := google.CredentialsFromJSONWithType(p.ctx(ctx), b, t, scopes...)
	if err != nil {
		return TokenSource{}, "", fmt.Errorf("load %s credentials: %w", head.Type, err)
	}
	return TokenSource{creds.TokenSource}, head.Type, nil
}

// ADC resolves Application Default Credentials. It is only used when a
// profile or flag asks for it, never as a silent fallback.
func (p *Provider) ADC(ctx context.Context, scopes []string) (TokenSource, error) {
	creds, err := google.FindDefaultCredentials(p.ctx(ctx), scopes...)
	if err != nil {
		return TokenSource{}, fmt.Errorf("Application Default Credentials not found: %w. Run gcloud auth application-default login --client-id-file=CLIENT.json --scopes=%s", err, strings.Join(append([]string{scopeCloud}, scopes...), ","))
	}
	return TokenSource{creds.TokenSource}, nil
}

// Impersonate mints short-lived tokens for target through the IAM Credentials
// API, authenticated by base (normally ADC with the cloud-platform scope).
func (p *Provider) Impersonate(ctx context.Context, base oauth2.TokenSource, target string, scopes []string) TokenSource {
	return TokenSource{oauth2.ReuseTokenSource(nil, &impersonator{p: p, ctx: ctx, base: base, target: target, scopes: scopes})}
}

type impersonator struct {
	p      *Provider
	ctx    context.Context
	base   oauth2.TokenSource
	target string
	scopes []string
}

func (i *impersonator) Token() (*oauth2.Token, error) {
	baseTok, err := i.base.Token()
	if err != nil {
		return nil, friendlyTokenError(err)
	}
	body, _ := json.Marshal(map[string]any{"scope": i.scopes, "lifetime": "3600s"})
	endpoint := strings.TrimRight(i.p.IAMBase, "/") + "/v1/projects/-/serviceAccounts/" + url.PathEscape(i.target) + ":generateAccessToken"
	req, err := http.NewRequestWithContext(i.ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+baseTok.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := i.p.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("impersonation request failed: %s", strings.ReplaceAll(err.Error(), baseTok.AccessToken, "[REDACTED]"))
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		var env struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(b, &env)
		msg := env.Error.Message
		if msg == "" {
			msg = http.StatusText(res.StatusCode)
		}
		return nil, fmt.Errorf("impersonating %s failed (HTTP %d): %s. The base identity needs roles/iam.serviceAccountTokenCreator on that service account", i.target, res.StatusCode, msg)
	}
	var out struct {
		AccessToken string    `json:"accessToken"`
		ExpireTime  time.Time `json:"expireTime"`
	}
	if err := json.Unmarshal(b, &out); err != nil || out.AccessToken == "" {
		return nil, fmt.Errorf("IAM Credentials returned no access token")
	}
	return &oauth2.Token{AccessToken: out.AccessToken, Expiry: out.ExpireTime, TokenType: "Bearer"}, nil
}

// BaseForImpersonation is the scope a base identity needs to call IAM Credentials.
func BaseForImpersonation() []string { return []string{scopeCloud} }
