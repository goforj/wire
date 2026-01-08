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
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestGenerateUsesManifestCache(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	tempDir := t.TempDir()
	osTempDir = func() string { return tempDir }

	wd := t.TempDir()
	file := filepath.Join(wd, "provider.go")
	if err := os.WriteFile(file, []byte("package p\n"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	env := []string{"A=B"}
	patterns := []string{"./..."}
	opts := &GenerateOptions{}
	key := manifestKey(wd, env, patterns, opts)

	pkg := &packages.Package{
		PkgPath: "example.com/p",
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
		t.Fatalf("buildCacheFiles root error: %v", err)
	}
	rootHash, err := hashFiles(rootFiles)
	if err != nil {
		t.Fatalf("hashFiles error: %v", err)
	}

	manifest := &cacheManifest{
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
	writeManifestFile(key, manifest)
	writeCache(contentHash, []byte("wire"))

	results, errs := Generate(context.Background(), wd, env, patterns, opts)
	if len(errs) > 0 {
		t.Fatalf("Generate returned errors: %v", errs)
	}
	if len(results) != 1 || string(results[0].Content) != "wire" {
		t.Fatalf("unexpected cached results: %+v", results)
	}
}
