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
	"time"
)

func TestLoadAndGenerateModule(t *testing.T) {
	repoRoot := mustRepoRoot(t)
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require github.com/goforj/wire v0.0.0",
		"replace github.com/goforj/wire => " + repoRoot,
		"",
	}, "\n"))

	writeFile(t, filepath.Join(root, "app", "app.go"), strings.Join([]string{
		"package app",
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
		"func Init() *dep.Foo {",
		"\twire.Build(dep.New)",
		"\treturn nil",
		"}",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"type Foo struct{}",
		"",
		"func New() *Foo {",
		"\treturn &Foo{}",
		"}",
		"",
	}, "\n"))

	writeFile(t, filepath.Join(root, "noop", "noop.go"), strings.Join([]string{
		"package noop",
		"",
		"type Thing struct{}",
		"",
	}, "\n"))

	env := append(os.Environ(), "GOWORK=off")
	ctx := context.Background()

	info, errs := Load(ctx, root, env, "", []string{"./app"})
	if len(errs) > 0 {
		t.Fatalf("Load returned errors: %v", errs)
	}
	if info == nil {
		t.Fatal("Load returned nil info")
	}
	if len(info.Injectors) != 1 || info.Injectors[0].FuncName != "Init" {
		t.Fatalf("Load returned unexpected injectors: %+v", info.Injectors)
	}

	gens, errs := Generate(ctx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("Generate returned errors: %v", errs)
	}
	if len(gens) != 1 {
		t.Fatalf("Generate returned %d results, want 1", len(gens))
	}
	if len(gens[0].Errs) > 0 {
		t.Fatalf("Generate result had errors: %v", gens[0].Errs)
	}
	if len(gens[0].Content) == 0 {
		t.Fatal("Generate returned empty output for wire package")
	}
	if gens[0].OutputPath == "" {
		t.Fatal("Generate returned empty output path")
	}

	noops, errs := Generate(ctx, root, env, []string{"./noop"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("Generate noop returned errors: %v", errs)
	}
	if len(noops) != 1 {
		t.Fatalf("Generate noop returned %d results, want 1", len(noops))
	}
	if len(noops[0].Errs) > 0 {
		t.Fatalf("Generate noop result had errors: %v", noops[0].Errs)
	}
	if noops[0].OutputPath == "" {
		t.Fatal("Generate noop returned empty output path")
	}
	if len(noops[0].Content) != 0 {
		t.Fatal("Generate noop returned unexpected output")
	}
}

func TestLoadAndGenerateModuleIncrementalMatches(t *testing.T) {
	repoRoot := mustRepoRoot(t)
	root := t.TempDir()

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
		"func Init() *dep.Foo {",
		"\twire.Build(dep.NewSet)",
		"\treturn nil",
		"}",
		"",
	}, "\n"))

	writeFile(t, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"type Foo struct { Message string }",
		"",
		"func NewMessage() string { return \"ok\" }",
		"",
		"func New(msg string) *Foo {",
		"\treturn &Foo{Message: msg}",
		"}",
		"",
	}, "\n"))

	writeFile(t, filepath.Join(root, "dep", "wire.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"github.com/goforj/wire\"",
		"",
		"var NewSet = wire.NewSet(NewMessage, New)",
		"",
	}, "\n"))

	env := append(os.Environ(), "GOWORK=off")

	info, errs := Load(context.Background(), root, env, "", []string{"./app"})
	if len(errs) > 0 {
		t.Fatalf("Load returned errors: %v", errs)
	}
	if info == nil || len(info.Injectors) != 1 {
		t.Fatalf("Load returned unexpected info: %+v errs=%v", info, errs)
	}

	incrementalCtx := WithIncremental(context.Background(), true)
	incrementalInfo, errs := Load(incrementalCtx, root, env, "", []string{"./app"})
	if len(errs) > 0 {
		t.Fatalf("incremental Load returned errors: %v", errs)
	}
	if incrementalInfo == nil || len(incrementalInfo.Injectors) != 1 {
		t.Fatalf("incremental Load returned unexpected info: %+v errs=%v", incrementalInfo, errs)
	}

	normalGens, errs := Generate(context.Background(), root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("Generate returned errors: %v", errs)
	}
	incrementalGens, errs := Generate(incrementalCtx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("incremental Generate returned errors: %v", errs)
	}
	if len(normalGens) != 1 || len(incrementalGens) != 1 {
		t.Fatalf("unexpected result counts: normal=%d incremental=%d", len(normalGens), len(incrementalGens))
	}
	if len(normalGens[0].Errs) > 0 || len(incrementalGens[0].Errs) > 0 {
		t.Fatalf("unexpected generate errors: normal=%v incremental=%v", normalGens[0].Errs, incrementalGens[0].Errs)
	}
	if normalGens[0].OutputPath != incrementalGens[0].OutputPath {
		t.Fatalf("output paths differ: normal=%q incremental=%q", normalGens[0].OutputPath, incrementalGens[0].OutputPath)
	}
	if string(normalGens[0].Content) != string(incrementalGens[0].Content) {
		t.Fatalf("generated content differs between normal and incremental modes")
	}
}

func TestGenerateIncrementalManifestSkipsLazyLoadOnBodyOnlyChange(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	cacheRoot := t.TempDir()
	osTempDir = func() string { return cacheRoot }

	repoRoot := mustRepoRoot(t)
	root := t.TempDir()

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
		"func Init() *dep.Foo {",
		"\twire.Build(dep.NewSet)",
		"\treturn nil",
		"}",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "app", "wire_gen.go"), strings.Join([]string{
		"//go:build !wireinject",
		"",
		"package app",
		"",
		"func generated() {}",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "app", "app_test.go"), strings.Join([]string{
		"package app",
		"",
		"func testOnly() {}",
		"",
	}, "\n"))

	depFile := filepath.Join(root, "dep", "dep.go")
	writeFile(t, depFile, strings.Join([]string{
		"package dep",
		"",
		"type Foo struct { Message string }",
		"",
		"func NewMessage() string { return \"a\" }",
		"",
		"func New(msg string) *Foo {",
		"\treturn &Foo{Message: msg}",
		"}",
		"",
	}, "\n"))

	writeFile(t, filepath.Join(root, "dep", "wire.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"github.com/goforj/wire\"",
		"",
		"var NewSet = wire.NewSet(NewMessage, New)",
		"",
	}, "\n"))

	env := append(os.Environ(), "GOWORK=off")
	ctx := WithIncremental(context.Background(), true)

	var firstLabels []string
	firstCtx := WithTiming(ctx, func(label string, _ time.Duration) {
		firstLabels = append(firstLabels, label)
	})
	first, errs := Generate(firstCtx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("first Generate returned errors: %v", errs)
	}
	if len(first) != 1 || len(first[0].Errs) > 0 {
		t.Fatalf("unexpected first Generate result: %+v", first)
	}
	if !containsLabel(firstLabels, "load.packages.lazy.load") {
		t.Fatalf("expected first incremental generate to perform lazy load, labels=%v", firstLabels)
	}

	if err := os.WriteFile(depFile, []byte(strings.Join([]string{
		"package dep",
		"",
		"type Foo struct { Message string }",
		"",
		"func NewMessage() string { return \"b\" }",
		"",
		"func New(msg string) *Foo {",
		"\treturn &Foo{Message: msg}",
		"}",
		"",
	}, "\n")), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	var secondLabels []string
	secondCtx := WithTiming(ctx, func(label string, _ time.Duration) {
		secondLabels = append(secondLabels, label)
	})
	second, errs := Generate(secondCtx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("second Generate returned errors: %v", errs)
	}
	if len(second) != 1 || len(second[0].Errs) > 0 {
		t.Fatalf("unexpected second Generate result: %+v", second)
	}
	if containsLabel(secondLabels, "generate.load") {
		t.Fatalf("expected second Generate to hit preload incremental manifest before package load, labels=%v", secondLabels)
	}
	if containsLabel(secondLabels, "load.packages.lazy.load") {
		t.Fatalf("expected second Generate to skip lazy load, labels=%v", secondLabels)
	}
	if string(first[0].Content) != string(second[0].Content) {
		t.Fatal("expected body-only change to reuse identical generated output")
	}
}

func TestGenerateIncrementalShapeChangeFallsBackToLazyLoad(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	cacheRoot := t.TempDir()
	osTempDir = func() string { return cacheRoot }

	repoRoot := mustRepoRoot(t)
	root := t.TempDir()

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
		"func Init() *dep.Foo {",
		"\twire.Build(dep.NewSet)",
		"\treturn nil",
		"}",
		"",
	}, "\n"))

	depFile := filepath.Join(root, "dep", "dep.go")
	writeFile(t, depFile, strings.Join([]string{
		"package dep",
		"",
		"type Foo struct { Message string }",
		"",
		"func NewMessage() string { return \"a\" }",
		"",
		"func New(msg string) *Foo {",
		"\treturn &Foo{Message: msg}",
		"}",
		"",
	}, "\n"))

	wireFile := filepath.Join(root, "dep", "wire.go")
	writeFile(t, wireFile, strings.Join([]string{
		"package dep",
		"",
		"import \"github.com/goforj/wire\"",
		"",
		"var NewSet = wire.NewSet(NewMessage, New)",
		"",
	}, "\n"))

	env := append(os.Environ(), "GOWORK=off")
	ctx := WithIncremental(context.Background(), true)

	var firstLabels []string
	firstCtx := WithTiming(ctx, func(label string, _ time.Duration) {
		firstLabels = append(firstLabels, label)
	})
	first, errs := Generate(firstCtx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("first Generate returned errors: %v", errs)
	}
	if len(first) != 1 || len(first[0].Errs) > 0 {
		t.Fatalf("unexpected first Generate result: %+v", first)
	}
	if !containsLabel(firstLabels, "load.packages.lazy.load") {
		t.Fatalf("expected first incremental generate to perform lazy load, labels=%v", firstLabels)
	}

	writeFile(t, depFile, strings.Join([]string{
		"package dep",
		"",
		"type Foo struct { Message string; Count int }",
		"",
		"func NewMessage() string { return \"a\" }",
		"",
		"func NewCount() int { return 7 }",
		"",
		"func New(msg string, count int) *Foo {",
		"\treturn &Foo{Message: msg, Count: count}",
		"}",
		"",
	}, "\n"))
	writeFile(t, wireFile, strings.Join([]string{
		"package dep",
		"",
		"import \"github.com/goforj/wire\"",
		"",
		"var NewSet = wire.NewSet(NewMessage, NewCount, New)",
		"",
	}, "\n"))

	var secondLabels []string
	secondCtx := WithTiming(ctx, func(label string, _ time.Duration) {
		secondLabels = append(secondLabels, label)
	})
	second, errs := Generate(secondCtx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("second Generate returned errors: %v", errs)
	}
	if len(second) != 1 || len(second[0].Errs) > 0 {
		t.Fatalf("unexpected second Generate result: %+v", second)
	}
	if containsLabel(secondLabels, "generate.load") {
		t.Fatalf("expected shape-changing incremental run to skip package load via local fast path, labels=%v", secondLabels)
	}
	if containsLabel(secondLabels, "load.packages.lazy.load") {
		t.Fatalf("expected shape-changing incremental run to skip lazy load via local fast path, labels=%v", secondLabels)
	}
	if !containsLabel(secondLabels, "incremental.local_fastpath.load") {
		t.Fatalf("expected shape-changing incremental run to use local fast path, labels=%v", secondLabels)
	}
	if string(first[0].Content) == string(second[0].Content) {
		t.Fatal("expected shape-changing edit to regenerate different output")
	}
}

func TestGenerateIncrementalRepeatedShapeStateHitsPreloadManifest(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	cacheRoot := t.TempDir()
	osTempDir = func() string { return cacheRoot }

	repoRoot := mustRepoRoot(t)
	root := t.TempDir()

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
		"func Init() *dep.Foo {",
		"\twire.Build(dep.NewSet)",
		"\treturn nil",
		"}",
		"",
	}, "\n"))

	depFile := filepath.Join(root, "dep", "dep.go")
	writeFile(t, depFile, strings.Join([]string{
		"package dep",
		"",
		"type Foo struct { Message string; Count int }",
		"",
		"func NewMessage() string { return \"a\" }",
		"",
		"func NewCount() int { return 7 }",
		"",
		"func New(msg string, count int) *Foo {",
		"\treturn &Foo{Message: msg, Count: count}",
		"}",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "dep", "wire.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"github.com/goforj/wire\"",
		"",
		"var NewSet = wire.NewSet(NewMessage, NewCount, New)",
		"",
	}, "\n"))

	env := append(os.Environ(), "GOWORK=off")
	ctx := WithIncremental(context.Background(), true)

	first, errs := Generate(ctx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("first Generate returned errors: %v", errs)
	}
	if len(first) != 1 || len(first[0].Errs) > 0 {
		t.Fatalf("unexpected first Generate result: %+v", first)
	}

	var secondLabels []string
	secondCtx := WithTiming(ctx, func(label string, _ time.Duration) {
		secondLabels = append(secondLabels, label)
	})
	second, errs := Generate(secondCtx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("second Generate returned errors: %v", errs)
	}
	if len(second) != 1 || len(second[0].Errs) > 0 {
		t.Fatalf("unexpected second Generate result: %+v", second)
	}
	if containsLabel(secondLabels, "generate.load") {
		t.Fatalf("expected repeated shape state to hit preload manifest before package load, labels=%v", secondLabels)
	}
	if containsLabel(secondLabels, "load.packages.lazy.load") {
		t.Fatalf("expected repeated shape state to skip lazy load, labels=%v", secondLabels)
	}
	if string(first[0].Content) != string(second[0].Content) {
		t.Fatal("expected repeated shape state to reuse identical generated output")
	}
}

func TestGenerateIncrementalShapeChangeThenRepeatHitsPreloadManifest(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	cacheRoot := t.TempDir()
	osTempDir = func() string { return cacheRoot }

	repoRoot := mustRepoRoot(t)
	root := t.TempDir()

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
		"func Init() *dep.Foo {",
		"\twire.Build(dep.NewSet)",
		"\treturn nil",
		"}",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "extra", "extra.go"), strings.Join([]string{
		"package extra",
		"",
		"type Marker struct{}",
		"",
	}, "\n"))

	depFile := filepath.Join(root, "dep", "dep.go")
	wireFile := filepath.Join(root, "dep", "wire.go")
	writeFile(t, depFile, strings.Join([]string{
		"package dep",
		"",
		"type Foo struct { Message string }",
		"",
		"func NewMessage() string { return \"a\" }",
		"",
		"func New(msg string) *Foo {",
		"\treturn &Foo{Message: msg}",
		"}",
		"",
	}, "\n"))
	writeFile(t, wireFile, strings.Join([]string{
		"package dep",
		"",
		"import \"github.com/goforj/wire\"",
		"",
		"var NewSet = wire.NewSet(NewMessage, New)",
		"",
	}, "\n"))

	env := append(os.Environ(), "GOWORK=off")
	ctx := WithIncremental(context.Background(), true)

	first, errs := Generate(ctx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("first Generate returned errors: %v", errs)
	}
	if len(first) != 1 || len(first[0].Errs) > 0 {
		t.Fatalf("unexpected first Generate result: %+v", first)
	}

	writeFile(t, depFile, strings.Join([]string{
		"package dep",
		"",
		"type Foo struct { Message string; Count int }",
		"",
		"func NewMessage() string { return \"a\" }",
		"",
		"func NewCount() int { return 7 }",
		"",
		"func New(msg string, count int) *Foo {",
		"\treturn &Foo{Message: msg, Count: count}",
		"}",
		"",
	}, "\n"))
	writeFile(t, wireFile, strings.Join([]string{
		"package dep",
		"",
		"import \"github.com/goforj/wire\"",
		"",
		"var NewSet = wire.NewSet(NewMessage, NewCount, New)",
		"",
	}, "\n"))

	var secondLabels []string
	secondCtx := WithTiming(ctx, func(label string, _ time.Duration) {
		secondLabels = append(secondLabels, label)
	})
	second, errs := Generate(secondCtx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("second Generate returned errors: %v", errs)
	}
	if len(second) != 1 || len(second[0].Errs) > 0 {
		t.Fatalf("unexpected second Generate result: %+v", second)
	}
	if containsLabel(secondLabels, "generate.load") {
		t.Fatalf("expected shape-changing Generate to skip package load via local fast path, labels=%v", secondLabels)
	}
	if containsLabel(secondLabels, "load.packages.lazy.load") {
		t.Fatalf("expected shape-changing Generate to skip lazy load via local fast path, labels=%v", secondLabels)
	}
	if !containsLabel(secondLabels, "incremental.local_fastpath.load") {
		t.Fatalf("expected shape-changing Generate to use local fast path, labels=%v", secondLabels)
	}

	var thirdLabels []string
	thirdCtx := WithTiming(ctx, func(label string, _ time.Duration) {
		thirdLabels = append(thirdLabels, label)
	})
	third, errs := Generate(thirdCtx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("third Generate returned errors: %v", errs)
	}
	if len(third) != 1 || len(third[0].Errs) > 0 {
		t.Fatalf("unexpected third Generate result: %+v", third)
	}
	if containsLabel(thirdLabels, "generate.load") {
		t.Fatalf("expected repeated shape-changing state to hit preload manifest before package load, labels=%v", thirdLabels)
	}
	if containsLabel(thirdLabels, "load.packages.lazy.load") {
		t.Fatalf("expected repeated shape-changing state to skip lazy load, labels=%v", thirdLabels)
	}
	if string(second[0].Content) != string(third[0].Content) {
		t.Fatal("expected repeated shape-changing state to reuse identical generated output")
	}
}

func TestGenerateIncrementalShapeChangeMatchesNormalGenerate(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	cacheRoot := t.TempDir()
	osTempDir = func() string { return cacheRoot }

	repoRoot := mustRepoRoot(t)
	root := t.TempDir()

	writeIncrementalBenchmarkModule(t, repoRoot, root)

	env := append(os.Environ(), "GOWORK=off")
	incrementalCtx := WithIncremental(context.Background(), true)

	if _, errs := Generate(incrementalCtx, root, env, []string{"./app"}, &GenerateOptions{}); len(errs) > 0 {
		t.Fatalf("baseline incremental Generate returned errors: %v", errs)
	}

	writeFile(t, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"type Foo struct { Message string; Count int }",
		"",
		"func NewMessage() string { return \"ok\" }",
		"",
		"func NewCount() int { return 7 }",
		"",
		"func New(msg string, count int) *Foo {",
		"\treturn &Foo{Message: msg, Count: count}",
		"}",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "dep", "wire.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"github.com/goforj/wire\"",
		"",
		"var NewSet = wire.NewSet(NewMessage, NewCount, New)",
		"",
	}, "\n"))

	var incrementalLabels []string
	incrementalTimedCtx := WithTiming(incrementalCtx, func(label string, _ time.Duration) {
		incrementalLabels = append(incrementalLabels, label)
	})
	incrementalGens, errs := Generate(incrementalTimedCtx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("incremental shape-change Generate returned errors: %v", errs)
	}
	if len(incrementalGens) != 1 || len(incrementalGens[0].Errs) > 0 {
		t.Fatalf("unexpected incremental Generate results: %+v", incrementalGens)
	}
	if !containsLabel(incrementalLabels, "incremental.local_fastpath.load") {
		t.Fatalf("expected incremental shape-change Generate to use local fast path, labels=%v", incrementalLabels)
	}

	normalGens, errs := Generate(context.Background(), root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("normal Generate returned errors: %v", errs)
	}
	if len(normalGens) != 1 || len(normalGens[0].Errs) > 0 {
		t.Fatalf("unexpected normal Generate results: %+v", normalGens)
	}
	if incrementalGens[0].OutputPath != normalGens[0].OutputPath {
		t.Fatalf("output paths differ: incremental=%q normal=%q", incrementalGens[0].OutputPath, normalGens[0].OutputPath)
	}
	if string(incrementalGens[0].Content) != string(normalGens[0].Content) {
		t.Fatal("shape-changing incremental output differs from normal Generate output")
	}
}

func TestGenerateIncrementalShapeChangeWithUnchangedDependentPackageMatchesNormalGenerate(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	cacheRoot := t.TempDir()
	osTempDir = func() string { return cacheRoot }

	repoRoot := mustRepoRoot(t)
	root := t.TempDir()

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
		"\t\"example.com/app/router\"",
		"\t\"github.com/goforj/wire\"",
		")",
		"",
		"func Init() *router.Routes {",
		"\twire.Build(dep.Set, router.Set)",
		"\treturn nil",
		"}",
		"",
	}, "\n"))

	writeFile(t, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"type Controller struct { Message string }",
		"",
		"func NewMessage() string { return \"ok\" }",
		"",
		"func NewController(msg string) *Controller {",
		"\treturn &Controller{Message: msg}",
		"}",
		"",
	}, "\n"))

	writeFile(t, filepath.Join(root, "dep", "wire.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"github.com/goforj/wire\"",
		"",
		"var Set = wire.NewSet(NewMessage, NewController)",
		"",
	}, "\n"))

	writeFile(t, filepath.Join(root, "router", "router.go"), strings.Join([]string{
		"package router",
		"",
		"import \"example.com/app/dep\"",
		"",
		"type Routes struct { Controller *dep.Controller }",
		"",
		"func ProvideRoutes(controller *dep.Controller) *Routes {",
		"\treturn &Routes{Controller: controller}",
		"}",
		"",
	}, "\n"))

	writeFile(t, filepath.Join(root, "router", "wire.go"), strings.Join([]string{
		"package router",
		"",
		"import \"github.com/goforj/wire\"",
		"",
		"var Set = wire.NewSet(ProvideRoutes)",
		"",
	}, "\n"))

	env := append(os.Environ(), "GOWORK=off")
	incrementalCtx := WithIncremental(context.Background(), true)

	if _, errs := Generate(incrementalCtx, root, env, []string{"./app"}, &GenerateOptions{}); len(errs) > 0 {
		t.Fatalf("baseline incremental Generate returned errors: %v", errs)
	}

	writeFile(t, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"type Controller struct { Message string; Count int }",
		"",
		"func NewMessage() string { return \"ok\" }",
		"",
		"func NewCount() int { return 7 }",
		"",
		"func NewController(msg string, count int) *Controller {",
		"\treturn &Controller{Message: msg, Count: count}",
		"}",
		"",
	}, "\n"))

	writeFile(t, filepath.Join(root, "dep", "wire.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"github.com/goforj/wire\"",
		"",
		"var Set = wire.NewSet(NewMessage, NewCount, NewController)",
		"",
	}, "\n"))

	var incrementalLabels []string
	incrementalTimedCtx := WithTiming(incrementalCtx, func(label string, _ time.Duration) {
		incrementalLabels = append(incrementalLabels, label)
	})
	incrementalGens, errs := Generate(incrementalTimedCtx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("incremental Generate returned errors: %v", errs)
	}
	if len(incrementalGens) != 1 || len(incrementalGens[0].Errs) > 0 {
		t.Fatalf("unexpected incremental Generate results: %+v", incrementalGens)
	}
	if !containsLabel(incrementalLabels, "incremental.local_fastpath.load") {
		t.Fatalf("expected incremental Generate to use local fast path, labels=%v", incrementalLabels)
	}

	normalGens, errs := Generate(context.Background(), root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("normal Generate returned errors: %v", errs)
	}
	if len(normalGens) != 1 || len(normalGens[0].Errs) > 0 {
		t.Fatalf("unexpected normal Generate results: %+v", normalGens)
	}
	if string(incrementalGens[0].Content) != string(normalGens[0].Content) {
		t.Fatal("incremental output differs from normal Generate output when unchanged package depends on changed package")
	}
}

func TestGenerateIncrementalInvalidShapeChangeDoesNotReuseManifest(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	cacheRoot := t.TempDir()
	osTempDir = func() string { return cacheRoot }

	repoRoot := mustRepoRoot(t)
	root := t.TempDir()

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
		"func Init() *dep.Foo {",
		"\twire.Build(dep.NewSet)",
		"\treturn nil",
		"}",
		"",
	}, "\n"))

	depFile := filepath.Join(root, "dep", "dep.go")
	wireFile := filepath.Join(root, "dep", "wire.go")
	writeFile(t, depFile, strings.Join([]string{
		"package dep",
		"",
		"type Foo struct { Message string }",
		"",
		"func NewMessage() string { return \"a\" }",
		"",
		"func New(msg string) *Foo {",
		"\treturn &Foo{Message: msg}",
		"}",
		"",
	}, "\n"))
	writeFile(t, wireFile, strings.Join([]string{
		"package dep",
		"",
		"import \"github.com/goforj/wire\"",
		"",
		"var NewSet = wire.NewSet(NewMessage, New)",
		"",
	}, "\n"))

	env := append(os.Environ(), "GOWORK=off")
	ctx := WithIncremental(context.Background(), true)

	first, errs := Generate(ctx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("first Generate returned errors: %v", errs)
	}
	if len(first) != 1 || len(first[0].Errs) > 0 {
		t.Fatalf("unexpected first Generate result: %+v", first)
	}

	writeFile(t, depFile, strings.Join([]string{
		"package dep",
		"",
		"import \"example.com/app/extra\"",
		"",
		"type Foo struct { Message string }",
		"",
		"func NewMessage() string { return \"a\" }",
		"",
		"func New(msg string) *Foo {",
		"\treturn &Foo{Message: msg}",
		"}",
		"",
	}, "\n"))

	var secondLabels []string
	secondCtx := WithTiming(ctx, func(label string, _ time.Duration) {
		secondLabels = append(secondLabels, label)
	})
	second, errs := Generate(secondCtx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(second) != 0 {
		t.Fatalf("expected invalid incremental generate to stop before generation, got %+v", second)
	}
	if len(errs) == 0 {
		t.Fatal("expected invalid incremental generate to return errors")
	}
	if containsLabel(secondLabels, "generate.load") {
		t.Fatalf("expected invalid incremental generate to stop before slow-path load, labels=%v", secondLabels)
	}
	if got := errs[0].Error(); !strings.Contains(got, "type-check failed for example.com/app/app") {
		t.Fatalf("expected fast-path type-check error, got %q", got)
	}
}

func TestGenerateIncrementalToggleBackToKnownShapeHitsArchivedPreloadManifest(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	cacheRoot := t.TempDir()
	osTempDir = func() string { return cacheRoot }

	repoRoot := mustRepoRoot(t)
	root := t.TempDir()

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
		"func Init() *dep.Foo {",
		"\twire.Build(dep.NewSet)",
		"\treturn nil",
		"}",
		"",
	}, "\n"))

	depFile := filepath.Join(root, "dep", "dep.go")
	wireFile := filepath.Join(root, "dep", "wire.go")

	oldDep := strings.Join([]string{
		"package dep",
		"",
		"type Foo struct { Message string }",
		"",
		"func NewMessage() string { return \"a\" }",
		"",
		"func New(msg string) *Foo {",
		"\treturn &Foo{Message: msg}",
		"}",
		"",
	}, "\n")
	newDep := strings.Join([]string{
		"package dep",
		"",
		"type Foo struct { Message string; Count int }",
		"",
		"func NewMessage() string { return \"a\" }",
		"",
		"func NewCount() int { return 7 }",
		"",
		"func New(msg string, count int) *Foo {",
		"\treturn &Foo{Message: msg, Count: count}",
		"}",
		"",
	}, "\n")
	oldWire := strings.Join([]string{
		"package dep",
		"",
		"import \"github.com/goforj/wire\"",
		"",
		"var NewSet = wire.NewSet(NewMessage, New)",
		"",
	}, "\n")
	newWire := strings.Join([]string{
		"package dep",
		"",
		"import \"github.com/goforj/wire\"",
		"",
		"var NewSet = wire.NewSet(NewMessage, NewCount, New)",
		"",
	}, "\n")

	writeFile(t, depFile, oldDep)
	writeFile(t, wireFile, oldWire)

	env := append(os.Environ(), "GOWORK=off")
	ctx := WithIncremental(context.Background(), true)

	first, errs := Generate(ctx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("first Generate returned errors: %v", errs)
	}
	if len(first) != 1 || len(first[0].Errs) > 0 {
		t.Fatalf("unexpected first Generate result: %+v", first)
	}

	writeFile(t, depFile, newDep)
	writeFile(t, wireFile, newWire)
	second, errs := Generate(ctx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("second Generate returned errors: %v", errs)
	}
	if len(second) != 1 || len(second[0].Errs) > 0 {
		t.Fatalf("unexpected second Generate result: %+v", second)
	}

	writeFile(t, depFile, oldDep)
	writeFile(t, wireFile, oldWire)

	var thirdLabels []string
	thirdCtx := WithTiming(ctx, func(label string, _ time.Duration) {
		thirdLabels = append(thirdLabels, label)
	})
	third, errs := Generate(thirdCtx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("third Generate returned errors: %v", errs)
	}
	if len(third) != 1 || len(third[0].Errs) > 0 {
		t.Fatalf("unexpected third Generate result: %+v", third)
	}
	if containsLabel(thirdLabels, "generate.load") {
		t.Fatalf("expected toggled-back shape state to hit archived preload manifest before package load, labels=%v", thirdLabels)
	}
	if containsLabel(thirdLabels, "load.packages.lazy.load") {
		t.Fatalf("expected toggled-back shape state to skip lazy load, labels=%v", thirdLabels)
	}
	if string(first[0].Content) != string(third[0].Content) {
		t.Fatal("expected toggled-back shape state to reuse archived generated output")
	}
}

func containsLabel(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}

func mustRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	repoRoot := filepath.Clean(filepath.Join(wd, "..", ".."))
	if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err != nil {
		t.Fatalf("repo root not found at %s: %v", repoRoot, err)
	}
	return repoRoot
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}
