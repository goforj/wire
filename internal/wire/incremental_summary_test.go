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

func TestIncrementalSummaryEncodeDecodeRoundTrip(t *testing.T) {
	summary := &packageSummary{
		Version:      incrementalSummaryVersion,
		WD:           "/tmp/app",
		Tags:         "dev",
		PkgPath:      "example.com/app/dep",
		ShapeHash:    "abc123",
		LocalImports: []string{"example.com/app/shared"},
		ProviderSets: []providerSetSummary{{
			VarName: "Set",
			Providers: []providerSummary{{
				PkgPath:    "example.com/app/dep",
				Name:       "NewThing",
				Args:       []providerInputSummary{{Type: "string"}},
				Out:        []string{"*example.com/app/dep.Thing"},
				HasCleanup: true,
			}},
			Imports:    []providerSetRefSummary{{PkgPath: "example.com/app/shared", VarName: "SharedSet"}},
			Bindings:   []ifaceBindingSummary{{Iface: "error", Provided: "*example.com/app/dep.Thing"}},
			Values:     []string{"string"},
			Fields:     []fieldSummary{{PkgPath: "example.com/app/dep", Parent: "example.com/app/dep.Config", Name: "Name", Out: []string{"string"}}},
			InputTypes: []string{"context.Context"},
		}},
		Injectors: []injectorSummary{{
			Name:   "Init",
			Inputs: []string{"context.Context"},
			Output: "*example.com/app/dep.Thing",
			Build: providerSetSummary{
				Imports: []providerSetRefSummary{{PkgPath: "example.com/app/shared", VarName: "SharedSet"}},
			},
		}},
	}
	data, err := encodeIncrementalSummary(summary)
	if err != nil {
		t.Fatalf("encodeIncrementalSummary: %v", err)
	}
	got, err := decodeIncrementalSummary(data)
	if err != nil {
		t.Fatalf("decodeIncrementalSummary: %v", err)
	}
	if got.Version != summary.Version || got.PkgPath != summary.PkgPath || got.ShapeHash != summary.ShapeHash {
		t.Fatalf("decoded summary mismatch: %+v", got)
	}
	if len(got.ProviderSets) != 1 || got.ProviderSets[0].VarName != "Set" {
		t.Fatalf("decoded provider sets mismatch: %+v", got.ProviderSets)
	}
	if len(got.Injectors) != 1 || got.Injectors[0].Name != "Init" {
		t.Fatalf("decoded injectors mismatch: %+v", got.Injectors)
	}
}

func TestBuildPackageSummary(t *testing.T) {
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
		"\twire.Build(dep.Set)",
		"\treturn nil",
		"}",
		"",
	}, "\n"))

	writeFile(t, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"type Foo struct{ Message string }",
		"",
		"func NewMessage() string { return \"ok\" }",
		"",
		"func New(msg string) *Foo { return &Foo{Message: msg} }",
		"",
	}, "\n"))

	writeFile(t, filepath.Join(root, "dep", "wire.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"github.com/goforj/wire\"",
		"",
		"var Set = wire.NewSet(NewMessage, New)",
		"",
	}, "\n"))

	env := append(os.Environ(), "GOWORK=off")
	ctx := WithIncremental(context.Background(), true)
	pkgs, loader, errs := load(ctx, root, env, "", []string{"./app"})
	if len(errs) > 0 {
		t.Fatalf("load returned errors: %v", errs)
	}
	oc := newObjectCache(pkgs, loader)
	loadedDep, errs := oc.ensurePackage("example.com/app/dep")
	if len(errs) > 0 {
		t.Fatalf("ensurePackage returned errors: %v", errs)
	}
	summary, err := buildPackageSummary(loader, oc, loadedDep)
	if err != nil {
		t.Fatalf("buildPackageSummary: %v", err)
	}
	if summary.PkgPath != "example.com/app/dep" {
		t.Fatalf("summary pkg path = %q", summary.PkgPath)
	}
	if len(summary.ProviderSets) != 1 || summary.ProviderSets[0].VarName != "Set" {
		t.Fatalf("unexpected provider sets: %+v", summary.ProviderSets)
	}
	if len(summary.ProviderSets[0].Providers) != 2 {
		t.Fatalf("unexpected providers: %+v", summary.ProviderSets[0].Providers)
	}
	loadedApp, errs := oc.ensurePackage("example.com/app/app")
	if len(errs) > 0 {
		t.Fatalf("ensurePackage app returned errors: %v", errs)
	}
	appSummary, err := buildPackageSummary(loader, oc, loadedApp)
	if err != nil {
		t.Fatalf("buildPackageSummary app: %v", err)
	}
	if len(appSummary.Injectors) != 1 || appSummary.Injectors[0].Name != "Init" {
		t.Fatalf("unexpected injectors: %+v", appSummary.Injectors)
	}
	if len(appSummary.Injectors[0].Build.Imports) != 1 || appSummary.Injectors[0].Build.Imports[0].PkgPath != "example.com/app/dep" {
		t.Fatalf("unexpected injector imports: %+v", appSummary.Injectors[0].Build.Imports)
	}
}

func TestCollectIncrementalPackageSummariesUsesCacheForUnchanged(t *testing.T) {
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
		"\twire.Build(dep.Set)",
		"\treturn nil",
		"}",
		"",
	}, "\n"))

	depFile := filepath.Join(root, "dep", "dep.go")
	writeFile(t, depFile, strings.Join([]string{
		"package dep",
		"",
		"type Foo struct{ Message string }",
		"",
		"func NewMessage() string { return \"ok\" }",
		"",
		"func New(msg string) *Foo { return &Foo{Message: msg} }",
		"",
	}, "\n"))

	writeFile(t, filepath.Join(root, "dep", "wire.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"github.com/goforj/wire\"",
		"",
		"var Set = wire.NewSet(NewMessage, New)",
		"",
	}, "\n"))

	env := append(os.Environ(), "GOWORK=off")
	ctx := WithIncremental(context.Background(), true)

	gens, errs := Generate(ctx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("Generate returned errors: %v", errs)
	}
	if len(gens) != 1 || len(gens[0].Errs) > 0 {
		t.Fatalf("unexpected Generate result: %+v", gens)
	}
	pkgs, loader, errs := load(ctx, root, env, "", []string{"./app"})
	if len(errs) > 0 {
		t.Fatalf("load returned errors while seeding summaries: %v", errs)
	}
	if _, errs := newObjectCache(pkgs, loader).ensurePackage("example.com/app/app"); len(errs) > 0 {
		t.Fatalf("ensurePackage returned errors while seeding summaries: %v", errs)
	}
	writeIncrementalPackageSummaries(loader, pkgs)

	writeFile(t, depFile, strings.Join([]string{
		"package dep",
		"",
		"type Foo struct{ Message string; Count int }",
		"",
		"func NewMessage() string { return \"ok\" }",
		"",
		"func NewCount() int { return 7 }",
		"",
		"func New(msg string, count int) *Foo { return &Foo{Message: msg, Count: count} }",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "dep", "wire.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"github.com/goforj/wire\"",
		"",
		"var Set = wire.NewSet(NewMessage, NewCount, New)",
		"",
	}, "\n"))

	pkgs, loader, errs = load(ctx, root, env, "", []string{"./app"})
	if len(errs) > 0 {
		t.Fatalf("load returned errors: %v", errs)
	}
	snapshot := collectIncrementalPackageSummaries(loader, pkgs)
	if snapshot == nil {
		t.Fatal("collectIncrementalPackageSummaries returned nil")
	}
	if _, ok := snapshot.Changed["example.com/app/dep"]; !ok {
		t.Fatalf("expected changed dep summary, got %+v", snapshot.Changed)
	}
	if _, ok := snapshot.Unchanged["example.com/app/app"]; !ok {
		t.Fatalf("expected unchanged app summary from cache, got %+v", snapshot.Unchanged)
	}
	if len(snapshot.Unchanged["example.com/app/app"].Injectors) != 1 {
		t.Fatalf("unexpected cached app summary: %+v", snapshot.Unchanged["example.com/app/app"])
	}
	if len(snapshot.Changed["example.com/app/dep"].ProviderSets) != 1 {
		t.Fatalf("unexpected changed dep summary: %+v", snapshot.Changed["example.com/app/dep"])
	}
}
