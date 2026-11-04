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
