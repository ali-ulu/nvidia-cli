package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ali-ulu/nvidia-cli/internal/nim"
)

func TestCachePathHonorsXDGCacheHome(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg")
	got, err := CachePath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/tmp/xdg", "nv", "models.json"); got != want {
		t.Errorf("CachePath = %q, want %q", got, want)
	}
}

func TestCachePathFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	got, err := CachePath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".cache", "nv", "models.json"); got != want {
		t.Errorf("CachePath = %q, want %q", got, want)
	}
}

func TestCachePathFailsWhenHomeUnresolvable(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "")
	if _, err := CachePath(); err == nil {
		t.Error("want an error when the home directory cannot be resolved")
	}
}

func TestLoadModelsCacheMissingIsNotAnError(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	c, ok, err := LoadModelsCache()
	if err != nil {
		t.Fatalf("missing cache should not error: %v", err)
	}
	if ok {
		t.Error("ok = true for a missing cache")
	}
	if len(c.Models) != 0 {
		t.Errorf("models = %v, want empty", c.Models)
	}
}

func TestModelsCacheRoundTrip(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	in := ModelsCache{
		FetchedAt: time.Now().Truncate(time.Second),
		Models:    []nim.Model{{ID: "a/b", OwnedBy: "a"}, {ID: "c/d", OwnedBy: "c"}},
	}
	if err := SaveModelsCache(in); err != nil {
		t.Fatalf("SaveModelsCache: %v", err)
	}
	out, ok, err := LoadModelsCache()
	if err != nil || !ok {
		t.Fatalf("LoadModelsCache: ok=%v err=%v", ok, err)
	}
	if len(out.Models) != 2 || out.Models[1].ID != "c/d" {
		t.Errorf("round-trip models = %+v", out.Models)
	}
	if !out.FetchedAt.Equal(in.FetchedAt) {
		t.Errorf("FetchedAt = %v, want %v", out.FetchedAt, in.FetchedAt)
	}
}

func TestSaveModelsCacheUses0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not modelled on Windows")
	}
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	if err := SaveModelsCache(ModelsCache{Models: []nim.Model{{ID: "a"}}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "nv", "models.json")
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("cache mode = %o, want 600", perm)
	}
}

func TestLoadModelsCacheMalformedIsAnError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	path := filepath.Join(dir, "nv", "models.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadModelsCache(); err == nil {
		t.Error("want a parse error for a malformed cache")
	}
}

func TestFreshRespectsTTL(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name      string
		fetchedAt time.Time
		want      bool
	}{
		{"zero is never fresh", time.Time{}, false},
		{"just now", now, true},
		{"within ttl", now.Add(-ModelsCacheTTL + time.Minute), true},
		{"at ttl", now.Add(-ModelsCacheTTL), false},
		{"past ttl", now.Add(-ModelsCacheTTL - time.Minute), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := ModelsCache{FetchedAt: tc.fetchedAt}
			if got := c.Fresh(now); got != tc.want {
				t.Errorf("Fresh = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCachePathErrorPropagates(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "")
	if _, _, err := LoadModelsCache(); err == nil {
		t.Error("LoadModelsCache: want a CachePath error")
	}
	if err := SaveModelsCache(ModelsCache{}); err == nil {
		t.Error("SaveModelsCache: want a CachePath error")
	}
}

func TestLoadModelsCacheReadError(t *testing.T) {
	// A file where the directory should be makes ReadFile fail with ENOTDIR,
	// which is not ErrNotExist, so the error branch is exercised.
	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", file)
	if _, _, err := LoadModelsCache(); err == nil {
		t.Error("want a read error when the cache path is not a directory")
	}
}

func TestSaveModelsCacheMkdirError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", file)
	if err := SaveModelsCache(ModelsCache{}); err == nil {
		t.Error("want a create-directory error when a parent is a file")
	}
}
