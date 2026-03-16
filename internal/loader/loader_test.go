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
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/tools/go/gcexportdata"
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
	switch got.Backend {
	case ModeCustom:
		if got.FallbackReason != FallbackReasonNone {
			t.Fatalf("fallback reason = %q, want empty for custom backend", got.FallbackReason)
		}
		if got.FallbackDetail != "" {
			t.Fatalf("fallback detail = %q, want empty for custom backend", got.FallbackDetail)
		}
	case ModeFallback:
		if got.FallbackReason != FallbackReasonCustomUnsupported {
			t.Fatalf("fallback reason = %q, want %q", got.FallbackReason, FallbackReasonCustomUnsupported)
		}
		if got.FallbackDetail != "metadata fingerprint mismatch" {
			t.Fatalf("fallback detail = %q, want %q", got.FallbackDetail, "metadata fingerprint mismatch")
		}
	default:
		t.Fatalf("backend = %q, want %q or %q", got.Backend, ModeCustom, ModeFallback)
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

func TestExportDataPairings(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "lib.go", "package lib\n\ntype T int\n", 0)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	pkg, err := new(types.Config).Check("lib", fset, []*ast.File{file}, nil)
	if err != nil {
		t.Fatalf("types.Check() error = %v", err)
	}

	t.Run("gcexportdata write/read direct", func(t *testing.T) {
		var out bytes.Buffer
		if err := gcexportdata.Write(&out, fset, pkg); err != nil {
			t.Fatalf("gcexportdata.Write() error = %v", err)
		}
		got, err := gcexportdata.Read(bytes.NewReader(out.Bytes()), token.NewFileSet(), make(map[string]*types.Package), pkg.Path())
		if err != nil {
			t.Fatalf("gcexportdata.Read() error = %v", err)
		}
		if got.Scope().Lookup("T") == nil {
			t.Fatal("reimported package missing T")
		}
	})

	t.Run("gcexportdata write with newreader fails", func(t *testing.T) {
		var out bytes.Buffer
		if err := gcexportdata.Write(&out, fset, pkg); err != nil {
			t.Fatalf("gcexportdata.Write() error = %v", err)
		}
		if _, err := gcexportdata.NewReader(bytes.NewReader(out.Bytes())); err == nil {
			t.Fatal("gcexportdata.NewReader() unexpectedly succeeded on direct gcexportdata.Write output")
		}
	})
}

func TestExportDataRoundTripWithImports(t *testing.T) {
	fset := token.NewFileSet()
	depPkg, err := new(types.Config).Check("example.com/dep", fset, []*ast.File{
		mustParseFile(t, fset, "dep.go", `package dep

type T int
`),
	}, nil)
	if err != nil {
		t.Fatalf("types.Check(dep) error = %v", err)
	}
	pkg, err := (&types.Config{
		Importer: importerFuncForTest(func(path string) (*types.Package, error) {
			if path == "example.com/dep" {
				return depPkg, nil
			}
			if path == "unsafe" {
				return types.Unsafe, nil
			}
			return nil, nil
		}),
	}).Check("example.com/lib", fset, []*ast.File{
		mustParseFile(t, fset, "lib.go", `package lib

import "example.com/dep"

type T struct {
	S dep.T
}
`),
	}, nil)
	if err != nil {
		t.Fatalf("types.Check() error = %v", err)
	}

	var out bytes.Buffer
	if err := gcexportdata.Write(&out, fset, pkg); err != nil {
		t.Fatalf("gcexportdata.Write() error = %v", err)
	}
	imports := make(map[string]*types.Package)
	got, err := gcexportdata.Read(bytes.NewReader(out.Bytes()), token.NewFileSet(), imports, pkg.Path())
	if err != nil {
		t.Fatalf("gcexportdata.Read() error = %v", err)
	}
	obj := got.Scope().Lookup("T")
	if obj == nil {
		t.Fatal("reimported package missing T")
	}
	named, ok := obj.Type().(*types.Named)
	if !ok {
		t.Fatalf("T type = %T, want *types.Named", obj.Type())
	}
	field := named.Underlying().(*types.Struct).Field(0)
	if field.Type().String() != "example.com/dep.T" {
		t.Fatalf("field type = %q, want %q", field.Type().String(), "example.com/dep.T")
	}
	depImport := imports["example.com/dep"]
	if depImport == nil {
		t.Fatal("imports map missing dep")
	}
	if depImport.Scope().Lookup("T") == nil {
		t.Fatal("dep import missing T after import")
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

func TestLoadTypedPackageGraphCustomExternalArtifactCache(t *testing.T) {
	root := t.TempDir()
	artifactDir := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(root, "app", "wire.go"), "package app\n\nimport \"fmt\"\n\nfunc Init() string { return fmt.Sprint(\"ok\") }\n")

	env := append(os.Environ(),
		loaderArtifactEnv+"=1",
		loaderArtifactDirEnv+"="+artifactDir,
	)

	run := func() int {
		var parseCalls int
		l := New()
		got, err := l.LoadTypedPackageGraph(context.Background(), LazyLoadRequest{
			WD:         root,
			Env:        env,
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
		rootPkg := got.Packages[0]
		if rootPkg.Imports["fmt"] == nil {
			t.Fatal("expected fmt import package")
		}
		return parseCalls
	}

	first := run()
	entries, err := os.ReadDir(artifactDir)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", artifactDir, err)
	}
	if len(entries) == 0 {
		t.Fatal("expected artifact cache files after first run")
	}
	second := run()
	if second >= first {
		t.Fatalf("second parseCalls = %d, want less than first run %d", second, first)
	}
}

func TestLoadTypedPackageGraphCustomExternalArtifactCacheReportsHits(t *testing.T) {
	root := t.TempDir()
	artifactDir := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(root, "app", "wire.go"), "package app\n\nimport \"fmt\"\n\nfunc Init() string { return fmt.Sprint(\"ok\") }\n")
	env := append(os.Environ(),
		loaderArtifactEnv+"=1",
		loaderArtifactDirEnv+"="+artifactDir,
	)

	run := func() []string {
		var labels []string
		ctx := WithTiming(context.Background(), func(label string, _ time.Duration) {
			labels = append(labels, label)
		})
		l := New()
		_, err := l.LoadTypedPackageGraph(ctx, LazyLoadRequest{
			WD:         root,
			Env:        env,
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
		return labels
	}

	_ = run()
	second := run()
	if !hasPrefixLabel(second, "loader.custom.lazy.artifact_hits=") {
		t.Fatalf("second run labels missing artifact hit count: %v", second)
	}
	if !containsPositiveIntLabel(second, "loader.custom.lazy.artifact_hits=") {
		t.Fatalf("second run artifact hit count was not positive: %v", second)
	}
}

func TestLoadTypedPackageGraphCustomExternalArtifactCacheRealAppParity(t *testing.T) {
	root := os.Getenv("WIRE_REAL_APP_ROOT")
	if root == "" {
		t.Skip("WIRE_REAL_APP_ROOT not set")
	}
	artifactDir := t.TempDir()
	load := func(env []string) (map[string]*packages.Package, error) {
		l := New()
		got, err := l.LoadPackages(context.Background(), PackageLoadRequest{
			WD:         root,
			Env:        env,
			Patterns:   []string{"."},
			Mode:       packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax | packages.NeedExportFile,
			LoaderMode: ModeCustom,
			Fset:       token.NewFileSet(),
			ParseFile: func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
				return parser.ParseFile(fset, filename, src, parser.ParseComments|parser.SkipObjectResolution)
			},
		})
		if err != nil {
			return nil, err
		}
		return collectGraph(got.Packages), nil
	}

	base, err := load(os.Environ())
	if err != nil {
		t.Fatalf("base load error = %v", err)
	}
	withArtifactsEnv := append(os.Environ(),
		loaderArtifactEnv+"=1",
		loaderArtifactDirEnv+"="+artifactDir,
	)
	firstArtifact, err := load(withArtifactsEnv)
	if err != nil {
		t.Fatalf("first artifact load error = %v", err)
	}
	secondArtifact, err := load(withArtifactsEnv)
	if err != nil {
		t.Fatalf("second artifact load error = %v", err)
	}
	if len(base) != len(firstArtifact) {
		t.Fatalf("first artifact graph size = %d, want %d", len(firstArtifact), len(base))
	}
	if len(base) != len(secondArtifact) {
		var missing []string
		for path := range base {
			if secondArtifact[path] == nil {
				missing = append(missing, path)
			}
		}
		sort.Strings(missing)
		parents := make(map[string][]string)
		for parentPath, pkg := range base {
			for impPath := range pkg.Imports {
				if secondArtifact[impPath] == nil {
					parents[impPath] = append(parents[impPath], parentPath)
				}
			}
		}
		parentSummary := make([]string, 0, 5)
		for _, path := range missing {
			if len(parentSummary) == 5 {
				break
			}
			importers := append([]string(nil), parents[path]...)
			sort.Strings(importers)
			if len(importers) > 3 {
				importers = importers[:3]
			}
			parentSummary = append(parentSummary, path+" <- "+strings.Join(importers, ","))
		}
		if len(missing) > 20 {
			missing = missing[:20]
		}
		secondParent := secondArtifact["github.com/shirou/gopsutil/v4/internal/common"]
		secondParentImports := []string(nil)
		if secondParent != nil {
			secondParentImports = sortedImportPaths(secondParent.Imports)
		}
		internalCommonParents := append([]string(nil), parents["github.com/shirou/gopsutil/v4/internal/common"]...)
		sort.Strings(internalCommonParents)
		t.Fatalf("second artifact graph size = %d, want %d; missing sample=%v; parent sample=%v; gopsutil/internal/common parents=%v; gopsutil/internal/common imports on second run=%v", len(secondArtifact), len(base), missing, parentSummary, internalCommonParents, secondParentImports)
	}
	if compiledFileCount(base) != compiledFileCount(secondArtifact) {
		t.Fatalf("second artifact compiled file count = %d, want %d", compiledFileCount(secondArtifact), compiledFileCount(base))
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

func compiledFileCount(pkgs map[string]*packages.Package) int {
	total := 0
	for _, pkg := range pkgs {
		total += len(pkg.CompiledGoFiles)
	}
	return total
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

type importerFuncForTest func(string) (*types.Package, error)

func (f importerFuncForTest) Import(path string) (*types.Package, error) {
	return f(path)
}

func mustParseFile(t *testing.T, fset *token.FileSet, filename, src string) *ast.File {
	t.Helper()
	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("ParseFile(%q) error = %v", filename, err)
	}
	return file
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

func hasPrefixLabel(labels []string, prefix string) bool {
	for _, label := range labels {
		if strings.HasPrefix(label, prefix) {
			return true
		}
	}
	return false
}

func containsPositiveIntLabel(labels []string, prefix string) bool {
	for _, label := range labels {
		if !strings.HasPrefix(label, prefix) {
			continue
		}
		value := strings.TrimPrefix(label, prefix)
		n, err := strconv.Atoi(value)
		if err == nil && n > 0 {
			return true
		}
	}
	return false
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
