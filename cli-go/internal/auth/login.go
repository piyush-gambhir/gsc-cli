package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/oauthflow"
	"golang.org/x/oauth2"
)

type LoginOptions struct {
	Client Client
	// Scopes requested in addition to openid and email.
	Scopes []string
	// Paste switches to the headless flow: the redirected URL is read here.
	Paste       io.Reader
	OpenBrowser func(string) error
	Log         io.Writer
	Timeout     time.Duration
}

type LoginResult struct {
	Token   *oauth2.Token
	Email   string
	Subject string
	Scopes  []string
}

// Login runs the browser (or paste) authorization-code flow with PKCE and
// exchanges the code for tokens.
func (p *Provider) Login(ctx context.Context, o LoginOptions) (*LoginResult, error) {
	scopes := append([]string{"openid", "email"}, o.Scopes...)
	verifier := oauth2.GenerateVerifier()
	cfg := oauth2.Config{ClientID: o.Client.ID, ClientSecret: o.Client.Secret, Endpoint: p.Endpoint, Scopes: scopes}
	res, err := oauthflow.Run(ctx, oauthflow.Options{
		AuthURL: func(redirectURI, state string) string {
			c := cfg
			c.RedirectURL = redirectURI
			return c.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.S256ChallengeOption(verifier),
				oauth2.SetAuthURLParam("prompt", "consent"))
		},
		Paste:       o.Paste,
		OpenBrowser: o.OpenBrowser,
		Log:         o.Log,
		Timeout:     o.Timeout,
	})
	if err != nil {
		return nil, err
	}
	cfg.RedirectURL = res.RedirectURI
	tok, err := cfg.Exchange(p.ctx(ctx), res.Code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, friendlyTokenError(err)
	}
	if tok.RefreshToken == "" {
		return nil, errors.New("Google returned no refresh token; run the login again")
	}
	out := &LoginResult{Token: tok, Scopes: scopes}
	if granted, ok := tok.Extra("scope").(string); ok && granted != "" {
		out.Scopes = strings.Fields(granted)
	}
	if id, ok := tok.Extra("id_token").(string); ok && id != "" {
		if claims, err := ParseIDToken(id); err == nil {
			out.Email, out.Subject = claims.Email, claims.Sub
		}
	}
	if !hasScope(out.Scopes, ScopeFull) && !hasScope(out.Scopes, ScopeReadonly) {
		return nil, errors.New("Search Console access was not granted; run the login again and allow access")
	}
	return out, nil
}

func hasScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want {
			return true
		}
	}
	return false
}

// HasWriteScope reports whether granted scopes allow write methods.
func HasWriteScope(scopes []string) bool { return hasScope(scopes, ScopeFull) }

// Revoke revokes a token at Google. Revoking ends the user's grant for every
// client in the OAuth client's Google Cloud project.
func (p *Provider) Revoke(ctx context.Context, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.RevokeURL, strings.NewReader(url.Values{"token": {token}}.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := p.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("revoke request failed: %s", strings.ReplaceAll(err.Error(), token, "[REDACTED]"))
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
	// 400 invalid_token means it was already revoked or expired: nothing left to revoke.
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusBadRequest {
		return fmt.Errorf("Google revoke endpoint returned HTTP %d", res.StatusCode)
	}
	return nil
}
