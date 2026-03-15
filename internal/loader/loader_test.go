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

package loader

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestModeFromEnvDefaultAuto(t *testing.T) {
	if got := ModeFromEnv(nil); got != ModeAuto {
		t.Fatalf("ModeFromEnv(nil) = %q, want %q", got, ModeAuto)
	}
}

func TestModeFromEnvUsesLastMatchingValue(t *testing.T) {
	env := []string{
		"WIRE_LOADER_MODE=fallback",
		"OTHER=value",
		"WIRE_LOADER_MODE=custom",
	}
	if got := ModeFromEnv(env); got != ModeCustom {
		t.Fatalf("ModeFromEnv(...) = %q, want %q", got, ModeCustom)
	}
}

func TestModeFromEnvIgnoresInvalidValues(t *testing.T) {
	env := []string{
		"WIRE_LOADER_MODE=invalid",
	}
	if got := ModeFromEnv(env); got != ModeAuto {
		t.Fatalf("ModeFromEnv(...) = %q, want %q", got, ModeAuto)
	}
}

func TestFallbackLoaderReasonFromMode(t *testing.T) {
	l := New()

	gotAuto, err := l.ValidateTouchedPackages(context.Background(), TouchedValidationRequest{
		WD:      ".",
		Env:     []string{},
		Touched: []string{},
		Mode:    ModeAuto,
	})
	if err != nil {
		t.Fatalf("ValidateTouchedPackages(auto) error = %v", err)
	}
	if gotAuto.Backend != ModeCustom {
		t.Fatalf("auto backend = %q, want %q", gotAuto.Backend, ModeCustom)
	}
	if gotAuto.FallbackReason != FallbackReasonNone {
		t.Fatalf("auto fallback reason = %q, want none", gotAuto.FallbackReason)
	}

	gotForced, err := l.ValidateTouchedPackages(context.Background(), TouchedValidationRequest{
		WD:      ".",
		Env:     []string{},
		Touched: []string{},
		Mode:    ModeFallback,
	})
	if err != nil {
		t.Fatalf("ValidateTouchedPackages(fallback) error = %v", err)
	}
	if gotForced.FallbackReason != FallbackReasonForcedFallback {
		t.Fatalf("forced fallback reason = %q, want %q", gotForced.FallbackReason, FallbackReasonForcedFallback)
	}
}

func TestCustomTouchedValidationSuccess(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(root, "a", "a.go"), "package a\n\nimport \"fmt\"\n\nfunc Use() string { return fmt.Sprint(\"ok\") }\n")

	l := New()
	got, err := l.ValidateTouchedPackages(context.Background(), TouchedValidationRequest{
		WD:      root,
		Env:     os.Environ(),
		Touched: []string{"example.com/app/a"},
		Mode:    ModeCustom,
	})
	if err != nil {
		t.Fatalf("ValidateTouchedPackages(custom) error = %v", err)
	}
	if got.Backend != ModeCustom {
		t.Fatalf("backend = %q, want %q", got.Backend, ModeCustom)
	}
	if got.FallbackReason != FallbackReasonNone {
		t.Fatalf("fallback reason = %q, want none", got.FallbackReason)
	}
	if len(got.Packages) != 1 {
		t.Fatalf("packages len = %d, want 1", len(got.Packages))
	}
	if len(got.Packages[0].Errors) != 0 {
		t.Fatalf("unexpected package errors: %+v", got.Packages[0].Errors)
	}
}

func TestValidateTouchedPackagesAutoUsesCustomWhenSupported(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(root, "a", "a.go"), "package a\n\nfunc Use() string { return \"ok\" }\n")

	l := New()
	got, err := l.ValidateTouchedPackages(context.Background(), TouchedValidationRequest{
		WD:      root,
		Env:     os.Environ(),
		Touched: []string{"example.com/app/a"},
		Mode:    ModeAuto,
	})
	if err != nil {
		t.Fatalf("ValidateTouchedPackages(auto) error = %v", err)
	}
	if got.Backend != ModeCustom {
		t.Fatalf("backend = %q, want %q", got.Backend, ModeCustom)
	}
	if got.FallbackReason != FallbackReasonNone {
		t.Fatalf("fallback reason = %q, want none", got.FallbackReason)
	}
}

func TestCustomTouchedValidationTypeError(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(root, "a", "a.go"), "package a\n\nfunc Broken() int { return missing }\n")

	l := New()
	got, err := l.ValidateTouchedPackages(context.Background(), TouchedValidationRequest{
		WD:      root,
		Env:     os.Environ(),
		Touched: []string{"example.com/app/a"},
		Mode:    ModeCustom,
	})
	if err != nil {
		t.Fatalf("ValidateTouchedPackages(custom) error = %v", err)
	}
	if got.Backend != ModeCustom {
		t.Fatalf("backend = %q, want %q", got.Backend, ModeCustom)
	}
	if len(got.Packages) != 1 {
		t.Fatalf("packages len = %d, want 1", len(got.Packages))
	}
	if len(got.Packages[0].Errors) == 0 {
		t.Fatal("expected type-check errors")
	}
}

func TestValidateTouchedPackagesCustomMatchesFallback(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(root, "dep", "dep.go"), "package dep\n\ntype T struct{}\nfunc New() *T { return &T{} }\n")
	writeTestFile(t, filepath.Join(root, "app", "app.go"), "package app\n\nimport \"example.com/app/dep\"\n\nfunc Use() *dep.T { return dep.New() }\n")

	l := New()
	custom, err := l.ValidateTouchedPackages(context.Background(), TouchedValidationRequest{
		WD:      root,
		Env:     os.Environ(),
		Touched: []string{"example.com/app/app"},
		Mode:    ModeCustom,
	})
	if err != nil {
		t.Fatalf("ValidateTouchedPackages(custom) error = %v", err)
	}
	fallback, err := l.ValidateTouchedPackages(context.Background(), TouchedValidationRequest{
		WD:      root,
		Env:     os.Environ(),
		Touched: []string{"example.com/app/app"},
		Mode:    ModeFallback,
	})
	if err != nil {
		t.Fatalf("ValidateTouchedPackages(fallback) error = %v", err)
	}
	compareRootPackagesOnly(t, custom.Packages, fallback.Packages, false)
}

func TestValidateTouchedPackagesCustomMatchesFallbackTypeErrors(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(root, "a", "a.go"), "package a\n\nfunc Broken() int { return missing }\n")

	l := New()
	custom, err := l.ValidateTouchedPackages(context.Background(), TouchedValidationRequest{
		WD:      root,
		Env:     os.Environ(),
		Touched: []string{"example.com/app/a"},
		Mode:    ModeCustom,
	})
	if err != nil {
		t.Fatalf("ValidateTouchedPackages(custom) error = %v", err)
	}
	fallback, err := l.ValidateTouchedPackages(context.Background(), TouchedValidationRequest{
		WD:      root,
		Env:     os.Environ(),
		Touched: []string{"example.com/app/a"},
		Mode:    ModeFallback,
	})
	if err != nil {
		t.Fatalf("ValidateTouchedPackages(fallback) error = %v", err)
	}
	compareRootPackagesOnly(t, custom.Packages, fallback.Packages, false)
}

func TestValidateTouchedPackagesAutoReportsFallbackDetail(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(root, "a", "a.go"), "package a\n\nfunc Use() string { return \"ok\" }\n")

	l := New()
	got, err := l.ValidateTouchedPackages(context.Background(), TouchedValidationRequest{
		WD:      root,
		Env:     os.Environ(),
		Touched: []string{"example.com/app/a"},
		Local: []LocalPackageFingerprint{
			{
				PkgPath:     "example.com/app/a",
				ContentHash: "wrong",
				ShapeHash:   "wrong",
				Files:       []string{filepath.Join(root, "a", "a.go")},
			},
		},
		Mode: ModeAuto,
	})
	if err != nil {
		t.Fatalf("ValidateTouchedPackages(auto) error = %v", err)
	}
	if got.Backend != ModeFallback {
		t.Fatalf("backend = %q, want %q", got.Backend, ModeFallback)
	}
	if got.FallbackReason != FallbackReasonCustomUnsupported {
		t.Fatalf("fallback reason = %q, want %q", got.FallbackReason, FallbackReasonCustomUnsupported)
	}
	if got.FallbackDetail != "metadata fingerprint mismatch" {
		t.Fatalf("fallback detail = %q, want %q", got.FallbackDetail, "metadata fingerprint mismatch")
	}
}

func TestLoadRootGraphFallback(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(root, "app", "app.go"), "package app\n\nimport _ \"fmt\"\n")

	l := New()
	got, err := l.LoadRootGraph(context.Background(), RootLoadRequest{
		WD:       root,
		Env:      os.Environ(),
		Patterns: []string{"./app"},
		NeedDeps: true,
		Mode:     ModeFallback,
	})
	if err != nil {
		t.Fatalf("LoadRootGraph error = %v", err)
	}
	if got.Backend != ModeFallback {
		t.Fatalf("backend = %q, want %q", got.Backend, ModeFallback)
	}
	if got.FallbackReason != FallbackReasonForcedFallback {
		t.Fatalf("fallback reason = %q, want %q", got.FallbackReason, FallbackReasonForcedFallback)
	}
	if len(got.Packages) == 0 {
		t.Fatal("expected loaded root packages")
	}
}

func TestLoadRootGraphCustom(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(root, "app", "app.go"), "package app\n\nimport _ \"example.com/app/dep\"\n")
	writeTestFile(t, filepath.Join(root, "dep", "dep.go"), "package dep\n")

	l := New()
	got, err := l.LoadRootGraph(context.Background(), RootLoadRequest{
		WD:       root,
		Env:      os.Environ(),
		Patterns: []string{"./app"},
		NeedDeps: true,
		Mode:     ModeCustom,
	})
	if err != nil {
		t.Fatalf("LoadRootGraph(custom) error = %v", err)
	}
	if got.Backend != ModeCustom {
		t.Fatalf("backend = %q, want %q", got.Backend, ModeCustom)
	}
	if got.FallbackReason != FallbackReasonNone {
		t.Fatalf("fallback reason = %q, want none", got.FallbackReason)
	}
	if len(got.Packages) != 1 {
		t.Fatalf("packages len = %d, want 1", len(got.Packages))
	}
	if got.Packages[0].Imports["example.com/app/dep"] == nil {
		t.Fatal("expected custom root graph to wire local import dependency")
	}
}

func TestLoadRootGraphAutoUsesCustomWhenSupported(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(root, "app", "app.go"), "package app\n\nimport _ \"example.com/app/dep\"\n")
	writeTestFile(t, filepath.Join(root, "dep", "dep.go"), "package dep\n")

	l := New()
	got, err := l.LoadRootGraph(context.Background(), RootLoadRequest{
		WD:       root,
		Env:      os.Environ(),
		Patterns: []string{"./app"},
		NeedDeps: true,
		Mode:     ModeAuto,
		Fset:     token.NewFileSet(),
	})
	if err != nil {
		t.Fatalf("LoadRootGraph(auto) error = %v", err)
	}
	if got.Backend != ModeCustom {
		t.Fatalf("backend = %q, want %q", got.Backend, ModeCustom)
	}
}

func TestMetaFilesFallsBackToGoFiles(t *testing.T) {
	meta := &packageMeta{
		GoFiles: []string{"a.go", "b.go"},
	}
	got := metaFiles(meta)
	if len(got) != 2 || got[0] != "a.go" || got[1] != "b.go" {
		t.Fatalf("metaFiles(go-only) = %v, want GoFiles fallback", got)
	}

	meta.CompiledGoFiles = []string{"c.go"}
	got = metaFiles(meta)
	if len(got) != 1 || got[0] != "c.go" {
		t.Fatalf("metaFiles(compiled) = %v, want CompiledGoFiles", got)
	}
}

func TestLoadTypedPackageGraphFallback(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(root, "app", "app.go"), "package app\n\nfunc Value() int { return 42 }\n")

	var parseCalls int
	l := New()
	got, err := l.LoadTypedPackageGraph(context.Background(), LazyLoadRequest{
		WD:         root,
		Env:        os.Environ(),
		Package:    "example.com/app/app",
		Mode:       packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax | packages.NeedExportFile,
		LoaderMode: ModeFallback,
		Fset:       token.NewFileSet(),
		ParseFile: func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
			parseCalls++
			return parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
		},
	})
	if err != nil {
		t.Fatalf("LoadTypedPackageGraph error = %v", err)
	}
	if got.Backend != ModeFallback {
		t.Fatalf("backend = %q, want %q", got.Backend, ModeFallback)
	}
	if got.FallbackReason != FallbackReasonForcedFallback {
		t.Fatalf("fallback reason = %q, want %q", got.FallbackReason, FallbackReasonForcedFallback)
	}
	if len(got.Packages) != 1 {
		t.Fatalf("packages len = %d, want 1", len(got.Packages))
	}
	if parseCalls == 0 {
		t.Fatal("expected ParseFile hook to be used")
	}
}

func TestLoadTypedPackageGraphCustom(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(root, "dep", "dep.go"), "package dep\n\ntype T struct{}\nfunc New() *T { return &T{} }\n")
	writeTestFile(t, filepath.Join(root, "app", "wire.go"), "package app\n\nimport \"example.com/app/dep\"\n\nfunc Init() *dep.T { return dep.New() }\n")

	var parseCalls int
	l := New()
	got, err := l.LoadTypedPackageGraph(context.Background(), LazyLoadRequest{
		WD:         root,
		Env:        os.Environ(),
		Package:    "example.com/app/app",
		Mode:       packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax | packages.NeedExportFile,
		LoaderMode: ModeCustom,
		Fset:       token.NewFileSet(),
		ParseFile: func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
			parseCalls++
			return parser.ParseFile(fset, filename, src, parser.ParseComments|parser.SkipObjectResolution)
		},
	})
	if err != nil {
		t.Fatalf("LoadTypedPackageGraph(custom) error = %v", err)
	}
	if got.Backend != ModeCustom {
		t.Fatalf("backend = %q, want %q", got.Backend, ModeCustom)
	}
	if got.FallbackReason != FallbackReasonNone {
		t.Fatalf("fallback reason = %q, want none", got.FallbackReason)
	}
	if len(got.Packages) != 1 {
		t.Fatalf("packages len = %d, want 1", len(got.Packages))
	}
	rootPkg := got.Packages[0]
	if rootPkg.Types == nil || rootPkg.TypesInfo == nil || len(rootPkg.Syntax) == 0 {
		t.Fatalf("root package missing typed syntax: %+v", rootPkg)
	}
	depPkg := rootPkg.Imports["example.com/app/dep"]
	if depPkg == nil || depPkg.Types == nil || len(depPkg.Syntax) == 0 {
		t.Fatalf("dep package missing typed syntax: %+v", depPkg)
	}
	if parseCalls < 2 {
		t.Fatalf("parseCalls = %d, want at least 2", parseCalls)
	}
}

func TestLoadTypedPackageGraphAutoUsesCustomWhenSupported(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(root, "dep", "dep.go"), "package dep\n\ntype T struct{}\nfunc New() *T { return &T{} }\n")
	writeTestFile(t, filepath.Join(root, "app", "wire.go"), "package app\n\nimport \"example.com/app/dep\"\n\nfunc Init() *dep.T { return dep.New() }\n")

	l := New()
	got, err := l.LoadTypedPackageGraph(context.Background(), LazyLoadRequest{
		WD:         root,
		Env:        os.Environ(),
		Package:    "example.com/app/app",
		Mode:       packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax | packages.NeedExportFile,
		LoaderMode: ModeAuto,
		Fset:       token.NewFileSet(),
		ParseFile: func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
			return parser.ParseFile(fset, filename, src, parser.ParseComments|parser.SkipObjectResolution)
		},
	})
	if err != nil {
		t.Fatalf("LoadTypedPackageGraph(auto) error = %v", err)
	}
	if got.Backend != ModeCustom {
		t.Fatalf("backend = %q, want %q", got.Backend, ModeCustom)
	}
}

func TestLoadTypedPackageGraphCustomKeepsExternalPackagesLight(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(root, "app", "wire.go"), "package app\n\nimport \"fmt\"\n\nfunc Init() string { return fmt.Sprint(\"ok\") }\n")

	l := New()
	got, err := l.LoadTypedPackageGraph(context.Background(), LazyLoadRequest{
		WD:         root,
		Env:        os.Environ(),
		Package:    "example.com/app/app",
		Mode:       packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax | packages.NeedExportFile,
		LoaderMode: ModeCustom,
		Fset:       token.NewFileSet(),
		ParseFile: func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
			return parser.ParseFile(fset, filename, src, parser.ParseComments|parser.SkipObjectResolution)
		},
	})
	if err != nil {
		t.Fatalf("LoadTypedPackageGraph(custom) error = %v", err)
	}
	rootPkg := got.Packages[0]
	fmtPkg := rootPkg.Imports["fmt"]
	if fmtPkg == nil {
		t.Fatal("expected fmt import package")
	}
	if fmtPkg.Types == nil {
		t.Fatalf("fmt package missing types: %+v", fmtPkg)
	}
	if fmtPkg.TypesInfo != nil {
		t.Fatalf("fmt package TypesInfo should be nil, got %+v", fmtPkg.TypesInfo)
	}
	if len(fmtPkg.Syntax) != 0 {
		t.Fatalf("fmt package Syntax len = %d, want 0", len(fmtPkg.Syntax))
	}
}

func TestLoadRootGraphCustomMatchesFallback(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(root, "dep", "dep.go"), "package dep\n")
	writeTestFile(t, filepath.Join(root, "app", "wire.go"), "package app\n\nimport _ \"example.com/app/dep\"\n")

	l := New()
	custom, err := l.LoadRootGraph(context.Background(), RootLoadRequest{
		WD:       root,
		Env:      os.Environ(),
		Patterns: []string{"./app"},
		NeedDeps: true,
		Mode:     ModeCustom,
		Fset:     token.NewFileSet(),
	})
	if err != nil {
		t.Fatalf("LoadRootGraph(custom) error = %v", err)
	}
	fallback, err := l.LoadRootGraph(context.Background(), RootLoadRequest{
		WD:       root,
		Env:      os.Environ(),
		Patterns: []string{"./app"},
		NeedDeps: true,
		Mode:     ModeFallback,
		Fset:     token.NewFileSet(),
	})
	if err != nil {
		t.Fatalf("LoadRootGraph(fallback) error = %v", err)
	}
	comparePackageGraphs(t, custom.Packages, fallback.Packages, false)
}

func TestLoadTypedPackageGraphCustomMatchesFallback(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(root, "dep", "dep.go"), "package dep\n\ntype T struct{}\nfunc New() *T { return &T{} }\n")
	writeTestFile(t, filepath.Join(root, "app", "wire.go"), "package app\n\nimport \"example.com/app/dep\"\n\nfunc Init() *dep.T { return dep.New() }\n")

	l := New()
	custom, err := l.LoadTypedPackageGraph(context.Background(), LazyLoadRequest{
		WD:         root,
		Env:        os.Environ(),
		Package:    "example.com/app/app",
		Mode:       packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax | packages.NeedExportFile,
		LoaderMode: ModeCustom,
		Fset:       token.NewFileSet(),
		ParseFile: func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
			return parser.ParseFile(fset, filename, src, parser.ParseComments|parser.SkipObjectResolution)
		},
	})
	if err != nil {
		t.Fatalf("LoadTypedPackageGraph(custom) error = %v", err)
	}
	fallback, err := l.LoadTypedPackageGraph(context.Background(), LazyLoadRequest{
		WD:         root,
		Env:        os.Environ(),
		Package:    "example.com/app/app",
		Mode:       packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax | packages.NeedExportFile,
		LoaderMode: ModeFallback,
		Fset:       token.NewFileSet(),
		ParseFile: func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
			return parser.ParseFile(fset, filename, src, parser.ParseComments|parser.SkipObjectResolution)
		},
	})
	if err != nil {
		t.Fatalf("LoadTypedPackageGraph(fallback) error = %v", err)
	}
	comparePackageGraphs(t, custom.Packages, fallback.Packages, true)
}

func TestLoadTypedPackageGraphCustomMatchesFallbackTypeErrors(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(root, "app", "wire.go"), "package app\n\nfunc Broken() int { return missing }\n")

	l := New()
	custom, err := l.LoadTypedPackageGraph(context.Background(), LazyLoadRequest{
		WD:         root,
		Env:        os.Environ(),
		Package:    "example.com/app/app",
		Mode:       packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax | packages.NeedExportFile,
		LoaderMode: ModeCustom,
		Fset:       token.NewFileSet(),
		ParseFile: func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
			return parser.ParseFile(fset, filename, src, parser.ParseComments|parser.SkipObjectResolution)
		},
	})
	if err != nil {
		t.Fatalf("LoadTypedPackageGraph(custom) error = %v", err)
	}
	fallback, err := l.LoadTypedPackageGraph(context.Background(), LazyLoadRequest{
		WD:         root,
		Env:        os.Environ(),
		Package:    "example.com/app/app",
		Mode:       packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax | packages.NeedExportFile,
		LoaderMode: ModeFallback,
		Fset:       token.NewFileSet(),
		ParseFile: func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
			return parser.ParseFile(fset, filename, src, parser.ParseComments|parser.SkipObjectResolution)
		},
	})
	if err != nil {
		t.Fatalf("LoadTypedPackageGraph(fallback) error = %v", err)
	}
	comparePackageGraphs(t, custom.Packages, fallback.Packages, true)
}

func comparePackageGraphs(t *testing.T, got []*packages.Package, want []*packages.Package, requireTyped bool) {
	t.Helper()
	gotAll := collectGraph(got)
	wantAll := collectGraph(want)
	if len(gotAll) != len(wantAll) {
		t.Fatalf("package graph size = %d, want %d", len(gotAll), len(wantAll))
	}
	for path, wantPkg := range wantAll {
		gotPkg := gotAll[path]
		if gotPkg == nil {
			t.Fatalf("missing package %q in custom graph", path)
		}
		if gotPkg.Name != wantPkg.Name {
			t.Fatalf("package %q name = %q, want %q", path, gotPkg.Name, wantPkg.Name)
		}
		if !equalStrings(gotPkg.CompiledGoFiles, wantPkg.CompiledGoFiles) {
			t.Fatalf("package %q compiled files = %v, want %v", path, gotPkg.CompiledGoFiles, wantPkg.CompiledGoFiles)
		}
		if !equalImportPaths(gotPkg.Imports, wantPkg.Imports) {
			t.Fatalf("package %q imports = %v, want %v", path, sortedImportPaths(gotPkg.Imports), sortedImportPaths(wantPkg.Imports))
		}
		gotErrs := comparableErrors(gotPkg.Errors)
		wantErrs := comparableErrors(wantPkg.Errors)
		if len(gotErrs) != len(wantErrs) {
			t.Fatalf("package %q comparable errors len = %d, want %d; got=%v want=%v", path, len(gotErrs), len(wantErrs), gotErrs, wantErrs)
		}
		for i := range gotErrs {
			if gotErrs[i] != wantErrs[i] {
				t.Fatalf("package %q comparable error[%d] = %q, want %q", path, i, gotErrs[i], wantErrs[i])
			}
		}
		if requireTyped {
			gotTyped := gotPkg.Types != nil && gotPkg.TypesInfo != nil && len(gotPkg.Syntax) > 0
			wantTyped := wantPkg.Types != nil && wantPkg.TypesInfo != nil && len(wantPkg.Syntax) > 0
			if gotTyped != wantTyped {
				t.Fatalf("package %q typed state = %v, want %v", path, gotTyped, wantTyped)
			}
		}
	}
}

func compareRootPackagesOnly(t *testing.T, got []*packages.Package, want []*packages.Package, requireTyped bool) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("root package count = %d, want %d", len(got), len(want))
	}
	gotByPath := make(map[string]*packages.Package, len(got))
	for _, pkg := range got {
		gotByPath[pkg.PkgPath] = pkg
	}
	for _, wantPkg := range want {
		gotPkg := gotByPath[wantPkg.PkgPath]
		if gotPkg == nil {
			t.Fatalf("missing root package %q", wantPkg.PkgPath)
		}
		if gotPkg.Name != wantPkg.Name {
			t.Fatalf("package %q name = %q, want %q", wantPkg.PkgPath, gotPkg.Name, wantPkg.Name)
		}
		if !equalStrings(gotPkg.CompiledGoFiles, wantPkg.CompiledGoFiles) {
			t.Fatalf("package %q compiled files = %v, want %v", wantPkg.PkgPath, gotPkg.CompiledGoFiles, wantPkg.CompiledGoFiles)
		}
		if !equalImportPaths(gotPkg.Imports, wantPkg.Imports) {
			t.Fatalf("package %q imports = %v, want %v", wantPkg.PkgPath, sortedImportPaths(gotPkg.Imports), sortedImportPaths(wantPkg.Imports))
		}
		gotErrs := comparableErrors(gotPkg.Errors)
		wantErrs := comparableErrors(wantPkg.Errors)
		if len(gotErrs) != len(wantErrs) {
			t.Fatalf("package %q comparable errors len = %d, want %d; got=%v want=%v", wantPkg.PkgPath, len(gotErrs), len(wantErrs), gotErrs, wantErrs)
		}
		for i := range gotErrs {
			if gotErrs[i] != wantErrs[i] {
				t.Fatalf("package %q comparable error[%d] = %q, want %q", wantPkg.PkgPath, i, gotErrs[i], wantErrs[i])
			}
		}
		if requireTyped {
			gotTyped := gotPkg.Types != nil && gotPkg.TypesInfo != nil && len(gotPkg.Syntax) > 0
			wantTyped := wantPkg.Types != nil && wantPkg.TypesInfo != nil && len(wantPkg.Syntax) > 0
			if gotTyped != wantTyped {
				t.Fatalf("package %q typed state = %v, want %v", wantPkg.PkgPath, gotTyped, wantTyped)
			}
		}
	}
}

func collectGraph(roots []*packages.Package) map[string]*packages.Package {
	out := make(map[string]*packages.Package)
	stack := append([]*packages.Package(nil), roots...)
	for len(stack) > 0 {
		pkg := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if pkg == nil || out[pkg.PkgPath] != nil {
			continue
		}
		out[pkg.PkgPath] = pkg
		for _, imp := range pkg.Imports {
			stack = append(stack, imp)
		}
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aCopy := append([]string(nil), a...)
	bCopy := append([]string(nil), b...)
	for i := range aCopy {
		aCopy[i] = normalizePathForCompare(aCopy[i])
	}
	for i := range bCopy {
		bCopy[i] = normalizePathForCompare(bCopy[i])
	}
	sort.Strings(aCopy)
	sort.Strings(bCopy)
	for i := range aCopy {
		if aCopy[i] != bCopy[i] {
			return false
		}
	}
	return true
}

func equalImportPaths(a, b map[string]*packages.Package) bool {
	return equalStrings(sortedImportPaths(a), sortedImportPaths(b))
}

func sortedImportPaths(m map[string]*packages.Package) []string {
	out := make([]string, 0, len(m))
	for path := range m {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func normalizePathForCompare(path string) string {
	if path == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil && resolved != "" {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}

func comparableErrors(errs []packages.Error) []string {
	seen := make(map[string]struct{}, len(errs))
	out := make([]string, 0, len(errs))
	add := func(value string) {
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, err := range errs {
		if strings.HasPrefix(err.Msg, "# ") {
			for _, value := range expandSummaryDiagnostics(err.Msg) {
				add(value)
			}
			continue
		}
		pos := normalizeErrorPos(err.Pos)
		add(pos + "|" + err.Msg)
	}
	sort.Strings(out)
	return out
}

func normalizeErrorPos(pos string) string {
	if pos == "" || pos == "-" {
		return pos
	}
	parts := strings.Split(pos, ":")
	if len(parts) < 2 {
		return shortenComparablePath(normalizePathForCompare(pos))
	}
	path := shortenComparablePath(normalizePathForCompare(parts[0]))
	return strings.Join(append([]string{path}, parts[1:]...), ":")
}

func expandSummaryDiagnostics(msg string) []string {
	lines := strings.Split(msg, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if parts := strings.SplitN(line, ": ", 2); len(parts) == 2 {
			pos := normalizeErrorPos(parts[0])
			out = append(out, pos+"|"+parts[1])
			continue
		}
		out = append(out, line)
	}
	return out
}

func shortenComparablePath(path string) string {
	path = filepath.Clean(path)
	parts := strings.Split(path, string(filepath.Separator))
	if len(parts) >= 2 {
		return filepath.Join(parts[len(parts)-2], parts[len(parts)-1])
	}
	return path
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}
