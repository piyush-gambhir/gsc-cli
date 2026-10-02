package secrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

func newStore(t *testing.T) Store {
	t.Helper()
	return Store{Service: "secrets-test", FilePath: filepath.Join(t.TempDir(), "secrets.yaml")}
}

func TestKeychainRoundTrip(t *testing.T) {
	keyring.MockInit()
	s, ctx := newStore(t), context.Background()
	saved, err := s.Save(ctx, "default", "refresh-1", Keychain, true)
	if err != nil || saved.Backend != Keychain || saved.FallbackReason != nil {
		t.Fatalf("save: %+v %v", saved, err)
	}
	if v, err := s.Load(Keychain, "default"); err != nil || v != "refresh-1" {
		t.Fatalf("load: %q %v", v, err)
	}
	if _, err := os.Stat(s.FilePath); !os.IsNotExist(err) {
		t.Fatal("keychain save wrote the fallback file")
	}
	if err := s.Delete(ctx, Keychain, "default"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(Keychain, "default"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
	if err := s.Delete(ctx, Keychain, "default"); err != nil {
		t.Fatalf("deleting a missing secret: %v", err)
	}
}

func TestFallbackWhenKeychainFails(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service"))
	s, ctx := newStore(t), context.Background()
	if _, err := s.Save(ctx, "default", "v", Keychain, false); err == nil {
		t.Fatal("save without fallback succeeded on a broken keychain")
	}
	saved, err := s.Save(ctx, "default", "refresh-2", Keychain, true)
	if err != nil || saved.Backend != File || saved.FallbackReason == nil {
		t.Fatalf("fallback: %+v %v", saved, err)
	}
	if v, err := s.Load(File, "default"); err != nil || v != "refresh-2" {
		t.Fatalf("load: %q %v", v, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(s.FilePath)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("file mode: %v %v", info.Mode(), err)
		}
	}
}

func TestFileBackendKeepsOtherKeys(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	for _, k := range []string{"a", "b"} {
		if _, err := s.Save(ctx, k, "secret-"+k, File, false); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Delete(ctx, File, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(File, "a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted key: %v", err)
	}
	if v, err := s.Load(File, "b"); err != nil || v != "secret-b" {
		t.Fatalf("kept key: %q %v", v, err)
	}
}

func TestCorruptFileDoesNotLeakContents(t *testing.T) {
	s := newStore(t)
	if err := os.WriteFile(s.FilePath, []byte("token: [unclosed super-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := s.Load(File, "token")
	if err == nil || strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("corrupt file error: %v", err)
	}
}

func TestKeychainTimeout(t *testing.T) {
	keyring.MockInit()
	s := newStore(t)
	s.Timeout = 50 * time.Millisecond
	release := make(chan struct{})
	block := func() error { <-release; return nil }
	if err := s.keychain(block); err == nil || !strings.Contains(err.Error(), "did not respond") {
		t.Fatalf("timeout: %v", err)
	}
	// The hung call still holds the keychain lock; once it returns, later calls work.
	close(release)
	s.Timeout = time.Second
	saved, err := s.Save(context.Background(), "k", "v", Keychain, false)
	if err != nil || saved.Backend != Keychain {
		t.Fatalf("fast keychain after timeout test: %+v %v", saved, err)
	}
}

func TestServiceForSeparatesConfigurations(t *testing.T) {
	def := filepath.Join(t.TempDir(), "default", "config.yaml")
	if got := ServiceFor("gsc-cli", def, def); got != "gsc-cli" {
		t.Fatalf("default config: %q", got)
	}
	a := ServiceFor("gsc-cli", "/env/production/gsc-cli/config.yaml", def)
	b := ServiceFor("gsc-cli", "/env/staging/gsc-cli/config.yaml", def)
	if a == b || a == "gsc-cli" || !strings.HasPrefix(a, "gsc-cli (") {
		t.Fatalf("per-environment services must differ: %q %q", a, b)
	}
	if a != ServiceFor("gsc-cli", "/env/production/gsc-cli/../gsc-cli/config.yaml", def) {
		t.Fatal("equivalent paths must map to the same service")
	}
}

func TestQueuedKeychainCallsDoNotRunAfterTimeout(t *testing.T) {
	keyring.MockInit()
	s := newStore(t)
	s.Timeout = 50 * time.Millisecond
	release := make(chan struct{})
	// Hold the keychain with a slow call, then queue a second one that times out.
	go func() { _ = s.keychain(func() error { <-release; return nil }) }()
	time.Sleep(10 * time.Millisecond)
	ran := make(chan struct{}, 1)
	if err := s.keychain(func() error { ran <- struct{}{}; return nil }); err == nil {
		t.Fatal("queued call did not time out")
	}
	close(release)
	time.Sleep(50 * time.Millisecond)
	select {
	case <-ran:
		t.Fatal("a timed-out queued call ran after its caller gave up")
	default:
	}
}
