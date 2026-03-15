package wire

import (
	"path/filepath"
	"testing"
)

func TestRunScopedKeysIgnoreEquivalentWorkingDirectories(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.22\n")
	wireDir := filepath.Join(root, "wire")
	writeFile(t, filepath.Join(wireDir, "wire.go"), "package wire\n")

	env := []string{"GOOS=darwin"}
	opts := &GenerateOptions{Tags: "wireinject", PrefixOutputFile: "gen_"}

	rootKey := manifestKey(root, env, []string{"./wire"}, opts)
	subdirKey := manifestKey(wireDir, env, []string{"."}, opts)
	if rootKey != subdirKey {
		t.Fatalf("manifestKey mismatch: root=%q subdir=%q", rootKey, subdirKey)
	}

	rootIncrementalKey := incrementalManifestSelectorKey(root, env, []string{"./wire"}, opts)
	subdirIncrementalKey := incrementalManifestSelectorKey(wireDir, env, []string{"."}, opts)
	if rootIncrementalKey != subdirIncrementalKey {
		t.Fatalf("incrementalManifestSelectorKey mismatch: root=%q subdir=%q", rootIncrementalKey, subdirIncrementalKey)
	}
}

func TestPackageScopedKeysIgnoreEquivalentWorkingDirectories(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.22\n")
	wireDir := filepath.Join(root, "wire")
	writeFile(t, filepath.Join(wireDir, "wire.go"), "package wire\n")

	rootFingerprintKey := incrementalFingerprintKey(root, "wireinject", "example.com/app/wire")
	subdirFingerprintKey := incrementalFingerprintKey(wireDir, "wireinject", "example.com/app/wire")
	if rootFingerprintKey != subdirFingerprintKey {
		t.Fatalf("incrementalFingerprintKey mismatch: root=%q subdir=%q", rootFingerprintKey, subdirFingerprintKey)
	}

	rootSummaryKey := incrementalSummaryKey(root, "wireinject", "example.com/app/wire")
	subdirSummaryKey := incrementalSummaryKey(wireDir, "wireinject", "example.com/app/wire")
	if rootSummaryKey != subdirSummaryKey {
		t.Fatalf("incrementalSummaryKey mismatch: root=%q subdir=%q", rootSummaryKey, subdirSummaryKey)
	}

	rootGraphKey := incrementalGraphKey(root, "wireinject", []string{"example.com/app/wire"})
	subdirGraphKey := incrementalGraphKey(wireDir, "wireinject", []string{"example.com/app/wire"})
	if rootGraphKey != subdirGraphKey {
		t.Fatalf("incrementalGraphKey mismatch: root=%q subdir=%q", rootGraphKey, subdirGraphKey)
	}

	rootSessionKey := sessionKey(root, []string{"GOOS=darwin"}, "wireinject")
	subdirSessionKey := sessionKey(wireDir, []string{"GOOS=darwin"}, "wireinject")
	if rootSessionKey != subdirSessionKey {
		t.Fatalf("sessionKey mismatch: root=%q subdir=%q", rootSessionKey, subdirSessionKey)
	}
}
