package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWireCacheTargetsDefault(t *testing.T) {
	base := filepath.Join(t.TempDir(), "cache")
	got := wireCacheTargets(nil, base)
	want := map[string]string{
		"discovery-cache":  filepath.Join(base, "wire", "discovery-cache"),
		"loader-artifacts": filepath.Join(base, "wire", "loader-artifacts"),
		"output-cache":     filepath.Join(base, "wire", "output-cache"),
	}
	if len(got) != len(want) {
		t.Fatalf("targets len = %d, want %d", len(got), len(want))
	}
	for _, target := range got {
		if target.path != want[target.name] {
			t.Fatalf("%s path = %q, want %q", target.name, target.path, want[target.name])
		}
	}
}

func TestWireCacheRoot(t *testing.T) {
	base := filepath.Join(t.TempDir(), "cache")
	old := osUserCacheDir
	osUserCacheDir = func() (string, error) { return base, nil }
	defer func() { osUserCacheDir = old }()

	got, err := wireCacheRoot(nil)
	if err != nil {
		t.Fatalf("wireCacheRoot() error = %v", err)
	}
	want := filepath.Join(base, "wire")
	if got != want {
		t.Fatalf("wireCacheRoot() = %q, want %q", got, want)
	}
}

func TestWireCacheTargetsRespectOverrides(t *testing.T) {
	base := filepath.Join(t.TempDir(), "cache")
	env := []string{
		loaderArtifactDirEnv + "=" + filepath.Join(base, "loader"),
		outputCacheDirEnv + "=" + filepath.Join(base, "output"),
	}
	got := wireCacheTargets(env, base)
	want := map[string]string{
		"discovery-cache":  filepath.Join(base, "wire", "discovery-cache"),
		"loader-artifacts": filepath.Join(base, "loader"),
		"output-cache":     filepath.Join(base, "output"),
	}
	for _, target := range got {
		if target.path != want[target.name] {
			t.Fatalf("%s path = %q, want %q", target.name, target.path, want[target.name])
		}
	}
}

func TestClearWireCachesRemovesTargets(t *testing.T) {
	base := filepath.Join(t.TempDir(), "cache")
	env := []string{
		loaderArtifactDirEnv + "=" + filepath.Join(base, "loader"),
		outputCacheDirEnv + "=" + filepath.Join(base, "output"),
	}
	for _, target := range wireCacheTargets(env, base) {
		if err := os.MkdirAll(target.path, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", target.path, err)
		}
		if err := os.WriteFile(filepath.Join(target.path, "marker"), []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", target.path, err)
		}
	}
	old := osUserCacheDir
	osUserCacheDir = func() (string, error) { return base, nil }
	defer func() { osUserCacheDir = old }()

	cleared, err := clearWireCaches(env)
	if err != nil {
		t.Fatalf("clearWireCaches() error = %v", err)
	}
	if len(cleared) != 3 {
		t.Fatalf("cleared len = %d, want 3", len(cleared))
	}
	for _, target := range wireCacheTargets(env, base) {
		if _, err := os.Stat(target.path); !os.IsNotExist(err) {
			t.Fatalf("%s still exists after clear, stat err = %v", target.path, err)
		}
	}
}
