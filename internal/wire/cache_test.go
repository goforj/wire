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
)

func TestCacheInvalidation(t *testing.T) {
	repoRoot := mustRepoRoot(t)
	root := t.TempDir()

	prevTmp := os.Getenv("TMPDIR")
	if err := os.Setenv("TMPDIR", t.TempDir()); err != nil {
		t.Fatalf("Setenv TMPDIR failed: %v", err)
	}
	t.Cleanup(func() {
		os.Setenv("TMPDIR", prevTmp)
	})

	writeFile(t, filepath.Join(root, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require github.com/goforj/wire v0.0.0",
		"replace github.com/goforj/wire => " + repoRoot,
		"",
	}, "\n"))

	writeFile(t, filepath.Join(root, "app", "wire.go"), strings.Join([]string{
		"//go:build wireinject",
		"// +build wireinject",
		"",
		"package app",
		"",
		"import (",
		"\t\"example.com/app/dep\"",
		"\t\"github.com/goforj/wire\"",
		")",
		"",
		"func Init() string {",
		"\twire.Build(dep.ProvideMessage)",
		"\treturn \"\"",
		"}",
		"",
	}, "\n"))

	depPath := filepath.Join(root, "dep", "dep.go")
	writeFile(t, depPath, strings.Join([]string{
		"package dep",
		"",
		"func ProvideMessage() string {",
		"\treturn \"hello\"",
		"}",
		"",
	}, "\n"))

	env := append(os.Environ(), "GOWORK=off")
	ctx := context.Background()
	opts := &GenerateOptions{}

	first, errs := Generate(ctx, root, env, []string{"./app"}, opts)
	if len(errs) > 0 {
		t.Fatalf("first Generate errors: %v", errs)
	}
	if len(first) != 1 || len(first[0].Content) == 0 {
		t.Fatalf("first Generate returned unexpected result: %+v", first)
	}

	pkgs, _, errs := load(ctx, root, env, opts.Tags, []string{"./app"})
	if len(errs) > 0 || len(pkgs) != 1 {
		t.Fatalf("load failed: %v", errs)
	}
	key, err := cacheKeyForPackage(pkgs[0], opts)
	if err != nil {
		t.Fatalf("cacheKeyForPackage failed: %v", err)
	}
	if cached, ok := readCache(key); !ok || len(cached) == 0 {
		t.Fatal("expected cache entry after first Generate")
	}

	writeFile(t, depPath, strings.Join([]string{
		"package dep",
		"",
		"func ProvideMessage() string {",
		"\treturn \"goodbye\"",
		"}",
		"",
	}, "\n"))

	second, errs := Generate(ctx, root, env, []string{"./app"}, opts)
	if len(errs) > 0 {
		t.Fatalf("second Generate errors: %v", errs)
	}
	if len(second) != 1 || len(second[0].Content) == 0 {
		t.Fatalf("second Generate returned unexpected result: %+v", second)
	}
	pkgs, _, errs = load(ctx, root, env, opts.Tags, []string{"./app"})
	if len(errs) > 0 || len(pkgs) != 1 {
		t.Fatalf("reload failed: %v", errs)
	}
	key2, err := cacheKeyForPackage(pkgs[0], opts)
	if err != nil {
		t.Fatalf("cacheKeyForPackage after update failed: %v", err)
	}
	if key2 == key {
		t.Fatal("expected cache key to change after source update")
	}
	if cached, ok := readCache(key2); !ok || len(cached) == 0 {
		t.Fatal("expected cache entry after second Generate")
	}
}

func TestManifestInvalidation(t *testing.T) {
	repoRoot := mustRepoRoot(t)
	root := t.TempDir()

	prevTmp := os.Getenv("TMPDIR")
	if err := os.Setenv("TMPDIR", t.TempDir()); err != nil {
		t.Fatalf("Setenv TMPDIR failed: %v", err)
	}
	t.Cleanup(func() {
		os.Setenv("TMPDIR", prevTmp)
	})

	writeFile(t, filepath.Join(root, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require github.com/goforj/wire v0.0.0",
		"replace github.com/goforj/wire => " + repoRoot,
		"",
	}, "\n"))

	writeFile(t, filepath.Join(root, "app", "wire.go"), strings.Join([]string{
		"//go:build wireinject",
		"// +build wireinject",
		"",
		"package app",
		"",
		"import (",
		"\t\"example.com/app/dep\"",
		"\t\"github.com/goforj/wire\"",
		")",
		"",
		"func Init() string {",
		"\twire.Build(dep.ProvideMessage)",
		"\treturn \"\"",
		"}",
		"",
	}, "\n"))

	depPath := filepath.Join(root, "dep", "dep.go")
	writeFile(t, depPath, strings.Join([]string{
		"package dep",
		"",
		"func ProvideMessage() string {",
		"\treturn \"hello\"",
		"}",
		"",
	}, "\n"))

	env := append(os.Environ(), "GOWORK=off")
	ctx := context.Background()
	opts := &GenerateOptions{}

	if _, errs := Generate(ctx, root, env, []string{"./app"}, opts); len(errs) > 0 {
		t.Fatalf("Generate errors: %v", errs)
	}

	key := manifestKey(root, env, []string{"./app"}, opts)
	manifest, ok := readManifest(key)
	if !ok {
		t.Fatal("expected manifest after Generate")
	}
	if !manifestValid(manifest) {
		t.Fatal("expected manifest to be valid")
	}

	writeFile(t, depPath, strings.Join([]string{
		"package dep",
		"",
		"func ProvideMessage() string {",
		"\treturn \"goodbye\"",
		"}",
		"",
	}, "\n"))

	if manifestValid(manifest) {
		t.Fatal("expected manifest to be invalid after source update")
	}
}

func TestManifestInvalidationGoMod(t *testing.T) {
	repoRoot := mustRepoRoot(t)
	root := t.TempDir()

	prevTmp := os.Getenv("TMPDIR")
	if err := os.Setenv("TMPDIR", t.TempDir()); err != nil {
		t.Fatalf("Setenv TMPDIR failed: %v", err)
	}
	t.Cleanup(func() {
		os.Setenv("TMPDIR", prevTmp)
	})

	goModPath := filepath.Join(root, "go.mod")
	writeFile(t, goModPath, strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require github.com/goforj/wire v0.0.0",
		"replace github.com/goforj/wire => " + repoRoot,
		"",
	}, "\n"))

	writeFile(t, filepath.Join(root, "app", "wire.go"), strings.Join([]string{
		"//go:build wireinject",
		"// +build wireinject",
		"",
		"package app",
		"",
		"import (",
		"\t\"example.com/app/dep\"",
		"\t\"github.com/goforj/wire\"",
		")",
		"",
		"func Init() string {",
		"\twire.Build(dep.ProvideMessage)",
		"\treturn \"\"",
		"}",
		"",
	}, "\n"))

	writeFile(t, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"func ProvideMessage() string {",
		"\treturn \"hello\"",
		"}",
		"",
	}, "\n"))

	env := append(os.Environ(), "GOWORK=off")
	ctx := context.Background()
	opts := &GenerateOptions{}

	if _, errs := Generate(ctx, root, env, []string{"./app"}, opts); len(errs) > 0 {
		t.Fatalf("Generate errors: %v", errs)
	}

	key := manifestKey(root, env, []string{"./app"}, opts)
	manifest, ok := readManifest(key)
	if !ok {
		t.Fatal("expected manifest after Generate")
	}
	if !manifestValid(manifest) {
		t.Fatal("expected manifest to be valid")
	}

	writeFile(t, goModPath, strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require github.com/goforj/wire v0.0.0 // updated",
		"replace github.com/goforj/wire => " + repoRoot,
		"",
	}, "\n"))

	if manifestValid(manifest) {
		t.Fatal("expected manifest to be invalid after go.mod update")
	}
}

func TestManifestInvalidationSameTimestamp(t *testing.T) {
	repoRoot := mustRepoRoot(t)
	root := t.TempDir()

	prevTmp := os.Getenv("TMPDIR")
	if err := os.Setenv("TMPDIR", t.TempDir()); err != nil {
		t.Fatalf("Setenv TMPDIR failed: %v", err)
	}
	t.Cleanup(func() {
		os.Setenv("TMPDIR", prevTmp)
	})

	writeFile(t, filepath.Join(root, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require github.com/goforj/wire v0.0.0",
		"replace github.com/goforj/wire => " + repoRoot,
		"",
	}, "\n"))

	wirePath := filepath.Join(root, "app", "wire.go")
	writeFile(t, wirePath, strings.Join([]string{
		"//go:build wireinject",
		"// +build wireinject",
		"",
		"package app",
		"",
		"import (",
		"\t\"example.com/app/dep\"",
		"\t\"github.com/goforj/wire\"",
		")",
		"",
		"func Init() string {",
		"\twire.Build(dep.ProvideMessage)",
		"\treturn \"\"",
		"}",
		"",
	}, "\n"))

	writeFile(t, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"func ProvideMessage() string {",
		"\treturn \"hello\"",
		"}",
		"",
	}, "\n"))

	env := append(os.Environ(), "GOWORK=off")
	ctx := context.Background()
	opts := &GenerateOptions{}

	if _, errs := Generate(ctx, root, env, []string{"./app"}, opts); len(errs) > 0 {
		t.Fatalf("Generate errors: %v", errs)
	}

	key := manifestKey(root, env, []string{"./app"}, opts)
	manifest, ok := readManifest(key)
	if !ok {
		t.Fatal("expected manifest after Generate")
	}
	if !manifestValid(manifest) {
		t.Fatal("expected manifest to be valid")
	}

	info, err := os.Stat(wirePath)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	originalMod := info.ModTime()

	original, err := os.ReadFile(wirePath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	updated := strings.Replace(string(original), "ProvideMessage", "ProvideMassage", 1)
	if len(updated) != len(original) {
		t.Fatalf("expected updated content to keep length; got %d vs %d", len(updated), len(original))
	}
	if err := os.WriteFile(wirePath, []byte(updated), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := os.Chtimes(wirePath, originalMod, originalMod); err != nil {
		t.Fatalf("Chtimes failed: %v", err)
	}

	if manifestValid(manifest) {
		t.Fatal("expected manifest to be invalid after same-timestamp content update")
	}
}
