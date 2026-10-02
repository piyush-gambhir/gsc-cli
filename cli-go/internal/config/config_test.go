package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestSelectProfilePrecedence(t *testing.T) {
	c := &Config{CurrentProfile: "saved", Profiles: map[string]Profile{}}
	t.Setenv("GSC_PROFILE", "env")
	if got := c.SelectProfile("flag"); got != "flag" {
		t.Fatal(got)
	}
	if got := c.SelectProfile(""); got != "env" {
		t.Fatal(got)
	}
	t.Setenv("GSC_PROFILE", "")
	if got := c.SelectProfile(""); got != "saved" {
		t.Fatal(got)
	}
}

func TestPathRespectsOverrides(t *testing.T) {
	t.Setenv("GSC_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	if p, _ := Path(); p != filepath.Join("/tmp/xdg", "gsc-cli", "config.yaml") {
		t.Fatal(p)
	}
	t.Setenv("GSC_CONFIG", "/tmp/custom.yaml")
	if p, _ := Path(); p != "/tmp/custom.yaml" {
		t.Fatal(p)
	}
}

func TestValidName(t *testing.T) {
	for _, bad := range []string{"", " x", "a/b", "a\nb", `a\b`} {
		if ValidName(bad) == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	if ValidName("work-2") != nil {
		t.Fatal("rejected a good name")
	}
}

func TestAtomicConcurrentUpdatesAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := Update(context.Background(), path, func(c *Config) error {
				c.Profiles[fmt.Sprint(i)] = Profile{Auth: AuthOAuth, Site: "sc-domain:example.com"}
				return nil
			}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	c, err := Load(path)
	if err != nil || len(c.Profiles) != 10 {
		t.Fatalf("lost updates: %v %d", err, len(c.Profiles))
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0600 {
			t.Fatalf("permissions %o", info.Mode().Perm())
		}
	}
	if err := Update(context.Background(), path, func(c *Config) error { c.Profiles = map[string]Profile{}; return fmt.Errorf("abort") }); err == nil {
		t.Fatal("expected abort")
	}
	if c, _ := Load(path); len(c.Profiles) != 10 {
		t.Fatal("failed transaction modified config")
	}
}

func TestMalformedConfigDoesNotEchoContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("profiles: [refresh-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || strings.Contains(err.Error(), "refresh-secret") {
		t.Fatalf("%v", err)
	}
}
