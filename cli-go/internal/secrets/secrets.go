// Package secrets stores credentials in the operating system keychain and
// falls back to an owner-only YAML file when no keychain service exists (for
// example a Linux server without Secret Service). Callers record which backend
// holds each secret so later reads go to the same place.
package secrets

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofrs/flock"
	"github.com/zalando/go-keyring"
	"go.yaml.in/yaml/v3"
)

type Backend string

const (
	Keychain Backend = "keychain"
	File     Backend = "file"
)

var ErrNotFound = errors.New("secret not found")

var errAbandoned = errors.New("keychain call abandoned after its caller timed out")

// Store addresses one CLI's secrets: Service names the keychain entries and
// FilePath is the fallback file (mode 0600). Timeout bounds each keychain call,
// default 10s, so a locked keychain waiting on an unlock prompt cannot hang a
// non-interactive run.
type Store struct {
	Service  string
	FilePath string
	Timeout  time.Duration
}

// keychainMu allows one keychain operation at a time per process: some
// backends (and go-keyring's test mock) are not safe for concurrent use.
var keychainMu sync.Mutex

// keychain runs one keychain operation under the store's timeout.
func (s Store) keychain(op func() error) error {
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	done := make(chan error, 1)
	var abandoned atomic.Bool
	go func() {
		keychainMu.Lock()
		defer keychainMu.Unlock()
		// A call still queued when its caller gave up must not run later (for
		// example after the caller released its profile lock).
		if abandoned.Load() {
			done <- errAbandoned
			return
		}
		done <- op()
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		abandoned.Store(true)
		return fmt.Errorf("OS keychain did not respond within %s (locked or waiting for an unlock prompt)", timeout)
	}
}

// ServiceFor names the keychain service for one configuration file. The default
// configuration keeps the plain base name; any other path (GSC_CONFIG,
// XDG_CONFIG_HOME, a per-environment directory) gets a short hash suffix, so
// profiles with the same name in different configurations never share
// keychain entries.
func ServiceFor(base, configPath, defaultPath string) string {
	if configPath == "" || filepath.Clean(configPath) == filepath.Clean(defaultPath) {
		return base
	}
	abs, err := filepath.Abs(configPath)
	if err != nil {
		abs = configPath
	}
	sum := sha256.Sum256([]byte(filepath.Clean(abs)))
	return fmt.Sprintf("%s (%x)", base, sum[:4])
}

// Saved reports where Save put a secret.
type Saved struct {
	Backend Backend
	// FallbackReason is the keychain error that caused a File fallback, if any.
	FallbackReason error
}

// Save stores value under key. With prefer set to File, or when the keychain
// is unavailable and allowFallback is true, the file backend is used.
func (s Store) Save(ctx context.Context, key, value string, prefer Backend, allowFallback bool) (Saved, error) {
	put := func(m map[string]string) { m[key] = value }
	if prefer == File {
		return Saved{Backend: File}, s.fileUpdate(ctx, put)
	}
	err := s.keychain(func() error { return keyring.Set(s.Service, key, value) })
	if err == nil {
		return Saved{Backend: Keychain}, nil
	}
	if !allowFallback {
		return Saved{}, fmt.Errorf("OS keychain unavailable: %w", err)
	}
	if ferr := s.fileUpdate(ctx, put); ferr != nil {
		return Saved{}, ferr
	}
	return Saved{Backend: File, FallbackReason: err}, nil
}

// Load reads key from the backend that Save reported.
func (s Store) Load(backend Backend, key string) (string, error) {
	switch backend {
	case Keychain:
		var v string
		err := s.keychain(func() (err error) { v, err = keyring.Get(s.Service, key); return err })
		if errors.Is(err, keyring.ErrNotFound) {
			return "", ErrNotFound
		}
		if err != nil {
			return "", fmt.Errorf("read OS keychain: %w", err)
		}
		return v, nil
	case File:
		m, err := s.fileRead()
		if err != nil {
			return "", err
		}
		v, ok := m[key]
		if !ok {
			return "", ErrNotFound
		}
		return v, nil
	default:
		return "", fmt.Errorf("unknown secret backend %q", backend)
	}
}

// Delete removes key from backend. A missing secret is not an error.
func (s Store) Delete(ctx context.Context, backend Backend, key string) error {
	switch backend {
	case Keychain:
		err := s.keychain(func() error { return keyring.Delete(s.Service, key) })
		if err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return fmt.Errorf("delete from OS keychain: %w", err)
		}
		return nil
	case File:
		return s.fileUpdate(ctx, func(m map[string]string) { delete(m, key) })
	default:
		return fmt.Errorf("unknown secret backend %q", backend)
	}
}

func (s Store) fileRead() (map[string]string, error) {
	m := map[string]string{}
	b, err := os.ReadFile(s.FilePath)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read secrets file: %w", err)
	}
	// Never echo parser errors: the file contains credentials.
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("invalid secrets file at %s", s.FilePath)
	}
	if m == nil {
		m = map[string]string{}
	}
	return m, nil
}

func (s Store) fileUpdate(ctx context.Context, mutate func(map[string]string)) error {
	if err := os.MkdirAll(filepath.Dir(s.FilePath), 0700); err != nil {
		return err
	}
	lock := flock.New(s.FilePath + ".lock")
	lockCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ok, err := lock.TryLockContext(lockCtx, 25*time.Millisecond)
	if err != nil {
		return fmt.Errorf("lock secrets file: %w", err)
	}
	if !ok {
		return fmt.Errorf("secrets file is locked by another process")
	}
	defer lock.Unlock()
	if err := os.Chmod(s.FilePath+".lock", 0600); err != nil {
		return err
	}
	m, err := s.fileRead()
	if err != nil {
		return err
	}
	mutate(m)
	b, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	return writeAtomic(s.FilePath, b)
}

func writeAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".secrets-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
