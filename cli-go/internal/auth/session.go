package auth

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gofrs/flock"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/config"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/secrets"
	"golang.org/x/oauth2"
)

type Session struct {
	Provider    *Provider
	Store       secrets.Store
	ConfigPath  string
	ProfileName string
	Profile     config.Profile
	ReadOnly    bool
	Now         func() time.Time

	mu     sync.Mutex
	cached Blob
}

func (s *Session) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Session) backend() secrets.Backend {
	if s.Profile.TokenStore == config.StoreFile {
		return secrets.File
	}
	return secrets.Keychain
}

func (s *Session) AccessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached.valid(s.now()) {
		return s.cached.AccessToken, nil
	}
	blob, err := LoadBlob(s.Store, s.backend(), s.ProfileName)
	if err != nil {
		return "", DescribeStoreError(err, s.ProfileName)
	}
	if err := s.sameLogin(blob); err != nil {
		return "", err
	}
	if blob.valid(s.now()) {
		s.cached = blob
		return blob.AccessToken, nil
	}
	unlock, err := s.lock(ctx)
	if err != nil {
		return "", err
	}
	defer unlock()
	if blob, err = LoadBlob(s.Store, s.backend(), s.ProfileName); err != nil {
		return "", DescribeStoreError(err, s.ProfileName)
	}
	if err := s.sameLogin(blob); err != nil {
		return "", err
	}
	if blob.valid(s.now()) {
		s.cached = blob
		return blob.AccessToken, nil
	}
	if blob.RefreshToken == "" {
		return "", fmt.Errorf("profile %q has no refresh token; run gsc auth login --profile %s", s.ProfileName, s.ProfileName)
	}
	client, err := s.client(blob)
	if err != nil {
		return "", err
	}
	cfg := oauth2.Config{ClientID: client.ID, ClientSecret: client.Secret, Endpoint: s.Provider.Endpoint}
	tok, err := cfg.TokenSource(s.Provider.ctx(ctx), &oauth2.Token{RefreshToken: blob.RefreshToken}).Token()
	if err != nil {
		return "", fmt.Errorf("%w. Run: gsc auth login --profile %s", friendlyTokenError(err), s.ProfileName)
	}
	next := blob
	next.AccessToken, next.Expiry = tok.AccessToken, tok.Expiry
	if tok.RefreshToken != "" {
		next.RefreshToken = tok.RefreshToken
	}
	s.cached = next
	if !s.ReadOnly {
		if err := s.persist(ctx, next); err != nil {
			return "", err
		}
	}
	return next.AccessToken, nil
}

func (s *Session) sameLogin(b Blob) error {
	if b.Generation != s.Profile.Generation {
		return fmt.Errorf("profile %q was replaced or logged out while this command ran; run it again", s.ProfileName)
	}
	return nil
}

func (s *Session) client(blob Blob) (Client, error) {
	relogin := fmt.Errorf("the OAuth client this profile logged in with (%s) is not available in this build or environment; run gsc auth login --profile %s", s.Profile.ClientID, s.ProfileName)
	switch s.Profile.Client {
	case config.ClientBuiltin:
		if BuiltinClientID == "" || BuiltinClientID != s.Profile.ClientID {
			return Client{}, relogin
		}
		return Client{ID: BuiltinClientID, Secret: BuiltinClientSecret, Source: "builtin"}, nil
	case config.ClientEnv:
		if os.Getenv("GSC_CLIENT_ID") != s.Profile.ClientID || os.Getenv("GSC_CLIENT_SECRET") == "" {
			return Client{}, relogin
		}
		return Client{ID: s.Profile.ClientID, Secret: os.Getenv("GSC_CLIENT_SECRET"), Source: "env"}, nil
	case config.ClientCustom:
		if blob.ClientSecret == "" {
			return Client{}, relogin
		}
		return Client{ID: s.Profile.ClientID, Secret: blob.ClientSecret, Source: "custom"}, nil
	}
	return Client{}, relogin
}

func (s *Session) persist(ctx context.Context, b Blob) error {
	cfg, err := config.Load(s.ConfigPath)
	if err != nil {
		return err
	}
	current, ok := cfg.Profiles[s.ProfileName]
	if !ok || current.Generation != s.Profile.Generation || b.Generation != s.Profile.Generation {
		return nil
	}
	if _, err := SaveBlob(ctx, s.Store, s.backend(), s.ProfileName, b); err != nil {
		return DescribeStoreError(err, s.ProfileName)
	}
	return nil
}

func (s *Session) lock(ctx context.Context) (func(), error) {
	dir := filepath.Join(filepath.Dir(s.ConfigPath), "locks")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	l := flock.New(filepath.Join(dir, s.ProfileName+".lock"))
	lockCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ok, err := l.TryLockContext(lockCtx, 50*time.Millisecond)
	if err != nil || !ok {
		return nil, fmt.Errorf("another gsc process is refreshing profile %q; try again", s.ProfileName)
	}
	return func() { _ = l.Unlock() }, nil
}

func LockProfile(ctx context.Context, configPath, profile string) (func(), error) {
	s := &Session{ConfigPath: configPath, ProfileName: profile}
	return s.lock(ctx)
}
