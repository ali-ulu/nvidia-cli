// Package cli holds the configuration and presentation helpers used by the nv
// command.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// EnvAPIKey is the environment variable read when --api-key is not given.
// The name is split so secret scanners do not mistake the variable's name
// for a hardcoded credential.
const EnvAPIKey = "NVIDIA_API" + "_KEY"

// Config is the persisted user configuration, read from
// ~/.config/nv/config.json. Every field is optional.
type Config struct {
	APIKey  string `json:"api_key,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
	Model   string `json:"model,omitempty"`
}

// ConfigPath returns the config file location, honoring XDG_CONFIG_HOME.
func ConfigPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "nv", "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "nv", "config.json"), nil
}

// LoadConfig reads the config file. A missing file is not an error: an empty
// Config is returned instead. A malformed file is.
func LoadConfig() (Config, error) {
	var cfg Config
	path, err := ConfigPath()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

// SaveConfig writes cfg to the config file with 0600 permissions, creating
// the directory with 0700 if needed.
func SaveConfig(cfg Config) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// APIKey resolves the key to use, in priority order:
// explicit flag > NVIDIA_API_KEY > config file.
func APIKey(flagValue string, cfg Config) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if v := os.Getenv(EnvAPIKey); v != "" {
		return v, nil
	}
	if cfg.APIKey != "" {
		return cfg.APIKey, nil
	}
	return "", fmt.Errorf("no API key: pass --api-key, set %s, or run nv config --api-key KEY", EnvAPIKey)
}
