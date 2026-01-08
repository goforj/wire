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
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestGenerateForPackageOptionAndDetectErrors(t *testing.T) {
	res := generateForPackage(context.Background(), &packages.Package{PkgPath: "example.com/empty"}, nil, nil)
	if len(res.Errs) == 0 {
		t.Fatal("expected error for empty package")
	}
	if _, err := detectOutputDir(nil); err == nil {
		t.Fatal("expected detectOutputDir error")
	}
}

func TestGenerateForPackageCacheKeyError(t *testing.T) {
	tempDir := t.TempDir()
	missing := filepath.Join(tempDir, "missing.go")
	pkg := &packages.Package{
		PkgPath: "example.com/missing",
		GoFiles: []string{missing},
	}
	res := generateForPackage(context.Background(), pkg, nil, &GenerateOptions{})
	if len(res.Errs) == 0 {
		t.Fatal("expected cache key error")
	}
}

func TestGenerateForPackageCacheHit(t *testing.T) {
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
	key, err := cacheKeyForPackage(pkg, opts)
	if err != nil || key == "" {
		t.Fatalf("cacheKeyForPackage failed: %v", err)
	}
	writeCache(key, []byte("cached"))
	res := generateForPackage(context.Background(), pkg, nil, opts)
	if string(res.Content) != "cached" {
		t.Fatalf("expected cached content, got %q", res.Content)
	}
}

func TestGenerateForPackageFormatError(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	tempDir := t.TempDir()
	osTempDir = func() string { return tempDir }

	repoRoot := mustRepoRoot(t)
	writeTempFile(t, tempDir, "go.mod", strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require github.com/goforj/wire v0.0.0",
		"replace github.com/goforj/wire => " + repoRoot,
		"",
	}, "\n"))
	appDir := filepath.Join(tempDir, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	writeTempFile(t, appDir, "wire.go", strings.Join([]string{
		"//go:build wireinject",
		"// +build wireinject",
		"",
		"package app",
		"",
		"import \"github.com/goforj/wire\"",
		"",
		"func Init() string {",
		"\twire.Build(NewMessage)",
		"\treturn \"\"",
		"}",
		"",
		"func NewMessage() string { return \"ok\" }",
		"",
	}, "\n"))

	ctx := context.Background()
	env := append(os.Environ(), "GOWORK=off")
	pkgs, loader, errs := load(ctx, tempDir, env, "", []string{"./app"})
	if len(errs) > 0 || len(pkgs) != 1 {
		t.Fatalf("load errors: %v", errs)
	}
	opts := &GenerateOptions{Header: []byte("invalid")}
	res := generateForPackage(ctx, pkgs[0], loader, opts)
	if len(res.Errs) == 0 {
		t.Fatal("expected format.Source error")
	}
}

func TestAllGeneratedOK(t *testing.T) {
	if allGeneratedOK(nil) {
		t.Fatal("expected empty results to be false")
	}
	if allGeneratedOK([]GenerateResult{{Errs: []error{context.DeadlineExceeded}}}) {
		t.Fatal("expected errors to be false")
	}
	if !allGeneratedOK([]GenerateResult{{}}) {
		t.Fatal("expected success results to be true")
	}
}
