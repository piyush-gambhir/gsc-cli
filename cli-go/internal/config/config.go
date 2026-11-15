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

const (
	AuthOAuth          = "oauth"
	AuthServiceAccount = "service_account"
	AuthADC            = "adc"
)

const (
	ClientBuiltin = "builtin"
	ClientEnv     = "env"
	ClientCustom  = "custom"
)

const (
	StoreKeychain = "keychain"
	StoreFile     = "file"
)

type Profile struct {
	Auth        string   `yaml:"auth"`
	Account     string   `yaml:"account,omitempty"`
	AccountID   string   `yaml:"account_id,omitempty"`
	Client      string   `yaml:"client,omitempty"`
	ClientID    string   `yaml:"client_id,omitempty"`
	Scopes      []string `yaml:"scopes,omitempty"`
	TokenStore  string   `yaml:"token_store,omitempty"`
	KeyFile     string   `yaml:"key_file,omitempty"`
	Subject     string   `yaml:"subject,omitempty"`
	Impersonate string   `yaml:"impersonate,omitempty"`
	Site        string   `yaml:"site,omitempty"`
	Generation  string   `yaml:"generation,omitempty"`
}

type Config struct {
	CurrentProfile string             `yaml:"current_profile,omitempty"`
	Profiles       map[string]Profile `yaml:"profiles"`
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "gsc-cli", "config.yaml"), nil
}

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

func (c *Config) SelectProfile(flag string) string {
	if flag != "" {
		return flag
	}
	if env := os.Getenv("GSC_PROFILE"); env != "" {
		return env
	}
	return c.CurrentProfile
}

func ValidName(name string) error {
	if name == "" || strings.TrimSpace(name) != name || strings.ContainsAny(name, "\r\n\t/\\") {
		return fmt.Errorf("profile name must be nonempty, without surrounding whitespace, slashes, or control characters")
	}
	return nil
}
