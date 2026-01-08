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
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"golang.org/x/tools/go/packages"
)

type cacheHookState struct {
	osCreateTemp        func(string, string) (*os.File, error)
	osMkdirAll          func(string, os.FileMode) error
	osReadFile          func(string) ([]byte, error)
	osRemove            func(string) error
	osRemoveAll         func(string) error
	osRename            func(string, string) error
	osStat              func(string) (os.FileInfo, error)
	osTempDir           func() string
	jsonMarshal         func(any) ([]byte, error)
	jsonUnmarshal       func([]byte, any) error
	extraCachePathsFunc func(string) []string
	cacheKeyForPackage  func(*packages.Package, *GenerateOptions) (string, error)
	detectOutputDir     func([]string) (string, error)
	buildCacheFiles     func([]string) ([]cacheFile, error)
	buildCacheFilesFrom func([]cacheFile) ([]cacheFile, error)
	rootPackageFiles    func(*packages.Package) []string
	hashFiles           func([]string) (string, error)
}

var cacheHooksMu sync.Mutex

func lockCacheHooks(t *testing.T) {
	t.Helper()
	cacheHooksMu.Lock()
	t.Cleanup(func() {
		cacheHooksMu.Unlock()
	})
}

func saveCacheHooks() cacheHookState {
	return cacheHookState{
		osCreateTemp:        osCreateTemp,
		osMkdirAll:          osMkdirAll,
		osReadFile:          osReadFile,
		osRemove:            osRemove,
		osRemoveAll:         osRemoveAll,
		osRename:            osRename,
		osStat:              osStat,
		osTempDir:           osTempDir,
		jsonMarshal:         jsonMarshal,
		jsonUnmarshal:       jsonUnmarshal,
		extraCachePathsFunc: extraCachePathsFunc,
		cacheKeyForPackage:  cacheKeyForPackageFunc,
		detectOutputDir:     detectOutputDirFunc,
		buildCacheFiles:     buildCacheFilesFunc,
		buildCacheFilesFrom: buildCacheFilesFromMetaFunc,
		rootPackageFiles:    rootPackageFilesFunc,
		hashFiles:           hashFilesFunc,
	}
}

func restoreCacheHooks(state cacheHookState) {
	osCreateTemp = state.osCreateTemp
	osMkdirAll = state.osMkdirAll
	osReadFile = state.osReadFile
	osRemove = state.osRemove
	osRemoveAll = state.osRemoveAll
	osRename = state.osRename
	osStat = state.osStat
	osTempDir = state.osTempDir
	jsonMarshal = state.jsonMarshal
	jsonUnmarshal = state.jsonUnmarshal
	extraCachePathsFunc = state.extraCachePathsFunc
	cacheKeyForPackageFunc = state.cacheKeyForPackage
	detectOutputDirFunc = state.detectOutputDir
	buildCacheFilesFunc = state.buildCacheFiles
	buildCacheFilesFromMetaFunc = state.buildCacheFilesFrom
	rootPackageFilesFunc = state.rootPackageFiles
	hashFilesFunc = state.hashFiles
}

func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile(%s) failed: %v", path, err)
	}
	return path
}

func cloneManifest(src *cacheManifest) *cacheManifest {
	if src == nil {
		return nil
	}
	dst := *src
	if src.Patterns != nil {
		dst.Patterns = append([]string(nil), src.Patterns...)
	}
	if src.ExtraFiles != nil {
		dst.ExtraFiles = append([]cacheFile(nil), src.ExtraFiles...)
	}
	if src.Packages != nil {
		dst.Packages = make([]manifestPackage, len(src.Packages))
		for i, pkg := range src.Packages {
			dstPkg := pkg
			if pkg.Files != nil {
				dstPkg.Files = append([]cacheFile(nil), pkg.Files...)
			}
			if pkg.RootFiles != nil {
				dstPkg.RootFiles = append([]cacheFile(nil), pkg.RootFiles...)
			}
			dst.Packages[i] = dstPkg
		}
	}
	return &dst
}

func TestCacheStoreReadWrite(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	tempDir := t.TempDir()
	osTempDir = func() string { return tempDir }

	if got := CacheDir(); got == "" {
		t.Fatal("expected CacheDir to return a value")
	}

	key := "cache-store"
	want := []byte("content")
	writeCache(key, want)

	got, ok := readCache(key)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("cache content mismatch: got %q, want %q", got, want)
	}
	if err := ClearCache(); err != nil {
		t.Fatalf("ClearCache failed: %v", err)
	}
	if _, ok := readCache(key); ok {
		t.Fatal("expected cache miss after clear")
	}
}

func TestCacheStoreReadError(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	osReadFile = func(string) ([]byte, error) {
		return nil, errors.New("boom")
	}
	if _, ok := readCache("missing"); ok {
		t.Fatal("expected cache miss on read error")
	}
}

func TestCacheStoreWriteErrors(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	tempDir := t.TempDir()
	osTempDir = func() string { return tempDir }

	t.Run("mkdir", func(t *testing.T) {
		osMkdirAll = func(string, os.FileMode) error { return errors.New("mkdir") }
		writeCache("mkdir", []byte("data"))
	})

	t.Run("create", func(t *testing.T) {
		restoreCacheHooks(state)
		osTempDir = func() string { return tempDir }
		osCreateTemp = func(string, string) (*os.File, error) {
			return nil, errors.New("create")
		}
		writeCache("create", []byte("data"))
	})

	t.Run("write", func(t *testing.T) {
		restoreCacheHooks(state)
		osTempDir = func() string { return tempDir }
		osCreateTemp = func(dir, pattern string) (*os.File, error) {
			tmp, err := os.CreateTemp(dir, pattern)
			if err != nil {
				return nil, err
			}
			name := tmp.Name()
			if err := tmp.Close(); err != nil {
				return nil, err
			}
			return os.Open(name)
		}
		writeCache("write", []byte("data"))
	})

	t.Run("rename-exist", func(t *testing.T) {
		restoreCacheHooks(state)
		osTempDir = func() string { return tempDir }
		osRename = func(string, string) error {
			return fs.ErrExist
		}
		writeCache("exist", []byte("data"))
	})

	t.Run("rename", func(t *testing.T) {
		restoreCacheHooks(state)
		osTempDir = func() string { return tempDir }
		osRename = func(string, string) error {
			return errors.New("rename")
		}
		writeCache("rename", []byte("data"))
	})
}

func TestCacheDirError(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	osRemoveAll = func(string) error { return errors.New("remove") }
	if err := ClearCache(); err == nil {
		t.Fatal("expected ClearCache error")
	}
}

func TestPackageFiles(t *testing.T) {
	tempDir := t.TempDir()
	rootFile := writeTempFile(t, tempDir, "root.go", "package root\n")
	childFile := writeTempFile(t, tempDir, "child.go", "package child\n")

	child := &packages.Package{
		PkgPath:         "example.com/child",
		CompiledGoFiles: []string{childFile},
	}
	root := &packages.Package{
		PkgPath: "example.com/root",
		GoFiles: []string{rootFile},
		Imports: map[string]*packages.Package{
			"child": child,
			"dup":   child,
			"nil":   nil,
		},
	}
	got := packageFiles(root)
	sort.Strings(got)
	if len(got) != 2 {
		t.Fatalf("expected 2 files, got %d", len(got))
	}
	if got[0] != childFile || got[1] != rootFile {
		t.Fatalf("unexpected files: %v", got)
	}
}

func TestCacheKeyEmptyPackage(t *testing.T) {
	key, err := cacheKeyForPackage(&packages.Package{PkgPath: "example.com/empty"}, &GenerateOptions{})
	if err != nil {
		t.Fatalf("cacheKeyForPackage error: %v", err)
	}
	if key != "" {
		t.Fatalf("expected empty cache key, got %q", key)
	}
}

func TestCacheKeyMetaHit(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	tempDir := t.TempDir()
	osTempDir = func() string { return tempDir }

	file := writeTempFile(t, tempDir, "hit.go", "package hit\n")
	pkg := &packages.Package{
		PkgPath: "example.com/hit",
		GoFiles: []string{file},
	}
	opts := &GenerateOptions{}
	files := packageFiles(pkg)
	sort.Strings(files)
	contentHash, err := contentHashForFiles(pkg, opts, files)
	if err != nil {
		t.Fatalf("contentHashForFiles error: %v", err)
	}
	rootFiles := rootPackageFiles(pkg)
	sort.Strings(rootFiles)
	rootHash, err := hashFiles(rootFiles)
	if err != nil {
		t.Fatalf("hashFiles error: %v", err)
	}
	metaFiles, err := buildCacheFiles(files)
	if err != nil {
		t.Fatalf("buildCacheFiles error: %v", err)
	}
	meta := &cacheMeta{
		Version:     cacheVersion,
		PkgPath:     pkg.PkgPath,
		Tags:        opts.Tags,
		Prefix:      opts.PrefixOutputFile,
		HeaderHash:  headerHash(opts.Header),
		Files:       metaFiles,
		ContentHash: contentHash,
		RootHash:    rootHash,
	}
	metaKey := cacheMetaKey(pkg, opts)
	writeCacheMeta(metaKey, meta)

	got, err := cacheKeyForPackage(pkg, opts)
	if err != nil {
		t.Fatalf("cacheKeyForPackage error: %v", err)
	}
	if got != contentHash {
		t.Fatalf("cache key mismatch: got %q, want %q", got, contentHash)
	}
}

func TestCacheKeyErrorPaths(t *testing.T) {
	pkg := &packages.Package{
		PkgPath: "example.com/missing",
		GoFiles: []string{filepath.Join(t.TempDir(), "missing.go")},
	}
	if _, err := cacheKeyForPackage(pkg, &GenerateOptions{}); err == nil {
		t.Fatal("expected cacheKeyForPackage error")
	}
	if _, err := buildCacheFiles([]string{filepath.Join(t.TempDir(), "missing.go")}); err == nil {
		t.Fatal("expected buildCacheFiles error")
	}
	if _, err := contentHashForPaths("example.com/missing", &GenerateOptions{}, []string{filepath.Join(t.TempDir(), "missing.go")}); err == nil {
		t.Fatal("expected contentHashForPaths error")
	}
	if _, err := hashFiles([]string{filepath.Join(t.TempDir(), "missing.go")}); err == nil {
		t.Fatal("expected hashFiles error")
	}
	if got, err := hashFiles(nil); err != nil || got != "" {
		t.Fatalf("expected empty hashFiles result, got %q err=%v", got, err)
	}
}

func TestCacheMetaMatches(t *testing.T) {
	tempDir := t.TempDir()
	file := writeTempFile(t, tempDir, "meta.go", "package meta\n")
	pkg := &packages.Package{
		PkgPath: "example.com/meta",
		GoFiles: []string{file},
	}
	opts := &GenerateOptions{}
	files := packageFiles(pkg)
	sort.Strings(files)
	metaFiles, err := buildCacheFiles(files)
	if err != nil {
		t.Fatalf("buildCacheFiles error: %v", err)
	}
	rootFiles := rootPackageFiles(pkg)
	sort.Strings(rootFiles)
	rootHash, err := hashFiles(rootFiles)
	if err != nil {
		t.Fatalf("hashFiles error: %v", err)
	}
	contentHash, err := contentHashForFiles(pkg, opts, files)
	if err != nil {
		t.Fatalf("contentHashForFiles error: %v", err)
	}
	meta := &cacheMeta{
		Version:     cacheVersion,
		PkgPath:     pkg.PkgPath,
		Tags:        opts.Tags,
		Prefix:      opts.PrefixOutputFile,
		HeaderHash:  headerHash(opts.Header),
		Files:       metaFiles,
		ContentHash: contentHash,
		RootHash:    rootHash,
	}
	if !cacheMetaMatches(meta, pkg, opts, files) {
		t.Fatal("expected cacheMetaMatches to succeed")
	}
	badVersion := *meta
	badVersion.Version = "nope"
	if cacheMetaMatches(&badVersion, pkg, opts, files) {
		t.Fatal("expected version mismatch")
	}
	badPkg := *meta
	badPkg.PkgPath = "example.com/other"
	if cacheMetaMatches(&badPkg, pkg, opts, files) {
		t.Fatal("expected pkg mismatch")
	}
	badHeader := *meta
	badHeader.HeaderHash = "bad"
	if cacheMetaMatches(&badHeader, pkg, opts, files) {
		t.Fatal("expected header mismatch")
	}
	shortFiles := *meta
	shortFiles.Files = nil
	if cacheMetaMatches(&shortFiles, pkg, opts, files) {
		t.Fatal("expected file count mismatch")
	}
	fileMismatch := *meta
	fileMismatch.Files = append([]cacheFile(nil), meta.Files...)
	fileMismatch.Files[0].Size++
	if cacheMetaMatches(&fileMismatch, pkg, opts, files) {
		t.Fatal("expected file metadata mismatch")
	}
	pkgNoRoot := &packages.Package{PkgPath: pkg.PkgPath}
	if cacheMetaMatches(meta, pkgNoRoot, opts, files) {
		t.Fatal("expected missing root files")
	}
	noRootHash := *meta
	noRootHash.RootHash = ""
	if cacheMetaMatches(&noRootHash, pkg, opts, files) {
		t.Fatal("expected empty root hash mismatch")
	}
	missingRootPkg := &packages.Package{
		PkgPath: "example.com/meta",
		GoFiles: []string{filepath.Join(tempDir, "missing.go")},
	}
	if cacheMetaMatches(meta, missingRootPkg, opts, files) {
		t.Fatal("expected root hash error")
	}
	badRoot := *meta
	badRoot.RootHash = "bad"
	if cacheMetaMatches(&badRoot, pkg, opts, files) {
		t.Fatal("expected root hash mismatch")
	}
	emptyContent := *meta
	emptyContent.ContentHash = ""
	if cacheMetaMatches(&emptyContent, pkg, opts, files) {
		t.Fatal("expected empty content hash mismatch")
	}

	if cacheMetaMatches(meta, pkg, opts, []string{filepath.Join(tempDir, "missing.go")}) {
		t.Fatal("expected buildCacheFiles error")
	}
}

func TestCacheMetaReadWriteErrors(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	tempDir := t.TempDir()
	osTempDir = func() string { return tempDir }

	if _, ok := readCacheMeta("missing"); ok {
		t.Fatal("expected cache meta miss")
	}

	osReadFile = func(string) ([]byte, error) {
		return []byte("{bad json"), nil
	}
	if _, ok := readCacheMeta("bad-json"); ok {
		t.Fatal("expected cache meta miss on invalid json")
	}

	restoreCacheHooks(state)
	osTempDir = func() string { return tempDir }
	osMkdirAll = func(string, os.FileMode) error { return errors.New("mkdir") }
	writeCacheMeta("mkdir", &cacheMeta{})

	restoreCacheHooks(state)
	osTempDir = func() string { return tempDir }
	jsonMarshal = func(any) ([]byte, error) { return nil, errors.New("marshal") }
	writeCacheMeta("marshal", &cacheMeta{})

	restoreCacheHooks(state)
	osTempDir = func() string { return tempDir }
	osCreateTemp = func(string, string) (*os.File, error) { return nil, errors.New("create") }
	writeCacheMeta("create", &cacheMeta{})

	restoreCacheHooks(state)
	osTempDir = func() string { return tempDir }
	osCreateTemp = func(dir, pattern string) (*os.File, error) {
		tmp, err := os.CreateTemp(dir, pattern)
		if err != nil {
			return nil, err
		}
		name := tmp.Name()
		if err := tmp.Close(); err != nil {
			return nil, err
		}
		return os.Open(name)
	}
	writeCacheMeta("write", &cacheMeta{})

	restoreCacheHooks(state)
	osTempDir = func() string { return tempDir }
	osRename = func(string, string) error { return errors.New("rename") }
	writeCacheMeta("rename", &cacheMeta{})
}

func TestManifestReadWriteErrors(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	tempDir := t.TempDir()
	osTempDir = func() string { return tempDir }

	if _, ok := readManifest("missing"); ok {
		t.Fatal("expected manifest miss")
	}

	osReadFile = func(string) ([]byte, error) {
		return []byte("{bad json"), nil
	}
	if _, ok := readManifest("bad-json"); ok {
		t.Fatal("expected manifest miss on invalid json")
	}

	restoreCacheHooks(state)
	osTempDir = func() string { return tempDir }
	osMkdirAll = func(string, os.FileMode) error { return errors.New("mkdir") }
	writeManifestFile("mkdir", &cacheManifest{})

	restoreCacheHooks(state)
	osTempDir = func() string { return tempDir }
	jsonMarshal = func(any) ([]byte, error) { return nil, errors.New("marshal") }
	writeManifestFile("marshal", &cacheManifest{})

	restoreCacheHooks(state)
	osTempDir = func() string { return tempDir }
	osCreateTemp = func(string, string) (*os.File, error) { return nil, errors.New("create") }
	writeManifestFile("create", &cacheManifest{})

	restoreCacheHooks(state)
	osTempDir = func() string { return tempDir }
	osCreateTemp = func(dir, pattern string) (*os.File, error) {
		tmp, err := os.CreateTemp(dir, pattern)
		if err != nil {
			return nil, err
		}
		name := tmp.Name()
		if err := tmp.Close(); err != nil {
			return nil, err
		}
		return os.Open(name)
	}
	writeManifestFile("write", &cacheManifest{})

	restoreCacheHooks(state)
	osTempDir = func() string { return tempDir }
	osRename = func(string, string) error { return errors.New("rename") }
	writeManifestFile("rename", &cacheManifest{})
}

func TestManifestKeyHelpers(t *testing.T) {
	if got := manifestKeyFromManifest(nil); got != "" {
		t.Fatalf("expected empty manifest key, got %q", got)
	}
	env := []string{"A=B"}
	opts := &GenerateOptions{
		Tags:             "tags",
		PrefixOutputFile: "prefix",
		Header:           []byte("header"),
	}
	manifest := &cacheManifest{
		WD:         t.TempDir(),
		EnvHash:    envHash(env),
		Tags:       opts.Tags,
		Prefix:     opts.PrefixOutputFile,
		HeaderHash: headerHash(opts.Header),
		Patterns:   []string{"./a", "./b"},
	}
	got := manifestKeyFromManifest(manifest)
	want := manifestKey(manifest.WD, env, manifest.Patterns, opts)
	if got != want {
		t.Fatalf("manifest key mismatch: got %q, want %q", got, want)
	}
}

func TestReadManifestResultsPaths(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	tempDir := t.TempDir()
	osTempDir = func() string { return tempDir }

	wd := t.TempDir()
	env := []string{"A=B"}
	patterns := []string{"./..."}
	opts := &GenerateOptions{}

	if _, ok := readManifestResults(wd, env, patterns, opts); ok {
		t.Fatal("expected no manifest")
	}

	key := manifestKey(wd, env, patterns, opts)
	invalid := &cacheManifest{Version: cacheVersion, WD: wd, EnvHash: "", Packages: nil}
	writeManifestFile(key, invalid)
	if _, ok := readManifestResults(wd, env, patterns, opts); ok {
		t.Fatal("expected invalid manifest miss")
	}

	file := writeTempFile(t, wd, "wire.go", "package app\n")
	pkg := &packages.Package{
		PkgPath: "example.com/app",
		GoFiles: []string{file},
	}
	files := packageFiles(pkg)
	sort.Strings(files)
	contentHash, err := contentHashForFiles(pkg, opts, files)
	if err != nil {
		t.Fatalf("contentHashForFiles error: %v", err)
	}
	metaFiles, err := buildCacheFiles(files)
	if err != nil {
		t.Fatalf("buildCacheFiles error: %v", err)
	}
	rootFiles := rootPackageFiles(pkg)
	sort.Strings(rootFiles)
	rootMeta, err := buildCacheFiles(rootFiles)
	if err != nil {
		t.Fatalf("buildCacheFiles error: %v", err)
	}
	rootHash, err := hashFiles(rootFiles)
	if err != nil {
		t.Fatalf("hashFiles error: %v", err)
	}
	valid := &cacheManifest{
		Version:    cacheVersion,
		WD:         wd,
		Tags:       opts.Tags,
		Prefix:     opts.PrefixOutputFile,
		HeaderHash: headerHash(opts.Header),
		EnvHash:    envHash(env),
		Patterns:   sortedStrings(patterns),
		Packages: []manifestPackage{
			{
				PkgPath:     pkg.PkgPath,
				OutputPath:  filepath.Join(wd, "wire_gen.go"),
				Files:       metaFiles,
				ContentHash: contentHash,
				RootFiles:   rootMeta,
				RootHash:    rootHash,
			},
		},
	}
	writeManifestFile(key, valid)
	if _, ok := readManifestResults(wd, env, patterns, opts); ok {
		t.Fatal("expected cache miss without content")
	}
	writeCache(contentHash, []byte("wire"))
	if results, ok := readManifestResults(wd, env, patterns, opts); !ok || len(results) != 1 {
		t.Fatalf("expected manifest cache hit, got ok=%v results=%d", ok, len(results))
	}
}

func TestWriteManifestBranches(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	tempDir := t.TempDir()
	osTempDir = func() string { return tempDir }

	wd := t.TempDir()
	env := []string{"A=B"}
	patterns := []string{"./..."}
	opts := &GenerateOptions{}

	writeManifest(wd, env, patterns, opts, nil)

	writeManifest(wd, env, patterns, opts, []*packages.Package{nil})

	writeManifest(wd, env, patterns, opts, []*packages.Package{{PkgPath: "example.com/empty"}})

	missingFilePkg := &packages.Package{
		PkgPath: "example.com/missing",
		GoFiles: []string{filepath.Join(wd, "missing.go")},
	}
	writeManifest(wd, env, patterns, opts, []*packages.Package{missingFilePkg})

	conflictDir := t.TempDir()
	fileA := writeTempFile(t, conflictDir, "a.go", "package a\n")
	fileB := writeTempFile(t, t.TempDir(), "b.go", "package b\n")
	conflictPkg := &packages.Package{
		PkgPath: "example.com/conflict",
		GoFiles: []string{fileA, fileB},
	}
	writeManifest(wd, env, patterns, opts, []*packages.Package{conflictPkg})

	okFile := writeTempFile(t, wd, "ok.go", "package ok\n")
	okPkg := &packages.Package{
		PkgPath: "example.com/ok",
		GoFiles: []string{okFile},
	}
	cacheKeyForPackageFunc = func(*packages.Package, *GenerateOptions) (string, error) {
		return "", errors.New("cache key")
	}
	writeManifest(wd, env, patterns, opts, []*packages.Package{okPkg})

	cacheKeyForPackageFunc = func(*packages.Package, *GenerateOptions) (string, error) {
		return "", nil
	}
	writeManifest(wd, env, patterns, opts, []*packages.Package{okPkg})

	cacheKeyForPackageFunc = func(*packages.Package, *GenerateOptions) (string, error) {
		return "hash", nil
	}
	detectOutputDirFunc = func([]string) (string, error) {
		return "", errors.New("output")
	}
	writeManifest(wd, env, patterns, opts, []*packages.Package{okPkg})

	detectOutputDirFunc = state.detectOutputDir
	buildCacheFilesFunc = func([]string) ([]cacheFile, error) {
		return nil, errors.New("build")
	}
	writeManifest(wd, env, patterns, opts, []*packages.Package{okPkg})

	call := 0
	buildCacheFilesFunc = func([]string) ([]cacheFile, error) {
		call++
		if call > 1 {
			return nil, errors.New("root")
		}
		return []cacheFile{{Path: okFile}}, nil
	}
	rootPackageFilesFunc = func(*packages.Package) []string {
		return []string{okFile}
	}
	writeManifest(wd, env, patterns, opts, []*packages.Package{okPkg})

	buildCacheFilesFunc = state.buildCacheFiles
	hashFilesFunc = func([]string) (string, error) {
		return "", errors.New("hash")
	}
	writeManifest(wd, env, patterns, opts, []*packages.Package{okPkg})

	restoreCacheHooks(state)
	statCalls := 0
	osStat = func(name string) (os.FileInfo, error) {
		statCalls++
		if statCalls > 3 {
			return nil, errors.New("stat")
		}
		return state.osStat(name)
	}
	writeManifest(wd, env, patterns, opts, []*packages.Package{okPkg})

	restoreCacheHooks(state)
	osTempDir = func() string { return tempDir }
	readCalls := 0
	osReadFile = func(name string) ([]byte, error) {
		readCalls++
		if readCalls > 2 {
			return nil, errors.New("read")
		}
		return state.osReadFile(name)
	}
	writeManifest(wd, env, patterns, opts, []*packages.Package{okPkg})
}

func TestManifestValidationAndExtras(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	if manifestValid(nil) {
		t.Fatal("expected nil manifest invalid")
	}
	if manifestValid(&cacheManifest{Version: "bad"}) {
		t.Fatal("expected version mismatch")
	}
	if manifestValid(&cacheManifest{Version: cacheVersion}) {
		t.Fatal("expected missing env hash")
	}

	tempDir := t.TempDir()
	file := writeTempFile(t, tempDir, "valid.go", "package valid\n")
	files, err := buildCacheFiles([]string{file})
	if err != nil {
		t.Fatalf("buildCacheFiles error: %v", err)
	}
	rootHash, err := hashFiles([]string{file})
	if err != nil {
		t.Fatalf("hashFiles error: %v", err)
	}
	valid := &cacheManifest{
		Version:    cacheVersion,
		WD:         tempDir,
		EnvHash:    "env",
		Packages:   []manifestPackage{{PkgPath: "example.com/valid", Files: files, RootFiles: files, ContentHash: "hash", RootHash: rootHash}},
		ExtraFiles: nil,
	}
	if !manifestValid(valid) {
		t.Fatal("expected valid manifest")
	}

	invalidExtra := cloneManifest(valid)
	invalidExtra.ExtraFiles = []cacheFile{{Path: filepath.Join(tempDir, "missing.go")}}
	if manifestValid(invalidExtra) {
		t.Fatal("expected invalid extra files")
	}

	extraMismatch := cloneManifest(valid)
	extraMismatch.ExtraFiles = []cacheFile{files[0]}
	extraMismatch.ExtraFiles[0].Size++
	if manifestValid(extraMismatch) {
		t.Fatal("expected extra file metadata mismatch")
	}

	invalidPkg := cloneManifest(valid)
	invalidPkg.Packages[0].ContentHash = ""
	if manifestValid(invalidPkg) {
		t.Fatal("expected invalid content hash")
	}

	invalidRoot := cloneManifest(valid)
	invalidRoot.Packages[0].RootHash = ""
	if manifestValid(invalidRoot) {
		t.Fatal("expected invalid root hash")
	}

	invalidFiles := cloneManifest(valid)
	invalidFiles.Packages[0].Files = []cacheFile{{Path: filepath.Join(tempDir, "missing.go")}}
	if manifestValid(invalidFiles) {
		t.Fatal("expected invalid package files")
	}

	fileMismatch := cloneManifest(valid)
	fileMismatch.Packages[0].Files = []cacheFile{files[0]}
	fileMismatch.Packages[0].Files[0].Size++
	if manifestValid(fileMismatch) {
		t.Fatal("expected package file mismatch")
	}

	invalidRootFiles := cloneManifest(valid)
	invalidRootFiles.Packages[0].RootFiles = []cacheFile{{Path: filepath.Join(tempDir, "missing.go")}}
	if manifestValid(invalidRootFiles) {
		t.Fatal("expected invalid root files")
	}

	rootMismatch := cloneManifest(valid)
	rootMismatch.Packages[0].RootFiles = []cacheFile{files[0]}
	rootMismatch.Packages[0].RootFiles[0].Size++
	if manifestValid(rootMismatch) {
		t.Fatal("expected root file mismatch")
	}

	emptyRoot := cloneManifest(valid)
	emptyRoot.Packages[0].RootFiles = nil
	if manifestValid(emptyRoot) {
		t.Fatal("expected empty root files")
	}

	badHash := cloneManifest(valid)
	badHash.Packages[0].RootHash = "bad"
	if manifestValid(badHash) {
		t.Fatal("expected root hash mismatch")
	}

	if _, err := buildCacheFilesFromMeta([]cacheFile{{Path: filepath.Join(tempDir, "missing.go")}}); err == nil {
		t.Fatal("expected buildCacheFilesFromMeta error")
	}

	extraCachePathsFunc = func(string) []string {
		return []string{file, file, filepath.Join(tempDir, "missing.go")}
	}
	extras := extraCacheFiles(tempDir)
	if len(extras) != 1 {
		t.Fatalf("expected 1 extra file, got %d", len(extras))
	}

	extraCachePathsFunc = func(string) []string { return nil }
	if extras := extraCacheFiles(tempDir); extras != nil {
		t.Fatal("expected nil extras")
	}

	extraCachePathsFunc = func(string) []string { return []string{file, writeTempFile(t, tempDir, "go.sum", "sum\n")} }
	if extras := extraCacheFiles(tempDir); len(extras) < 2 {
		t.Fatalf("expected extras to include two files, got %v", extras)
	}
}

func TestExtraCachePaths(t *testing.T) {
	tempDir := t.TempDir()
	rootMod := writeTempFile(t, tempDir, "go.mod", "module example.com/root\n")
	writeTempFile(t, tempDir, "go.sum", "sum\n")
	nested := filepath.Join(tempDir, "nested", "dir")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	paths := extraCachePaths(nested)
	if len(paths) < 2 {
		t.Fatalf("expected extra cache paths, got %v", paths)
	}
	found := false
	for _, path := range paths {
		if path == rootMod {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected %s in paths: %v", rootMod, paths)
	}
	if got := sortedStrings(nil); got != nil {
		t.Fatal("expected nil for empty sortedStrings")
	}
	if got := envHash(nil); got != "" {
		t.Fatal("expected empty env hash")
	}
}

func TestRootPackageFiles(t *testing.T) {
	if rootPackageFiles(nil) != nil {
		t.Fatal("expected nil root files for nil package")
	}
	tempDir := t.TempDir()
	compiled := writeTempFile(t, tempDir, "compiled.go", "package compiled\n")
	pkg := &packages.Package{
		PkgPath:         "example.com/compiled",
		CompiledGoFiles: []string{compiled},
	}
	got := rootPackageFiles(pkg)
	if len(got) != 1 || got[0] != compiled {
		t.Fatalf("unexpected compiled files: %v", got)
	}
}

func TestAddExtraCachePath(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	tempDir := t.TempDir()
	file := writeTempFile(t, tempDir, "go.mod", "module example.com\n")
	var paths []string
	seen := make(map[string]struct{})
	addExtraCachePath(&paths, seen, file)
	addExtraCachePath(&paths, seen, file)
	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(paths))
	}
	addExtraCachePath(&paths, seen, filepath.Join(tempDir, "missing.go"))
	if len(paths) != 1 {
		t.Fatalf("unexpected extra path append: %v", paths)
	}
}

func TestManifestValidHookBranches(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	tempDir := t.TempDir()
	file := writeTempFile(t, tempDir, "hook.go", "package hook\n")
	files, err := buildCacheFiles([]string{file})
	if err != nil {
		t.Fatalf("buildCacheFiles error: %v", err)
	}
	rootHash, err := hashFiles([]string{file})
	if err != nil {
		t.Fatalf("hashFiles error: %v", err)
	}
	base := &cacheManifest{
		Version:    cacheVersion,
		WD:         tempDir,
		EnvHash:    "env",
		Packages:   []manifestPackage{{PkgPath: "example.com/hook", Files: files, RootFiles: files, ContentHash: "hash", RootHash: rootHash}},
		ExtraFiles: []cacheFile{files[0]},
	}

	buildCacheFilesFromMetaFunc = func(in []cacheFile) ([]cacheFile, error) {
		if len(in) == 1 && in[0].Path == files[0].Path {
			return []cacheFile{}, nil
		}
		return buildCacheFilesFromMeta(in)
	}
	if manifestValid(base) {
		t.Fatal("expected extra file length mismatch")
	}

	restoreCacheHooks(state)
	emptyRoot := cloneManifest(base)
	emptyRoot.Packages[0].RootFiles = nil
	if manifestValid(emptyRoot) {
		t.Fatal("expected empty root files")
	}

	restoreCacheHooks(state)
	buildCacheFilesFromMetaFunc = func(in []cacheFile) ([]cacheFile, error) {
		if len(in) == 1 && in[0].Path == file {
			return nil, errors.New("pkg files")
		}
		return buildCacheFilesFromMeta(in)
	}
	noExtra := cloneManifest(base)
	noExtra.ExtraFiles = nil
	if manifestValid(noExtra) {
		t.Fatal("expected pkg files error")
	}

	restoreCacheHooks(state)
	buildCacheFilesFromMetaFunc = func(in []cacheFile) ([]cacheFile, error) {
		if len(in) == 1 && in[0].Path == file {
			return []cacheFile{}, nil
		}
		return buildCacheFilesFromMeta(in)
	}
	if manifestValid(noExtra) {
		t.Fatal("expected pkg files length mismatch")
	}

	restoreCacheHooks(state)
	call := 0
	buildCacheFilesFromMetaFunc = func(in []cacheFile) ([]cacheFile, error) {
		call++
		if call == 2 {
			return nil, errors.New("root files")
		}
		return buildCacheFilesFromMeta(in)
	}
	if manifestValid(noExtra) {
		t.Fatal("expected root files error")
	}

	restoreCacheHooks(state)
	call = 0
	buildCacheFilesFromMetaFunc = func(in []cacheFile) ([]cacheFile, error) {
		call++
		if call == 2 {
			return []cacheFile{}, nil
		}
		return buildCacheFilesFromMeta(in)
	}
	if manifestValid(noExtra) {
		t.Fatal("expected root files length mismatch")
	}

	restoreCacheHooks(state)
	call = 0
	buildCacheFilesFromMetaFunc = func(in []cacheFile) ([]cacheFile, error) {
		call++
		if call == 2 {
			return []cacheFile{{Path: file, Size: files[0].Size + 1}}, nil
		}
		return buildCacheFilesFromMeta(in)
	}
	if manifestValid(noExtra) {
		t.Fatal("expected root files mismatch")
	}
}
