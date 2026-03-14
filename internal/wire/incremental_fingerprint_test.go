// Copyright 2026 The Wire Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package wire

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestPackageShapeHashIgnoresFunctionBodies(t *testing.T) {
	dir := t.TempDir()
	file := writeTempFile(t, dir, "pkg.go", "package p\n\nfunc Hello() string { return \"a\" }\n")
	hash1, err := packageShapeHash([]string{file})
	if err != nil {
		t.Fatalf("packageShapeHash first failed: %v", err)
	}
	if err := os.WriteFile(file, []byte("package p\n\nfunc Hello() string { return \"b\" }\n"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	hash2, err := packageShapeHash([]string{file})
	if err != nil {
		t.Fatalf("packageShapeHash second failed: %v", err)
	}
	if hash1 != hash2 {
		t.Fatalf("body-only change should not affect shape hash: %q vs %q", hash1, hash2)
	}
}

func TestIncrementalFingerprintRoundTrip(t *testing.T) {
	fp := &packageFingerprint{
		Version:      incrementalFingerprintVersion,
		WD:           "/tmp/app",
		Tags:         "dev",
		PkgPath:      "example.com/app",
		ShapeHash:    "shape",
		Files:        []cacheFile{{Path: "/tmp/app/pkg.go", Size: 12, ModTime: 34}},
		LocalImports: []string{"example.com/dep"},
	}
	data, err := encodeIncrementalFingerprint(fp)
	if err != nil {
		t.Fatalf("encodeIncrementalFingerprint failed: %v", err)
	}
	got, err := decodeIncrementalFingerprint(data)
	if err != nil {
		t.Fatalf("decodeIncrementalFingerprint failed: %v", err)
	}
	if !incrementalFingerprintEquivalent(fp, got) {
		t.Fatalf("fingerprint mismatch after round-trip: got %+v want %+v", got, fp)
	}
	if len(got.Files) != 1 || got.Files[0] != fp.Files[0] {
		t.Fatalf("file metadata mismatch after round-trip: got %+v want %+v", got.Files, fp.Files)
	}
}

func TestCollectIncrementalFingerprintsTreatsBodyOnlyChangeAsUnchanged(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	cacheRoot := t.TempDir()
	osTempDir = func() string { return cacheRoot }

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/test\n\ngo 1.21\n")
	file := filepath.Join(root, "app", "app.go")
	writeFile(t, file, "package app\n\nfunc Hello() string { return \"a\" }\n")
	pkg := &packages.Package{
		PkgPath:         "example.com/app",
		CompiledGoFiles: []string{file},
		GoFiles:         []string{file},
		Imports:         map[string]*packages.Package{},
	}

	snapshot := collectIncrementalFingerprints(root, "", []*packages.Package{pkg})
	if snapshot.stats.changed != 1 || len(snapshot.changed) != 1 || snapshot.changed[0] != pkg.PkgPath {
		t.Fatalf("first run stats=%+v changed=%v", snapshot.stats, snapshot.changed)
	}

	if err := os.WriteFile(file, []byte("package app\n\nfunc Hello() string { return \"b\" }\n"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	snapshot = collectIncrementalFingerprints(root, "", []*packages.Package{pkg})
	if snapshot.stats.unchanged != 1 {
		t.Fatalf("body-only change should be unchanged by shape, stats=%+v changed=%v", snapshot.stats, snapshot.changed)
	}
	if len(snapshot.changed) != 0 {
		t.Fatalf("body-only change should not report changed packages, got %v", snapshot.changed)
	}
}
