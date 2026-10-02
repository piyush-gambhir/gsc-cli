package cmd

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/auth"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/config"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/secrets"
)

func TestReadonlyProfilesOfEveryAuthTypeBlockWrites(t *testing.T) {
	for _, kind := range []string{config.AuthOAuth, config.AuthServiceAccount, config.AuthADC} {
		p := config.Profile{Auth: kind, Scopes: []string{auth.ScopeReadonly}}
		if err := requireWriteScope(&creds{profile: &p, profileName: "p"}); err == nil {
			t.Fatalf("%s readonly profile allowed a write", kind)
		}
		if got := (&app{}).profileScopes(p); len(got) != 1 || got[0] != auth.ScopeReadonly {
			t.Fatalf("%s readonly profile minted %v", kind, got)
		}
	}
	full := config.Profile{Auth: config.AuthServiceAccount, Scopes: []string{auth.ScopeFull}}
	if err := requireWriteScope(&creds{profile: &full, profileName: "p"}); err != nil {
		t.Fatalf("full-scope profile blocked: %v", err)
	}
	if got := (&app{readOnly: true}).profileScopes(full); got[0] != auth.ScopeReadonly {
		t.Fatalf("--read-only must narrow the minted scope: %v", got)
	}
}

func TestReplacingAnOAuthProfileDeletesItsLogin(t *testing.T) {
	isolate(t)
	ctx := context.Background()
	store, err := (&app{}).store()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.SaveBlob(ctx, store, secrets.Keychain, "default", auth.Blob{RefreshToken: "old-refresh", Generation: "g1"}); err != nil {
		t.Fatal(err)
	}
	if err := config.Update(ctx, os.Getenv("GSC_CONFIG"), func(c *config.Config) error {
		c.Profiles["default"] = config.Profile{Auth: config.AuthOAuth, TokenStore: config.StoreKeychain, Generation: "g1"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var errOut bytes.Buffer
	a := &app{out: &bytes.Buffer{}, errOut: &errOut}
	if err := a.replaceProfile(ctx, "default", config.Profile{Auth: config.AuthADC, Scopes: []string{auth.ScopeReadonly}, Generation: "g2"}); err != nil {
		t.Fatalf("replace: %v %s", err, errOut.String())
	}
	if _, err := auth.LoadBlob(store, secrets.Keychain, "default"); err == nil {
		t.Fatal("the replaced OAuth refresh token was left in the keychain")
	}
	cfg, _ := config.Load(os.Getenv("GSC_CONFIG"))
	if p := cfg.Profiles["default"]; p.Auth != config.AuthADC || strings.Contains(errOut.String(), "could not be deleted") {
		t.Fatalf("profile after replace: %+v %s", p, errOut.String())
	}
}
