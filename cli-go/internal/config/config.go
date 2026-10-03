// Package config stores named profiles in an XDG YAML file. Profiles hold
// metadata only; credentials live in the OS keychain (internal/secrets) unless
// a profile was created with --insecure-storage.
package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"go.yaml.in/yaml/v3"
)

// Authentication types a profile can use.
const (
	AuthOAuth          = "oauth"
	AuthServiceAccount = "service_account"
	AuthADC            = "adc"
)

// OAuth client sources recorded at login, so refreshes reuse the same client.
const (
	ClientBuiltin = "builtin"
	ClientEnv     = "env"
	ClientCustom  = "custom"
)

// Token stores.
const (
	StoreKeychain = "keychain"
	StoreFile     = "file"
)

type Profile struct {
	Auth string `yaml:"auth"`
	// Account is the signed-in Google email (display only); AccountID is the
	// stable Google subject identifier from the ID token.
	Account   string `yaml:"account,omitempty"`
	AccountID string `yaml:"account_id,omitempty"`
	// Client records which OAuth client issued the refresh token.
	Client   string   `yaml:"client,omitempty"`
	ClientID string   `yaml:"client_id,omitempty"`
	Scopes   []string `yaml:"scopes,omitempty"`
	// TokenStore is where the profile's secret blob lives.
	TokenStore string `yaml:"token_store,omitempty"`
	// KeyFile is a service account JSON key path; Subject is the Workspace
	// user for domain-wide delegation.
	KeyFile string `yaml:"key_file,omitempty"`
	Subject string `yaml:"subject,omitempty"`
	// Impersonate is a service account email impersonated through the IAM
	// Credentials API, using ADC as the base identity.
	Impersonate string `yaml:"impersonate,omitempty"`
	// Site is the default property for site-scoped commands.
	Site string `yaml:"site,omitempty"`
	// Generation changes on every login so a slow refresh in another process
	// cannot overwrite credentials saved by a newer login.
	Generation string `yaml:"generation,omitempty"`
}

type Config struct {
	CurrentProfile string             `yaml:"current_profile,omitempty"`
	Profiles       map[string]Profile `yaml:"profiles"`
}

// DefaultPath is the configuration path when neither GSC_CONFIG nor
// XDG_CONFIG_HOME is set.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "gsc-cli", "config.yaml"), nil
}

// Path returns the config file path: GSC_CONFIG, else $XDG_CONFIG_HOME/gsc-cli/config.yaml.
func Path() (string, error) {
	if path := os.Getenv("GSC_CONFIG"); path != "" {
		return path, nil
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "gsc-cli", "config.yaml"), nil
}

// Dir is the directory holding the config, the opt-in secrets file, and locks.
func Dir() (string, error) {
	p, err := Path()
	if err != nil {
		return "", err
	}
	return filepath.Dir(p), nil
}

func Load(path string) (*Config, error) {
	c := &Config{Profiles: map[string]Profile{}}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	// Do not echo YAML parser errors: malformed lines can contain credentials.
	if err := yaml.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("invalid YAML config at %s", path)
	}
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	return c, nil
}

func Update(ctx context.Context, path string, mutate func(*Config) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	lock := flock.New(path + ".lock")
	lockCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ok, err := lock.TryLockContext(lockCtx, 25*time.Millisecond)
	if err != nil {
		return fmt.Errorf("lock config: %w", err)
	}
	if !ok {
		return fmt.Errorf("config is locked by another process")
	}
	defer lock.Unlock()
	if err := os.Chmod(path+".lock", 0600); err != nil {
		return err
	}
	c, err := Load(path)
	if err != nil {
		return err
	}
	if err := mutate(c); err != nil {
		return err
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return atomicWrite(path, b)
}

// SelectProfile resolves the profile name: flag, then GSC_PROFILE, then the
// saved current profile.
func (c *Config) SelectProfile(flag string) string {
	if flag != "" {
		return flag
	}
	if env := os.Getenv("GSC_PROFILE"); env != "" {
		return env
	}
	return c.CurrentProfile
}

// ValidName rejects names that would be awkward as keychain keys or YAML keys.
func ValidName(name string) error {
	if name == "" || strings.TrimSpace(name) != name || strings.ContainsAny(name, "\r\n\t/\\") {
		return fmt.Errorf("profile name must be nonempty, without surrounding whitespace, slashes, or control characters")
	}
	return nil
}
