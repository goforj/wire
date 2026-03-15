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

func TestGenerateIncrementalBodyOnlyChangeUsesPreloadManifestAndReusesOutput(t *testing.T) {
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
		t.Fatalf("expected second Generate to reuse preload manifest after body-only change, labels=%v", secondLabels)
	}
	if string(first[0].Content) != string(second[0].Content) {
		t.Fatal("expected body-only change to reuse identical generated output")
	}
}

func TestGenerateIncrementalTouchedValidationCacheReusesSuccessfulValidation(t *testing.T) {
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
	writeBodyVariant := func(message string) {
		t.Helper()
		writeFile(t, depFile, strings.Join([]string{
			"package dep",
			"",
			"type Foo struct { Message string }",
			"",
			"func NewMessage() string { return \"" + message + "\" }",
			"",
			"func New(msg string) *Foo {",
			"\treturn &Foo{Message: msg}",
			"}",
			"",
		}, "\n"))
	}
	writeBodyVariant("a")

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

	first, errs := Generate(ctx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("first Generate returned errors: %v", errs)
	}
	if len(first) != 1 || len(first[0].Errs) > 0 {
		t.Fatalf("unexpected first Generate result: %+v", first)
	}

	writeBodyVariant("b")

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
		t.Fatalf("expected first body-only variant change to avoid generate.load, labels=%v", secondLabels)
	}
	if containsLabel(secondLabels, "incremental.preload_manifest.validate_touched_cache_hit") {
		t.Fatalf("did not expect first body-only variant change to hit touched validation cache, labels=%v", secondLabels)
	}

	writeBodyVariant("a")
	third, errs := Generate(ctx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("third Generate returned errors: %v", errs)
	}
	if len(third) != 1 || len(third[0].Errs) > 0 {
		t.Fatalf("unexpected third Generate result: %+v", third)
	}

	writeBodyVariant("b")

	var fourthLabels []string
	fourthCtx := WithTiming(ctx, func(label string, _ time.Duration) {
		fourthLabels = append(fourthLabels, label)
	})
	fourth, errs := Generate(fourthCtx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("fourth Generate returned errors: %v", errs)
	}
	if len(fourth) != 1 || len(fourth[0].Errs) > 0 {
		t.Fatalf("unexpected fourth Generate result: %+v", fourth)
	}
	if containsLabel(fourthLabels, "generate.load") {
		t.Fatalf("expected repeated body-only variant change to avoid generate.load, labels=%v", fourthLabels)
	}
	if !containsLabel(fourthLabels, "incremental.preload_manifest.validate_touched_cache_hit") {
		t.Fatalf("expected repeated body-only variant change to hit touched validation cache, labels=%v", fourthLabels)
	}
	if string(first[0].Content) != string(fourth[0].Content) {
		t.Fatal("expected repeated body-only variant change to reuse identical generated output")
	}
}

func TestGenerateIncrementalConstValueChangeUsesPreloadManifestAndReusesOutput(t *testing.T) {
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
		"const SQLText = \"green\"",
		"",
		"type Foo struct { Message string }",
		"",
		"func NewMessage() string { return SQLText }",
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
		"const SQLText = \"blue\"",
		"",
		"type Foo struct { Message string }",
		"",
		"func NewMessage() string { return SQLText }",
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
	if len(errs) > 0 {
		t.Fatalf("second Generate returned errors: %v", errs)
	}
	if len(second) != 1 || len(second[0].Errs) > 0 {
		t.Fatalf("unexpected second Generate result: %+v", second)
	}
	if containsLabel(secondLabels, "generate.load") {
		t.Fatalf("expected const-value change to reuse preload manifest, labels=%v", secondLabels)
	}
	if string(first[0].Content) != string(second[0].Content) {
		t.Fatal("expected const-value change to reuse identical generated output")
	}
}

func TestGenerateIncrementalBodyOnlyInvalidChangeDoesNotReusePreloadManifest(t *testing.T) {
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
		"type Foo struct { Message string }",
		"",
		"func NewMessage() string { return \"a\" }",
		"",
		"func New(msg string) *Foo {",
		"\treturn missing",
		"}",
		"",
	}, "\n"))

	var secondLabels []string
	secondCtx := WithTiming(ctx, func(label string, _ time.Duration) {
		secondLabels = append(secondLabels, label)
	})
	second, errs := Generate(secondCtx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(second) != 0 {
		t.Fatalf("expected invalid body-only change to stop before generation, got %+v", second)
	}
	if len(errs) == 0 {
		t.Fatal("expected invalid body-only change to return errors")
	}
	if !containsLabel(secondLabels, "generate.load") {
		t.Fatalf("expected invalid body-only change to bypass preload manifest and load packages, labels=%v", secondLabels)
	}
	if got := errs[0].Error(); !strings.Contains(got, "undefined: missing") {
		t.Fatalf("expected load/type-check error from invalid body-only change, got %q", got)
	}
}

func TestGenerateIncrementalScenarioMatrix(t *testing.T) {
	t.Parallel()

	type scenarioExpectation struct {
		mode           string
		wantErr        bool
		wantSameOutput bool
	}

	scenarios := []struct {
		name  string
		apply func(t *testing.T, fx incrementalScenarioFixture)
		want  scenarioExpectation
	}{
		{
			name: "comment_only_change_reuses_preload",
			apply: func(t *testing.T, fx incrementalScenarioFixture) {
				writeFile(t, fx.depFile, strings.Join([]string{
					"package dep",
					"",
					"// SQLText controls SQL highlighting in log output.",
					"const SQLText = \"green\"",
					"",
					"var defaultCount = 1",
					"",
					"type Foo struct { Message string }",
					"",
					"func NewMessage() string { return SQLText }",
					"",
					"func helper(msg string) string { return msg }",
					"",
					"func New(msg string) *Foo {",
					"\treturn &Foo{Message: helper(msg)}",
					"}",
					"",
				}, "\n"))
			},
			want: scenarioExpectation{mode: "preload", wantSameOutput: true},
		},
		{
			name: "whitespace_only_change_reuses_preload",
			apply: func(t *testing.T, fx incrementalScenarioFixture) {
				writeFile(t, fx.depFile, strings.Join([]string{
					"package dep",
					"",
					"const SQLText = \"green\"",
					"",
					"var defaultCount = 1",
					"",
					"type Foo struct { Message string }",
					"",
					"func NewMessage() string { return SQLText }",
					"",
					"func helper(msg string) string { return msg }",
					"",
					"",
					"func New(msg string) *Foo {",
					"",
					"\treturn &Foo{Message: helper(msg)}",
					"",
					"}",
					"",
				}, "\n"))
			},
			want: scenarioExpectation{mode: "preload", wantSameOutput: true},
		},
		{
			name: "function_body_change_reuses_preload",
			apply: func(t *testing.T, fx incrementalScenarioFixture) {
				writeFile(t, fx.depFile, strings.Join([]string{
					"package dep",
					"",
					"const SQLText = \"green\"",
					"",
					"var defaultCount = 1",
					"",
					"type Foo struct { Message string }",
					"",
					"func NewMessage() string {",
					"\treturn helper(SQLText)",
					"}",
					"",
					"func helper(msg string) string { return msg }",
					"",
					"func New(msg string) *Foo {",
					"\treturn &Foo{Message: helper(msg)}",
					"}",
					"",
				}, "\n"))
			},
			want: scenarioExpectation{mode: "preload", wantSameOutput: true},
		},
		{
			name: "method_body_change_uses_local_fastpath",
			apply: func(t *testing.T, fx incrementalScenarioFixture) {
				writeFile(t, fx.depFile, strings.Join([]string{
					"package dep",
					"",
					"const SQLText = \"green\"",
					"",
					"var defaultCount = 1",
					"",
					"type Foo struct { Message string }",
					"",
					"func (f Foo) Summary() string {",
					"\treturn helper(f.Message)",
					"}",
					"",
					"func NewMessage() string { return SQLText }",
					"",
					"func helper(msg string) string { return msg }",
					"",
					"func New(msg string) *Foo {",
					"\treturn &Foo{Message: msg}",
					"}",
					"",
				}, "\n"))
			},
			want: scenarioExpectation{mode: "local_fastpath", wantSameOutput: true},
		},
		{
			name: "const_value_change_reuses_preload",
			apply: func(t *testing.T, fx incrementalScenarioFixture) {
				writeFile(t, fx.depFile, strings.Join([]string{
					"package dep",
					"",
					"const SQLText = \"blue\"",
					"",
					"var defaultCount = 1",
					"",
					"type Foo struct { Message string }",
					"",
					"func NewMessage() string { return SQLText }",
					"",
					"func helper(msg string) string { return msg }",
					"",
					"func New(msg string) *Foo {",
					"\treturn &Foo{Message: helper(msg)}",
					"}",
					"",
				}, "\n"))
			},
			want: scenarioExpectation{mode: "preload", wantSameOutput: true},
		},
		{
			name: "var_initializer_change_reuses_preload",
			apply: func(t *testing.T, fx incrementalScenarioFixture) {
				writeFile(t, fx.depFile, strings.Join([]string{
					"package dep",
					"",
					"const SQLText = \"green\"",
					"",
					"var defaultCount = 2",
					"",
					"type Foo struct { Message string }",
					"",
					"func NewMessage() string { return SQLText }",
					"",
					"func helper(msg string) string { return msg }",
					"",
					"func New(msg string) *Foo {",
					"\treturn &Foo{Message: helper(msg)}",
					"}",
					"",
				}, "\n"))
			},
			want: scenarioExpectation{mode: "preload", wantSameOutput: true},
		},
		{
			name: "add_top_level_helper_uses_local_fastpath",
			apply: func(t *testing.T, fx incrementalScenarioFixture) {
				writeFile(t, fx.depFile, strings.Join([]string{
					"package dep",
					"",
					"const SQLText = \"green\"",
					"",
					"var defaultCount = 1",
					"",
					"type Foo struct { Message string }",
					"",
					"func NewMessage() string { return SQLText }",
					"",
					"func helper(msg string) string { return msg }",
					"",
					"func NewTag() string { return \"tag\" }",
					"",
					"func New(msg string) *Foo {",
					"\treturn &Foo{Message: helper(msg)}",
					"}",
					"",
				}, "\n"))
			},
			want: scenarioExpectation{mode: "local_fastpath", wantSameOutput: true},
		},
		{
			name: "import_only_implementation_change_reuses_preload",
			apply: func(t *testing.T, fx incrementalScenarioFixture) {
				writeFile(t, fx.depFile, strings.Join([]string{
					"package dep",
					"",
					"import \"fmt\"",
					"",
					"const SQLText = \"green\"",
					"",
					"var defaultCount = 1",
					"",
					"type Foo struct { Message string }",
					"",
					"func NewMessage() string { return SQLText }",
					"",
					"func helper(msg string) string { return fmt.Sprint(msg) }",
					"",
					"func New(msg string) *Foo {",
					"\treturn &Foo{Message: helper(msg)}",
					"}",
					"",
				}, "\n"))
			},
			want: scenarioExpectation{mode: "preload", wantSameOutput: true},
		},
		{
			name: "signature_change_uses_local_fastpath",
			apply: func(t *testing.T, fx incrementalScenarioFixture) {
				writeFile(t, fx.depFile, strings.Join([]string{
					"package dep",
					"",
					"const SQLText = \"green\"",
					"",
					"var defaultCount = 7",
					"",
					"type Foo struct {",
					"\tMessage string",
					"\tCount   int",
					"}",
					"",
					"func NewMessage() string { return SQLText }",
					"",
					"func NewCount() int { return defaultCount }",
					"",
					"func helper(msg string) string { return msg }",
					"",
					"func New(msg string, count int) *Foo {",
					"\treturn &Foo{Message: helper(msg), Count: count}",
					"}",
					"",
				}, "\n"))
				writeFile(t, fx.wireFile, strings.Join([]string{
					"package dep",
					"",
					"import \"github.com/goforj/wire\"",
					"",
					"var NewSet = wire.NewSet(NewMessage, NewCount, New)",
					"",
				}, "\n"))
			},
			want: scenarioExpectation{mode: "local_fastpath", wantSameOutput: false},
		},
		{
			name: "struct_field_addition_uses_local_fastpath",
			apply: func(t *testing.T, fx incrementalScenarioFixture) {
				writeFile(t, fx.depFile, strings.Join([]string{
					"package dep",
					"",
					"const SQLText = \"green\"",
					"",
					"var defaultCount = 1",
					"",
					"type Foo struct {",
					"\tMessage string",
					"\tCount   int",
					"}",
					"",
					"func NewMessage() string { return SQLText }",
					"",
					"func helper(msg string) string { return msg }",
					"",
					"func New(msg string) *Foo {",
					"\treturn &Foo{Message: helper(msg), Count: defaultCount}",
					"}",
					"",
				}, "\n"))
			},
			want: scenarioExpectation{mode: "local_fastpath", wantSameOutput: true},
		},
		{
			name: "interface_method_addition_uses_local_fastpath",
			apply: func(t *testing.T, fx incrementalScenarioFixture) {
				writeFile(t, fx.depFile, strings.Join([]string{
					"package dep",
					"",
					"const SQLText = \"green\"",
					"",
					"var defaultCount = 1",
					"",
					"type Fooer interface {",
					"\tMessage() string",
					"\tCount() int",
					"}",
					"",
					"type Foo struct { Message string }",
					"",
					"func NewMessage() string { return SQLText }",
					"",
					"func helper(msg string) string { return msg }",
					"",
					"func New(msg string) *Foo {",
					"\treturn &Foo{Message: helper(msg)}",
					"}",
					"",
				}, "\n"))
			},
			want: scenarioExpectation{mode: "local_fastpath", wantSameOutput: true},
		},
		{
			name: "new_source_file_uses_local_fastpath",
			apply: func(t *testing.T, fx incrementalScenarioFixture) {
				writeFile(t, fx.extraFile, strings.Join([]string{
					"package dep",
					"",
					"func NewTag() string { return \"tag\" }",
					"",
				}, "\n"))
			},
			want: scenarioExpectation{mode: "fast", wantSameOutput: true},
		},
		{
			name: "invalid_body_change_falls_back_and_errors",
			apply: func(t *testing.T, fx incrementalScenarioFixture) {
				writeFile(t, fx.depFile, strings.Join([]string{
					"package dep",
					"",
					"const SQLText = \"green\"",
					"",
					"var defaultCount = 1",
					"",
					"type Foo struct { Message string }",
					"",
					"func NewMessage() string { return missing }",
					"",
					"func helper(msg string) string { return msg }",
					"",
					"func New(msg string) *Foo {",
					"\treturn &Foo{Message: helper(msg)}",
					"}",
					"",
				}, "\n"))
			},
			want: scenarioExpectation{mode: "generate_load", wantErr: true},
		},
	}

	for _, scenario := range scenarios {
		scenario := scenario
		t.Run(scenario.name, func(t *testing.T) {
			fx := newIncrementalScenarioFixture(t)

			first, errs := Generate(fx.ctx, fx.root, fx.env, []string{"./app"}, &GenerateOptions{})
			if len(errs) > 0 {
				t.Fatalf("baseline Generate returned errors: %v", errs)
			}
			if len(first) != 1 || len(first[0].Errs) > 0 {
				t.Fatalf("unexpected baseline Generate result: %+v", first)
			}

			scenario.apply(t, fx)

			var labels []string
			timedCtx := WithTiming(fx.ctx, func(label string, _ time.Duration) {
				labels = append(labels, label)
			})
			second, errs := Generate(timedCtx, fx.root, fx.env, []string{"./app"}, &GenerateOptions{})

			if scenario.want.wantErr {
				if len(errs) == 0 {
					t.Fatal("expected Generate to return errors")
				}
				if len(second) != 0 {
					t.Fatalf("expected invalid incremental generate to stop before generation, got %+v", second)
				}
			} else {
				if len(errs) > 0 {
					t.Fatalf("incremental Generate returned errors: %v", errs)
				}
				if len(second) != 1 || len(second[0].Errs) > 0 {
					t.Fatalf("unexpected incremental Generate result: %+v", second)
				}
			}

			switch scenario.want.mode {
			case "preload":
				if containsLabel(labels, "generate.load") {
					t.Fatalf("expected preload reuse without generate.load, labels=%v", labels)
				}
			case "fast":
				if containsLabel(labels, "generate.load") {
					t.Fatalf("expected fast incremental path without generate.load, labels=%v", labels)
				}
			case "local_fastpath":
				if containsLabel(labels, "generate.load") {
					t.Fatalf("expected local fast path without generate.load, labels=%v", labels)
				}
				if containsLabel(labels, "load.packages.lazy.load") {
					t.Fatalf("expected local fast path to skip lazy load, labels=%v", labels)
				}
				if !containsLabel(labels, "incremental.local_fastpath.load") {
					t.Fatalf("expected local fast path load, labels=%v", labels)
				}
			case "generate_load":
				if !containsLabel(labels, "generate.load") {
					t.Fatalf("expected generate.load fallback, labels=%v", labels)
				}
			default:
				t.Fatalf("unknown expected mode %q", scenario.want.mode)
			}

			if scenario.want.wantErr {
				return
			}

			normal, errs := Generate(context.Background(), fx.root, fx.env, []string{"./app"}, &GenerateOptions{})
			if len(errs) > 0 {
				t.Fatalf("normal Generate returned errors after edit: %v", errs)
			}
			if len(normal) != 1 || len(normal[0].Errs) > 0 {
				t.Fatalf("unexpected normal Generate result after edit: %+v", normal)
			}
			if second[0].OutputPath != normal[0].OutputPath {
				t.Fatalf("output paths differ: incremental=%q normal=%q", second[0].OutputPath, normal[0].OutputPath)
			}
			if string(second[0].Content) != string(normal[0].Content) {
				t.Fatalf("incremental output differs from normal output after %s", scenario.name)
			}
			if scenario.want.wantSameOutput && string(first[0].Content) != string(second[0].Content) {
				t.Fatalf("expected generated output to stay unchanged for %s", scenario.name)
			}
			if !scenario.want.wantSameOutput && string(first[0].Content) == string(second[0].Content) {
				t.Fatalf("expected generated output to change for %s", scenario.name)
			}
		})
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

func TestGenerateIncrementalColdBootstrapStillSeedsFastPath(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	cacheRoot := t.TempDir()
	osTempDir = func() string { return cacheRoot }

	repoRoot := mustRepoRoot(t)
	root := t.TempDir()
	writeLargeBenchmarkModule(t, repoRoot, root, 24)

	env := append(os.Environ(), "GOWORK=off")
	ctx := WithIncremental(context.Background(), true)

	if _, errs := Generate(ctx, root, env, []string{"./app"}, &GenerateOptions{}); len(errs) > 0 {
		t.Fatalf("cold bootstrap Generate returned errors: %v", errs)
	}

	mutateLargeBenchmarkModule(t, root, 12)

	var labels []string
	timedCtx := WithTiming(ctx, func(label string, _ time.Duration) {
		labels = append(labels, label)
	})
	gens, errs := Generate(timedCtx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("shape-change Generate returned errors: %v", errs)
	}
	if len(gens) != 1 || len(gens[0].Errs) > 0 {
		t.Fatalf("unexpected Generate results: %+v", gens)
	}
	if !containsLabel(labels, "incremental.local_fastpath.load") {
		t.Fatalf("expected cold bootstrap to seed fast path, labels=%v", labels)
	}
}

func TestLoadLocalPackagesForFastPathImportsUnchangedLocalDependencyFromLocalExport(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	cacheRoot := t.TempDir()
	osTempDir = func() string { return cacheRoot }

	repoRoot := mustRepoRoot(t)
	root := t.TempDir()
	writeDepRouterModule(t, root, repoRoot)

	env := append(os.Environ(), "GOWORK=off")
	incrementalCtx := WithIncremental(context.Background(), true)

	if _, errs := Generate(incrementalCtx, root, env, []string{"./app"}, &GenerateOptions{}); len(errs) > 0 {
		t.Fatalf("baseline incremental Generate returned errors: %v", errs)
	}

	depPkgPath := "example.com/app/dep"
	depExportPath := mustLocalExportPath(t, root, env, depPkgPath)
	if _, err := os.Stat(depExportPath); err != nil {
		t.Fatalf("expected local export artifact at %s: %v", depExportPath, err)
	}

	mutateRouterModule(t, root)

	preloadState, ok := prepareIncrementalPreloadState(context.Background(), root, env, []string{"./app"}, &GenerateOptions{})
	if !ok || preloadState == nil || preloadState.manifest == nil {
		t.Fatal("expected preload state after baseline incremental generate")
	}
	loaded, err := loadLocalPackagesForFastPath(context.Background(), root, "", "example.com/app/app", []string{"example.com/app/router"}, preloadState.currentLocal, preloadState.manifest.ExternalPkgs)
	if err != nil {
		t.Fatalf("loadLocalPackagesForFastPath returned error: %v", err)
	}
	if _, ok := loaded.loader.localExports[depPkgPath]; !ok {
		t.Fatalf("expected %s to be a local export candidate", depPkgPath)
	}
	if _, ok := loaded.loader.sourcePkgs[depPkgPath]; ok {
		t.Fatalf("did not expect %s to be source-loaded", depPkgPath)
	}
	typesPkg, err := loaded.loader.importPackage(depPkgPath)
	if err != nil {
		t.Fatalf("importPackage(%s) returned error: %v", depPkgPath, err)
	}
	if typesPkg == nil || !typesPkg.Complete() {
		t.Fatalf("expected complete imported package for %s, got %#v", depPkgPath, typesPkg)
	}
	if loaded.loader.pkgs[depPkgPath] != nil {
		t.Fatalf("expected %s to avoid source loading when local export artifact is present", depPkgPath)
	}
}

func TestGenerateIncrementalMissingLocalExportFallsBackSafely(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	cacheRoot := t.TempDir()
	osTempDir = func() string { return cacheRoot }

	repoRoot := mustRepoRoot(t)
	root := t.TempDir()
	writeDepRouterModule(t, root, repoRoot)

	env := append(os.Environ(), "GOWORK=off")
	incrementalCtx := WithIncremental(context.Background(), true)

	if _, errs := Generate(incrementalCtx, root, env, []string{"./app"}, &GenerateOptions{}); len(errs) > 0 {
		t.Fatalf("baseline incremental Generate returned errors: %v", errs)
	}

	depExportPath := mustLocalExportPath(t, root, env, "example.com/app/dep")
	if err := os.Remove(depExportPath); err != nil {
		t.Fatalf("Remove(%s) failed: %v", depExportPath, err)
	}

	mutateRouterModule(t, root)

	var labels []string
	timedCtx := WithTiming(incrementalCtx, func(label string, _ time.Duration) {
		labels = append(labels, label)
	})
	gens, errs := Generate(timedCtx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("Generate returned errors: %v", errs)
	}
	if len(gens) != 1 || len(gens[0].Errs) > 0 {
		t.Fatalf("unexpected Generate results: %+v", gens)
	}
	if !containsLabel(labels, "incremental.local_fastpath.load") {
		t.Fatalf("expected missing local export to stay on local fast path, labels=%v", labels)
	}
	refreshedExportPath := mustLocalExportPath(t, root, env, "example.com/app/dep")
	if _, err := os.Stat(refreshedExportPath); err != nil {
		t.Fatalf("expected local export artifact to be refreshed at %s: %v", refreshedExportPath, err)
	}
}

func TestGenerateIncrementalCorruptedLocalExportFallsBackSafely(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	cacheRoot := t.TempDir()
	osTempDir = func() string { return cacheRoot }

	repoRoot := mustRepoRoot(t)
	root := t.TempDir()
	writeDepRouterModule(t, root, repoRoot)

	env := append(os.Environ(), "GOWORK=off")
	incrementalCtx := WithIncremental(context.Background(), true)

	if _, errs := Generate(incrementalCtx, root, env, []string{"./app"}, &GenerateOptions{}); len(errs) > 0 {
		t.Fatalf("baseline incremental Generate returned errors: %v", errs)
	}

	depExportPath := mustLocalExportPath(t, root, env, "example.com/app/dep")
	if err := os.WriteFile(depExportPath, []byte("not-a-valid-export"), 0644); err != nil {
		t.Fatalf("WriteFile(%s) failed: %v", depExportPath, err)
	}

	mutateRouterModule(t, root)

	var labels []string
	timedCtx := WithTiming(incrementalCtx, func(label string, _ time.Duration) {
		labels = append(labels, label)
	})
	gens, errs := Generate(timedCtx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("Generate returned errors: %v", errs)
	}
	if len(gens) != 1 || len(gens[0].Errs) > 0 {
		t.Fatalf("unexpected Generate results: %+v", gens)
	}
	if !containsLabel(labels, "incremental.local_fastpath.load") {
		t.Fatalf("expected corrupted local export to stay on local fast path, labels=%v", labels)
	}
	refreshedExportPath := mustLocalExportPath(t, root, env, "example.com/app/dep")
	data, err := os.ReadFile(refreshedExportPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) failed: %v", refreshedExportPath, err)
	}
	if string(data) == "not-a-valid-export" {
		t.Fatalf("expected corrupted local export artifact to be refreshed at %s", refreshedExportPath)
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
	if got := errs[0].Error(); !strings.Contains(got, "type-check failed for example.com/app/app") {
		t.Fatalf("expected fast-path type-check error, got %q", got)
	}
	if _, ok := readIncrementalManifest(incrementalManifestSelectorKey(root, env, []string{"./app"}, &GenerateOptions{})); ok {
		t.Fatal("expected invalid incremental generate to invalidate selector manifest")
	}
}

func TestGenerateIncrementalRecoversAfterInvalidShapeChange(t *testing.T) {
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

	second, errs := Generate(ctx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(second) != 0 {
		t.Fatalf("expected invalid incremental generate to stop before generation, got %+v", second)
	}
	if len(errs) == 0 {
		t.Fatal("expected invalid incremental generate to return errors")
	}
	clearIncrementalSessions()

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

	var thirdLabels []string
	thirdCtx := WithTiming(ctx, func(label string, _ time.Duration) {
		thirdLabels = append(thirdLabels, label)
	})
	third, errs := Generate(thirdCtx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("recovery incremental Generate returned errors: %v", errs)
	}
	if len(third) != 1 || len(third[0].Errs) > 0 {
		t.Fatalf("unexpected recovery incremental Generate result: %+v", third)
	}

	normal, errs := Generate(context.Background(), root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("normal Generate returned errors: %v", errs)
	}
	if len(normal) != 1 || len(normal[0].Errs) > 0 {
		t.Fatalf("unexpected normal Generate result: %+v", normal)
	}
	if string(third[0].Content) != string(normal[0].Content) {
		t.Fatal("incremental output differs from normal Generate output after recovering from invalid shape change")
	}
	if !containsLabel(thirdLabels, "incremental.local_fastpath.load") && !containsLabel(thirdLabels, "generate.load") {
		t.Fatalf("expected recovery run to rebuild through local fast path or normal load, labels=%v", thirdLabels)
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

func TestGenerateIncrementalPreloadHitRefreshesMissingContentHashes(t *testing.T) {
	fx := newIncrementalScenarioFixture(t)

	first, errs := Generate(fx.ctx, fx.root, fx.env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("baseline Generate returned errors: %v", errs)
	}
	if len(first) != 1 || len(first[0].Errs) > 0 {
		t.Fatalf("unexpected baseline Generate result: %+v", first)
	}

	selectorKey := incrementalManifestSelectorKey(fx.root, fx.env, []string{"./app"}, &GenerateOptions{})
	manifest, ok := readIncrementalManifest(selectorKey)
	if !ok {
		t.Fatal("expected incremental manifest after baseline generate")
	}
	if len(manifest.LocalPackages) == 0 {
		t.Fatal("expected local packages in incremental manifest")
	}

	stale := *manifest
	stale.LocalPackages = append([]packageFingerprint(nil), manifest.LocalPackages...)
	for i := range stale.LocalPackages {
		stale.LocalPackages[i].ContentHash = ""
		stale.LocalPackages[i].Dirs = nil
	}
	writeIncrementalManifestFile(selectorKey, &stale)
	writeIncrementalManifestFile(incrementalManifestStateKey(selectorKey, stale.LocalPackages), &stale)

	second, errs := Generate(fx.ctx, fx.root, fx.env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("refresh Generate returned errors: %v", errs)
	}
	if len(second) != 1 || len(second[0].Errs) > 0 {
		t.Fatalf("unexpected refresh Generate result: %+v", second)
	}

	preloadState, ok := prepareIncrementalPreloadState(fx.ctx, fx.root, fx.env, []string{"./app"}, &GenerateOptions{})
	if !ok {
		t.Fatal("expected preload state after manifest refresh")
	}
	if !preloadState.valid {
		t.Fatalf("expected refreshed preload state to be valid, reason=%s", preloadState.reason)
	}
	if len(preloadState.touched) != 0 {
		t.Fatalf("expected refreshed preload state to have no touched packages, got %v", preloadState.touched)
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

type incrementalScenarioFixture struct {
	root      string
	env       []string
	ctx       context.Context
	depFile   string
	wireFile  string
	extraFile string
}

func newIncrementalScenarioFixture(t *testing.T) incrementalScenarioFixture {
	t.Helper()

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
		"const SQLText = \"green\"",
		"",
		"var defaultCount = 1",
		"",
		"type Foo struct { Message string }",
		"",
		"func NewMessage() string { return SQLText }",
		"",
		"func helper(msg string) string { return msg }",
		"",
		"func New(msg string) *Foo {",
		"\treturn &Foo{Message: helper(msg)}",
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

	return incrementalScenarioFixture{
		root:      root,
		env:       append(os.Environ(), "GOWORK=off"),
		ctx:       WithIncremental(context.Background(), true),
		depFile:   depFile,
		wireFile:  wireFile,
		extraFile: filepath.Join(root, "dep", "extra.go"),
	}
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

func writeDepRouterModule(t *testing.T, root string, repoRoot string) {
	t.Helper()
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
}

func mutateRouterModule(t *testing.T, root string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "router", "router.go"), strings.Join([]string{
		"package router",
		"",
		"import \"example.com/app/dep\"",
		"",
		"type Routes struct {",
		"\tController *dep.Controller",
		"\tVersion int",
		"}",
		"",
		"func NewVersion() int {",
		"\treturn 2",
		"}",
		"",
		"func ProvideRoutes(controller *dep.Controller, version int) *Routes {",
		"\treturn &Routes{Controller: controller, Version: version}",
		"}",
		"",
	}, "\n"))

	writeFile(t, filepath.Join(root, "router", "wire.go"), strings.Join([]string{
		"package router",
		"",
		"import \"github.com/goforj/wire\"",
		"",
		"var Set = wire.NewSet(NewVersion, ProvideRoutes)",
		"",
	}, "\n"))
}

func mustLocalExportPath(t *testing.T, root string, env []string, pkgPath string) string {
	t.Helper()
	pkgs, loader, errs := load(context.Background(), root, env, "", []string{"./app"})
	if len(errs) > 0 {
		t.Fatalf("load returned errors: %v", errs)
	}
	if loader == nil {
		t.Fatal("load returned nil loader")
	}
	if _, errs := loader.load("example.com/app/app"); len(errs) > 0 {
		t.Fatalf("lazy load returned errors: %v", errs)
	}
	snapshot := buildIncrementalManifestSnapshotFromPackages(root, "", incrementalManifestPackages(pkgs, loader))
	if snapshot == nil || snapshot.fingerprints[pkgPath] == nil {
		t.Fatalf("missing fingerprint for %s", pkgPath)
	}
	path := localExportPathForFingerprint(root, "", snapshot.fingerprints[pkgPath])
	if path == "" {
		t.Fatalf("missing local export path for %s", pkgPath)
	}
	return path
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
