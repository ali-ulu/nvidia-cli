package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ali-ulu/nvidia-cli/internal/nim"
)

// ModelsCacheTTL is how long a cached model list is considered fresh. `nv
// models` serves the cache while it is younger than this and refreshes it in
// the background only when it has expired.
const ModelsCacheTTL = 24 * time.Hour

// ModelsCache is the on-disk snapshot of GET /models, so a fresh list does not
// require a network round trip on every invocation.
type ModelsCache struct {
	FetchedAt time.Time   `json:"fetched_at"`
	Models    []nim.Model `json:"models"`
}

// Fresh reports whether the cache is younger than ModelsCacheTTL at now.
func (c ModelsCache) Fresh(now time.Time) bool {
	return !c.FetchedAt.IsZero() && now.Sub(c.FetchedAt) < ModelsCacheTTL
}

// CachePath returns the model cache location, honoring XDG_CACHE_HOME.
func CachePath() (string, error) {
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "nv", "models.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".cache", "nv", "models.json"), nil
}

// LoadModelsCache reads the cache file. A missing file returns ok=false with no
// error; a malformed file is an error.
func LoadModelsCache() (ModelsCache, bool, error) {
	var c ModelsCache
	path, err := CachePath()
	if err != nil {
		return c, false, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, false, nil
	}
	if err != nil {
		return c, false, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, false, fmt.Errorf("parse %s: %w", path, err)
	}
	return c, true, nil
}

// SaveModelsCache writes the cache with 0600 permissions, creating the
// directory with 0700 if needed.
func SaveModelsCache(c ModelsCache) error {
	path, err := CachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode cache: %w", err)
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}
