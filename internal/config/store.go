package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mekedron/wolt-cli/internal/domain"
)

const (
	defaultDirName  = ".wolt"
	defaultFileName = ".wolt-config.json"
	envConfigPath   = "WOLT_CONFIG_PATH"
)

var (
	// ErrConfigNotFound is returned when config file does not exist.
	ErrConfigNotFound = errors.New("config file not found")
	// ErrInvalidConfig is returned when config payload is malformed.
	ErrInvalidConfig = errors.New("config file is invalid")
)

// Store loads and writes profile configuration.
type Store struct {
	path string
}

// NewStore creates a store using env overrides or defaults.
func NewStore() (*Store, error) {
	if cfg := os.Getenv(envConfigPath); cfg != "" {
		return &Store{path: cfg}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	return &Store{path: filepath.Join(home, defaultDirName, defaultFileName)}, nil
}

// Path returns current config path.
func (s *Store) Path() string {
	return s.path
}

// Load reads and validates configuration.
func (s *Store) Load(_ context.Context) (domain.Config, error) {
	payload, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.Config{}, ErrConfigNotFound
		}
		return domain.Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg domain.Config
	if err := json.Unmarshal(payload, &cfg); err != nil {
		return domain.Config{}, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	normalized, err := normalizeSingleAccountConfig(cfg)
	if err != nil {
		return domain.Config{}, err
	}
	return normalized, nil
}

func normalizeSingleAccountConfig(cfg domain.Config) (domain.Config, error) {
	if accountHasData(cfg.Account) {
		profile := profileFromAccount(cfg.Account)
		return domain.Config{Account: accountFromProfile(profile), Profiles: []domain.Profile{profile}}, nil
	}
	if len(cfg.Profiles) == 0 {
		return domain.Config{}, fmt.Errorf("%w: profiles is empty", ErrInvalidConfig)
	}
	selected := cfg.Profiles[0]
	for _, profile := range cfg.Profiles {
		if profile.IsDefault {
			selected = profile
			break
		}
	}
	selected.Name = "default"
	selected.IsDefault = true
	return domain.Config{Account: accountFromProfile(selected), Profiles: []domain.Profile{selected}}, nil
}

// Save writes a configuration payload.
func (s *Store) Save(_ context.Context, cfg domain.Config) error {
	normalized, err := normalizeSingleAccountConfig(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	payload, err := json.Marshal(domain.Config{Account: normalized.Account})
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(s.path, payload, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	_ = os.Chmod(s.path, 0o600)
	return nil
}

func accountHasData(account domain.Account) bool {
	return account.WToken != "" ||
		account.WRefreshToken != "" ||
		len(account.Cookies) > 0 ||
		account.WoltAddressID != "" ||
		account.Location.Lat != 0 ||
		account.Location.Lon != 0
}

func profileFromAccount(account domain.Account) domain.Profile {
	return domain.Profile{
		Name:          "default",
		IsDefault:     true,
		Location:      account.Location,
		WToken:        account.WToken,
		WRefreshToken: account.WRefreshToken,
		Cookies:       account.Cookies,
		WoltAddressID: account.WoltAddressID,
	}
}

func accountFromProfile(profile domain.Profile) domain.Account {
	return domain.Account{
		Location:      profile.Location,
		WToken:        profile.WToken,
		WRefreshToken: profile.WRefreshToken,
		Cookies:       profile.Cookies,
		WoltAddressID: profile.WoltAddressID,
	}
}
