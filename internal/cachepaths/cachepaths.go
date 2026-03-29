package cachepaths

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	BaseDirEnv           = "WIRE_CACHE_DIR"
	LoaderArtifactDirEnv = "WIRE_LOADER_ARTIFACT_DIR"
	DiscoveryCacheDirEnv = "WIRE_DISCOVERY_CACHE_DIR"
	OutputCacheDirEnv    = "WIRE_OUTPUT_CACHE_DIR"
)

var UserCacheDir = os.UserCacheDir

func Root(env []string) (string, error) {
	if dir := envValue(env, BaseDirEnv); dir != "" {
		return filepath.Clean(dir), nil
	}
	base, err := UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "wire"), nil
}

func Dir(env []string, specificEnv, name string) (string, error) {
	if dir := envValue(env, specificEnv); dir != "" {
		return filepath.Clean(dir), nil
	}
	root, err := Root(env)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, name), nil
}

func EnvValueDefault(env []string, key, fallback string) string {
	if value := envValue(env, key); value != "" {
		return value
	}
	return fallback
}

func envValue(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		name, value, ok := strings.Cut(env[i], "=")
		if ok && name == key && value != "" {
			return value
		}
	}
	return ""
}
