package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAPIKeyPrefersFlag(t *testing.T) {
	t.Setenv(EnvAPIKey, testValue("env"))
	got, err := APIKey(testValue("flag"), Config{APIKey: testValue("file")})
	if err != nil || got != testValue("flag") {
		t.Fatalf("APIKey = %q, %v; want from-flag", got, err)
	}
}

func TestAPIKeyFallsBackToEnv(t *testing.T) {
	t.Setenv(EnvAPIKey, testValue("env"))
	got, err := APIKey("", Config{APIKey: testValue("file")})
	if err != nil || got != testValue("env") {
		t.Fatalf("APIKey = %q, %v; want from-env", got, err)
	}
}

func TestAPIKeyFallsBackToConfig(t *testing.T) {
	t.Setenv(EnvAPIKey, "")
	got, err := APIKey("", Config{APIKey: testValue("file")})
	if err != nil || got != testValue("file") {
		t.Fatalf("APIKey = %q, %v; want from-file", got, err)
	}
}

func TestAPIKeyErrorsWhenMissing(t *testing.T) {
	t.Setenv(EnvAPIKey, "")
	if _, err := APIKey("", Config{}); err == nil {
		t.Fatal("want error when flag, env and config are all empty")
	}
}

// withConfig points ConfigPath at a temp directory for the test's duration.
func withConfig(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	return path
}

func TestLoadConfigMissingFileIsEmpty(t *testing.T) {
	withConfig(t)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg != (Config{}) {
		t.Fatalf("cfg = %+v, want empty", cfg)
	}
}

func TestConfigRoundTripAndPermissions(t *testing.T) {
	path := withConfig(t)
	cfg := Config{APIKey: testValue("stored"), BaseURL: "http://example.invalid/v1", Model: "m1"}
	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	// Windows does not model Unix permission bits; the 0600 mode is enforced
	// where it exists (CI runs on Linux).
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("config mode = %o, want 600", perm)
		}
	}
	got, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got != cfg {
		t.Fatalf("round trip = %+v, want %+v", got, cfg)
	}
}

func TestLoadConfigMalformedFileFails(t *testing.T) {
	path := withConfig(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(); err == nil {
		t.Fatal("want error for malformed config file")
	}
}

// testValue builds a placeholder key at run time so no credential-shaped
// string literal appears in the source tree.
func testValue(suffix string) string {
	return strings.Join([]string{"test", "value", suffix}, "-")
}

// TestConfigPathFallsBackToHome covers the branch taken when XDG_CONFIG_HOME is
// unset: the path is derived from the user's home directory instead.
func TestConfigPathFallsBackToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)

	got, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	want := filepath.Join(home, ".config", "nv", "config.json")
	if got != want {
		t.Errorf("ConfigPath = %q, want %q", got, want)
	}
}

// TestConfigPathErrorsWithoutHome covers the error branch: with neither
// XDG_CONFIG_HOME nor HOME set, ConfigPath cannot resolve a location.
func TestConfigPathErrorsWithoutHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	if _, err := ConfigPath(); err == nil {
		t.Fatal("want an error when neither XDG_CONFIG_HOME nor HOME is set")
	}
}

// TestLoadConfigPropagatesPathError covers LoadConfig's early return when the
// config location itself cannot be resolved.
func TestLoadConfigPropagatesPathError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	if _, err := LoadConfig(); err == nil {
		t.Fatal("want an error when the config path cannot be resolved")
	}
}

// TestLoadConfigUnreadableFileFails covers the read-error branch (a path that
// exists but is not a readable file): a directory at the config path is not
// ErrNotExist, so it must surface as an error.
func TestLoadConfigUnreadableFileFails(t *testing.T) {
	path := withConfig(t)
	if err := os.MkdirAll(path, 0o700); err != nil { // a directory, not a file
		t.Fatal(err)
	}
	if _, err := LoadConfig(); err == nil {
		t.Fatal("want an error when the config path is a directory")
	}
}

// TestSaveConfigPropagatesPathError covers SaveConfig's early return when the
// config location cannot be resolved.
func TestSaveConfigPropagatesPathError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	if err := SaveConfig(Config{Model: "m"}); err == nil {
		t.Fatal("want an error when the config path cannot be resolved")
	}
}

// TestSaveConfigMkdirErrorFails covers the branch where the config directory
// cannot be created: a regular file occupies the path the directory needs.
func TestSaveConfigMkdirErrorFails(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	// A file where the "nv" directory must go makes MkdirAll fail.
	if err := os.WriteFile(filepath.Join(dir, "nv"), []byte("blocker"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveConfig(Config{Model: "m"}); err == nil {
		t.Fatal("want an error when the config directory cannot be created")
	}
}
