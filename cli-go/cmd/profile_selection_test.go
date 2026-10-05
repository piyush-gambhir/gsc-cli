package cmd

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/config"
)

// TestLoginProfileSelection pins docs/auth.md: login without --profile saves to
// GSC_PROFILE, then the current profile, and only then "default".
func TestLoginProfileSelection(t *testing.T) {
	key := serviceAccountKey(t)
	for _, tc := range []struct {
		name, flag, env, want string
		seed                  bool
	}{
		{name: "default when nothing saved", want: "default"},
		{name: "saved current profile", seed: true, want: "prod"},
		{name: "environment over current", seed: true, env: "staging", want: "staging"},
		{name: "flag over environment", seed: true, env: "staging", flag: "qa", want: "qa"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			path, err := config.Path()
			if err != nil {
				t.Fatal(err)
			}
			if tc.seed {
				if err := config.Update(context.Background(), path, func(c *config.Config) error {
					c.Profiles["default"] = config.Profile{Auth: config.AuthADC}
					c.Profiles["prod"] = config.Profile{Auth: config.AuthADC}
					c.CurrentProfile = "prod"
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("GSC_PROFILE", tc.env)
			args := []string{"auth", "login", "--service-account", key, "--no-verify", "-o", "json"}
			if tc.flag != "" {
				args = append(args, "--profile", tc.flag)
			}
			r := cli(t, newFake(t), now, "", args...)
			if r.code != 0 || decode(t, r.out)["profile"] != tc.want {
				t.Fatalf("login: %s %s", r.out, r.errOut)
			}
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.CurrentProfile != tc.want || cfg.Profiles[tc.want].Auth != config.AuthServiceAccount {
				t.Fatalf("current %q, profiles %+v", cfg.CurrentProfile, cfg.Profiles)
			}
			for _, other := range []string{"default", "prod"} {
				if tc.seed && other != tc.want && cfg.Profiles[other].Auth != config.AuthADC {
					t.Fatalf("login to %q overwrote profile %q", tc.want, other)
				}
			}
		})
	}
}

func TestLoginWithUnreadableConfigFailsBeforeSigningIn(t *testing.T) {
	isolate(t)
	path, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("profiles: [unterminated\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f := newFake(t)
	for _, args := range [][]string{{"auth", "login"}, {"auth", "login", "--profile", "work"}, {"auth", "login", "--service-account", serviceAccountKey(t)}} {
		r := cli(t, f, now, "", args...)
		if r.code == 0 || !strings.Contains(r.errOut, "invalid YAML config") || f.total() != 0 {
			t.Fatalf("%v: code %d, %d requests, stderr %q", args, r.code, f.total(), r.errOut)
		}
	}
}

func TestLogoutWithoutAProfileSaysWhatToPass(t *testing.T) {
	isolate(t)
	r := cli(t, newFake(t), now, "", "auth", "logout")
	if r.code == 0 || !strings.Contains(r.errOut, "no profile selected") || !strings.Contains(r.errOut, "--profile NAME") {
		t.Fatalf("logout without a profile: %d %q", r.code, r.errOut)
	}
}
