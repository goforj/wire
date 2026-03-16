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
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
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

func TestLoadTypedPackageGraphCustomArtifactCacheReplacedModuleSourceChange(t *testing.T) {
	root := t.TempDir()
	depRoot := filepath.Join(root, "depmod")
	appRoot := filepath.Join(root, "appmod")
	artifactDir := t.TempDir()
	homeDir := t.TempDir()

	writeTestFile(t, filepath.Join(depRoot, "go.mod"), "module example.com/dep\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(depRoot, "dep.go"), "package dep\n\nfunc New() string { return \"ok\" }\n")

	writeTestFile(t, filepath.Join(appRoot, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require example.com/dep v0.0.0",
		"",
		"replace example.com/dep => " + depRoot,
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "app", "app.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/dep\"",
		"",
		"func Use() string {",
		"\treturn dep.New()",
		"}",
		"",
	}, "\n"))

	env := append(os.Environ(),
		"HOME="+homeDir,
		loaderArtifactEnv+"=1",
		loaderArtifactDirEnv+"="+artifactDir,
	)

	load := func(mode Mode) (*LazyLoadResult, error) {
		l := New()
		return l.LoadTypedPackageGraph(context.Background(), LazyLoadRequest{
			WD:         appRoot,
			Env:        env,
			Package:    "example.com/app/app",
			Mode:       packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax | packages.NeedExportFile,
			LoaderMode: mode,
			Fset:       token.NewFileSet(),
			ParseFile: func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
				return parser.ParseFile(fset, filename, src, parser.ParseComments|parser.SkipObjectResolution)
			},
		})
	}

	first, err := load(ModeCustom)
	if err != nil {
		t.Fatalf("first LoadTypedPackageGraph(custom) error = %v", err)
	}
	if len(first.Packages) != 1 || len(first.Packages[0].Errors) != 0 {
		t.Fatalf("first custom load returned errors: %+v", first.Packages)
	}

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(depRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"type Logger interface { Log(string) }",
		"",
		"type NoopLogger struct{}",
		"",
		"func (NoopLogger) Log(string) {}",
		"",
		"func New(Logger) string { return \"ok\" }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "app", "app.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/dep\"",
		"",
		"func Use() string {",
		"\tvar l dep.Logger = dep.NoopLogger{}",
		"\treturn dep.New(l)",
		"}",
		"",
	}, "\n"))

	custom, err := load(ModeCustom)
	if err != nil {
		t.Fatalf("second LoadTypedPackageGraph(custom) error = %v", err)
	}
	if len(custom.Packages) != 1 {
		t.Fatalf("second custom packages len = %d, want 1", len(custom.Packages))
	}
	if got := comparableErrors(custom.Packages[0].Errors); len(got) != 0 {
		t.Fatalf("second custom load returned errors: %v", got)
	}

	fallback, err := load(ModeFallback)
	if err != nil {
		t.Fatalf("second LoadTypedPackageGraph(fallback) error = %v", err)
	}
	compareRootPackagesOnly(t, custom.Packages, fallback.Packages, true)
}

func TestDiscoveryCacheInvalidatesOnGoModResolutionChange(t *testing.T) {
	root := t.TempDir()
	depOneRoot := filepath.Join(root, "dep-one")
	depTwoRoot := filepath.Join(root, "dep-two")
	appRoot := filepath.Join(root, "appmod")
	homeDir := t.TempDir()

	writeTestFile(t, filepath.Join(depOneRoot, "go.mod"), "module example.com/dep\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(depOneRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"func New() string { return \"one\" }",
		"",
	}, "\n"))

	writeTestFile(t, filepath.Join(depTwoRoot, "go.mod"), "module example.com/dep\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(depTwoRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"strings\"",
		"",
		"func New() string { return strings.ToUpper(\"two\") }",
		"",
	}, "\n"))

	writeTestFile(t, filepath.Join(appRoot, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require example.com/dep v0.0.0",
		"",
		"replace example.com/dep => " + depOneRoot,
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "app", "app.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/dep\"",
		"",
		"func Use() string { return dep.New() }",
		"",
	}, "\n"))

	env := append(os.Environ(), "HOME="+homeDir)

	first := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeCustom)
	if len(first.Packages) != 1 || len(first.Packages[0].Errors) != 0 {
		t.Fatalf("first custom load returned errors: %+v", first.Packages)
	}

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(appRoot, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require example.com/dep v0.0.0",
		"",
		"replace example.com/dep => " + depTwoRoot,
		"",
	}, "\n"))

	custom := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeCustom)
	fallback := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeFallback)
	compareRootPackagesOnly(t, custom.Packages, fallback.Packages, true)
	comparePackageByPath(t, custom.Packages, fallback.Packages, "example.com/dep", false)
}

func TestLoadTypedPackageGraphCustomCrossWorkspaceReplaceTargetIsolation(t *testing.T) {
	cacheHome := t.TempDir()
	artifactDir := t.TempDir()
	repoOne := filepath.Join(t.TempDir(), "repo-one")
	repoTwo := filepath.Join(t.TempDir(), "repo-two")

	depOneRoot := filepath.Join(repoOne, "depmod")
	appOneRoot := filepath.Join(repoOne, "appmod")
	depTwoRoot := filepath.Join(repoTwo, "depmod")
	appTwoRoot := filepath.Join(repoTwo, "appmod")

	writeTestFile(t, filepath.Join(depOneRoot, "go.mod"), "module example.com/dep\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(depOneRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"func New() string { return \"one\" }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appOneRoot, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require example.com/dep v0.0.0",
		"",
		"replace example.com/dep => " + depOneRoot,
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appOneRoot, "app", "app.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/dep\"",
		"",
		"func Use() string { return dep.New() }",
		"",
	}, "\n"))

	writeTestFile(t, filepath.Join(depTwoRoot, "go.mod"), "module example.com/dep\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(depTwoRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"strings\"",
		"",
		"func New() string { return strings.ToUpper(\"two\") }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appTwoRoot, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require example.com/dep v0.0.0",
		"",
		"replace example.com/dep => " + depTwoRoot,
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appTwoRoot, "app", "app.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/dep\"",
		"",
		"func Use() string { return dep.New() }",
		"",
	}, "\n"))

	env := append(os.Environ(),
		"HOME="+cacheHome,
		loaderArtifactEnv+"=1",
		loaderArtifactDirEnv+"="+artifactDir,
	)

	warm := loadTypedPackageGraphForTest(t, appOneRoot, env, "example.com/app/app", ModeCustom)
	if len(warm.Packages) != 1 || len(warm.Packages[0].Errors) != 0 {
		t.Fatalf("repo one warm custom load returned errors: %+v", warm.Packages)
	}

	custom := loadTypedPackageGraphForTest(t, appTwoRoot, env, "example.com/app/app", ModeCustom)
	fallback := loadTypedPackageGraphForTest(t, appTwoRoot, env, "example.com/app/app", ModeFallback)
	compareRootPackagesOnly(t, custom.Packages, fallback.Packages, true)
	comparePackageByPath(t, custom.Packages, fallback.Packages, "example.com/dep", false)
}

func TestLoadTypedPackageGraphCustomTransitiveShapeChangeWarmParity(t *testing.T) {
	root := t.TempDir()

	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(root, "b", "b.go"), strings.Join([]string{
		"package b",
		"",
		"type T struct{}",
		"",
		"func New() *T { return &T{} }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(root, "a", "a.go"), strings.Join([]string{
		"package a",
		"",
		"import \"example.com/app/b\"",
		"",
		"func New() *b.T { return b.New() }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(root, "app", "wire.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/app/a\"",
		"",
		"func Init() any { return a.New() }",
		"",
	}, "\n"))

	first := loadTypedPackageGraphForTest(t, root, os.Environ(), "example.com/app/app", ModeCustom)
	if len(first.Packages) != 1 || len(first.Packages[0].Errors) != 0 {
		t.Fatalf("first custom load returned errors: %+v", first.Packages)
	}

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(root, "b", "b.go"), strings.Join([]string{
		"package b",
		"",
		"type T struct{}",
		"",
		"type Logger interface { Log(string) }",
		"",
		"type NoopLogger struct{}",
		"",
		"func (NoopLogger) Log(string) {}",
		"",
		"func New(Logger) *T { return &T{} }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(root, "a", "a.go"), strings.Join([]string{
		"package a",
		"",
		"import \"example.com/app/b\"",
		"",
		"func New() *b.T {",
		"\tvar logger b.Logger = b.NoopLogger{}",
		"\treturn b.New(logger)",
		"}",
		"",
	}, "\n"))

	custom := loadTypedPackageGraphForTest(t, root, os.Environ(), "example.com/app/app", ModeCustom)
	fallback := loadTypedPackageGraphForTest(t, root, os.Environ(), "example.com/app/app", ModeFallback)
	comparePackageGraphs(t, custom.Packages, fallback.Packages, true)
}

func TestLoadTypedPackageGraphCustomReplacePathSwitchInvalidatesCaches(t *testing.T) {
	root := t.TempDir()
	depOneRoot := filepath.Join(root, "dep-one")
	depTwoRoot := filepath.Join(root, "dep-two")
	appRoot := filepath.Join(root, "appmod")
	artifactDir := t.TempDir()
	homeDir := t.TempDir()

	writeTestFile(t, filepath.Join(depOneRoot, "go.mod"), "module example.com/dep\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(depOneRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"func New() string { return \"one\" }",
		"",
	}, "\n"))

	writeTestFile(t, filepath.Join(depTwoRoot, "go.mod"), "module example.com/dep\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(depTwoRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"strings\"",
		"",
		"func New() string { return strings.TrimSpace(\" two \") }",
		"",
	}, "\n"))

	writeTestFile(t, filepath.Join(appRoot, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require example.com/dep v0.0.0",
		"",
		"replace example.com/dep => " + depOneRoot,
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "app", "app.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/dep\"",
		"",
		"func Use() string { return dep.New() }",
		"",
	}, "\n"))

	env := append(os.Environ(),
		"HOME="+homeDir,
		loaderArtifactEnv+"=1",
		loaderArtifactDirEnv+"="+artifactDir,
	)

	first := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeCustom)
	if len(first.Packages) != 1 || len(first.Packages[0].Errors) != 0 {
		t.Fatalf("first custom load returned errors: %+v", first.Packages)
	}

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(appRoot, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require example.com/dep v0.0.0",
		"",
		"replace example.com/dep => " + depTwoRoot,
		"",
	}, "\n"))

	custom := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeCustom)
	fallback := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeFallback)
	compareRootPackagesOnly(t, custom.Packages, fallback.Packages, true)
	comparePackageByPath(t, custom.Packages, fallback.Packages, "example.com/dep", false)
}

func TestLoadTypedPackageGraphCustomDiscoveryCacheReplacedSiblingOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	depRoot := filepath.Join(root, "depmod")
	appRoot := filepath.Join(root, "appmod")
	homeDir := t.TempDir()

	writeTestFile(t, filepath.Join(depRoot, "go.mod"), "module example.com/dep\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(depRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"func New() string { return \"ok\" }",
		"",
	}, "\n"))

	writeTestFile(t, filepath.Join(appRoot, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require example.com/dep v0.0.0",
		"",
		"replace example.com/dep => " + depRoot,
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "app", "app.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/dep\"",
		"",
		"func Use() string { return dep.New() }",
		"",
	}, "\n"))

	env := append(os.Environ(), "HOME="+homeDir)
	rootLoad := loadRootGraphForTest(t, appRoot, env, []string{"./app"}, ModeCustom)
	if rootLoad.Discovery == nil {
		t.Fatal("expected discovery snapshot from custom root load")
	}

	first := loadTypedPackageGraphWithDiscoveryForTest(t, appRoot, env, "example.com/app/app", ModeCustom, rootLoad.Discovery)
	if len(first.Packages) != 1 || len(first.Packages[0].Errors) != 0 {
		t.Fatalf("first custom load returned errors: %+v", first.Packages)
	}

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(depRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"type Logger interface { Log(string) }",
		"",
		"type NoopLogger struct{}",
		"",
		"func (NoopLogger) Log(string) {}",
		"",
		"func New(Logger) string { return \"ok\" }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "app", "app.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/dep\"",
		"",
		"func Use() string {",
		"\tvar logger dep.Logger = dep.NoopLogger{}",
		"\treturn dep.New(logger)",
		"}",
		"",
	}, "\n"))

	custom := loadTypedPackageGraphWithDiscoveryForTest(t, appRoot, env, "example.com/app/app", ModeCustom, rootLoad.Discovery)
	fallback := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeFallback)
	compareRootPackagesOnly(t, custom.Packages, fallback.Packages, true)
	comparePackageByPath(t, custom.Packages, fallback.Packages, "example.com/dep", false)
}

func TestDiscoveryCacheInvalidatesOnGeneratedFileSetChange(t *testing.T) {
	root := t.TempDir()
	homeDir := t.TempDir()

	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"func New() string { return \"base\" }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(root, "app", "wire.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/app/dep\"",
		"",
		"func Init() string { return dep.New() }",
		"",
	}, "\n"))

	env := append(os.Environ(), "HOME="+homeDir)
	first := loadTypedPackageGraphForTest(t, root, env, "example.com/app/app", ModeCustom)
	if len(first.Packages) != 1 || len(first.Packages[0].Errors) != 0 {
		t.Fatalf("first custom load returned errors: %+v", first.Packages)
	}

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(root, "dep", "zz_generated.go"), strings.Join([]string{
		"package dep",
		"",
		"func Generated() string { return \"generated\" }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(root, "app", "wire.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/app/dep\"",
		"",
		"func Init() string { return dep.New() + dep.Generated() }",
		"",
	}, "\n"))

	custom := loadTypedPackageGraphForTest(t, root, env, "example.com/app/app", ModeCustom)
	fallback := loadTypedPackageGraphForTest(t, root, env, "example.com/app/app", ModeFallback)
	comparePackageGraphs(t, custom.Packages, fallback.Packages, true)
}

func TestLoadTypedPackageGraphCustomBodyOnlyEditWarmParity(t *testing.T) {
	root := t.TempDir()
	homeDir := t.TempDir()

	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"fmt\"",
		"",
		"func Message() string {",
		"\treturn fmt.Sprint(\"before\")",
		"}",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(root, "app", "wire.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/app/dep\"",
		"",
		"func Init() string { return dep.Message() }",
		"",
	}, "\n"))

	env := append(os.Environ(), "HOME="+homeDir)
	first := loadTypedPackageGraphForTest(t, root, env, "example.com/app/app", ModeCustom)
	if len(first.Packages) != 1 || len(first.Packages[0].Errors) != 0 {
		t.Fatalf("first custom load returned errors: %+v", first.Packages)
	}

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"fmt\"",
		"",
		"func Message() string {",
		"\treturn fmt.Sprint(\"after\")",
		"}",
		"",
	}, "\n"))

	custom := loadTypedPackageGraphForTest(t, root, env, "example.com/app/app", ModeCustom)
	fallback := loadTypedPackageGraphForTest(t, root, env, "example.com/app/app", ModeFallback)
	compareRootPackagesOnly(t, custom.Packages, fallback.Packages, true)
	comparePackageByPath(t, custom.Packages, fallback.Packages, "example.com/app/dep", true)
}

func TestLoadTypedPackageGraphCustomReplaceNestedModuleParity(t *testing.T) {
	root := t.TempDir()
	appRoot := filepath.Join(root, "appmod")
	depRoot := filepath.Join(appRoot, "third_party", "depmod")
	homeDir := t.TempDir()

	writeTestFile(t, filepath.Join(depRoot, "go.mod"), "module example.com/dep\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(depRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"func New() string { return \"ok\" }",
		"",
	}, "\n"))

	writeTestFile(t, filepath.Join(appRoot, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require example.com/dep v0.0.0",
		"",
		"replace example.com/dep => ./third_party/depmod",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "app", "app.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/dep\"",
		"",
		"func Use() string { return dep.New() }",
		"",
	}, "\n"))

	env := append(os.Environ(), "HOME="+homeDir)
	first := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeCustom)
	if len(first.Packages) != 1 || len(first.Packages[0].Errors) != 0 {
		t.Fatalf("first custom load returned errors: %+v", first.Packages)
	}

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(depRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"type Logger interface { Log(string) }",
		"",
		"type NoopLogger struct{}",
		"",
		"func (NoopLogger) Log(string) {}",
		"",
		"func New(Logger) string { return \"ok\" }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "app", "app.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/dep\"",
		"",
		"func Use() string {",
		"\tvar logger dep.Logger = dep.NoopLogger{}",
		"\treturn dep.New(logger)",
		"}",
		"",
	}, "\n"))

	custom := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeCustom)
	fallback := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeFallback)
	compareRootPackagesOnly(t, custom.Packages, fallback.Packages, true)
	comparePackageByPath(t, custom.Packages, fallback.Packages, "example.com/dep", false)
}

func TestLoadTypedPackageGraphCustomReplaceChainParity(t *testing.T) {
	root := t.TempDir()
	depRoot := filepath.Join(root, "depmod")
	midRoot := filepath.Join(root, "midmod")
	appRoot := filepath.Join(root, "appmod")
	homeDir := t.TempDir()

	writeTestFile(t, filepath.Join(depRoot, "go.mod"), "module example.com/dep\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(depRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"func New() string { return \"ok\" }",
		"",
	}, "\n"))

	writeTestFile(t, filepath.Join(midRoot, "go.mod"), "module example.com/mid\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(midRoot, "mid.go"), strings.Join([]string{
		"package mid",
		"",
		"import \"example.com/dep\"",
		"",
		"func Use() string { return dep.New() }",
		"",
	}, "\n"))

	writeTestFile(t, filepath.Join(appRoot, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require (",
		"\texample.com/dep v0.0.0",
		"\texample.com/mid v0.0.0",
		")",
		"",
		"replace example.com/dep => " + depRoot,
		"replace example.com/mid => " + midRoot,
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "app", "app.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/mid\"",
		"",
		"func Use() string { return mid.Use() }",
		"",
	}, "\n"))

	env := append(os.Environ(), "HOME="+homeDir)
	first := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeCustom)
	if len(first.Packages) != 1 || len(first.Packages[0].Errors) != 0 {
		t.Fatalf("first custom load returned errors: %+v", first.Packages)
	}

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(depRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"type Logger interface { Log(string) }",
		"",
		"type NoopLogger struct{}",
		"",
		"func (NoopLogger) Log(string) {}",
		"",
		"func New(Logger) string { return \"ok\" }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(midRoot, "mid.go"), strings.Join([]string{
		"package mid",
		"",
		"import \"example.com/dep\"",
		"",
		"func Use() string {",
		"\tvar logger dep.Logger = dep.NoopLogger{}",
		"\treturn dep.New(logger)",
		"}",
		"",
	}, "\n"))

	custom := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeCustom)
	fallback := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeFallback)
	compareRootPackagesOnly(t, custom.Packages, fallback.Packages, true)
	comparePackageByPath(t, custom.Packages, fallback.Packages, "example.com/mid", false)
	comparePackageByPath(t, custom.Packages, fallback.Packages, "example.com/dep", false)
}

func TestLoadTypedPackageGraphCustomGoWorkWorkspaceParity(t *testing.T) {
	root := t.TempDir()
	appRoot := filepath.Join(root, "appmod")
	depRoot := filepath.Join(root, "depmod")
	homeDir := t.TempDir()

	writeTestFile(t, filepath.Join(root, "go.work"), strings.Join([]string{
		"go 1.19",
		"",
		"use (",
		"\t./appmod",
		"\t./depmod",
		")",
		"",
	}, "\n"))

	writeTestFile(t, filepath.Join(depRoot, "go.mod"), "module example.com/dep\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(depRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"func New() string { return \"ok\" }",
		"",
	}, "\n"))

	writeTestFile(t, filepath.Join(appRoot, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require example.com/dep v0.0.0",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "app", "app.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/dep\"",
		"",
		"func Use() string { return dep.New() }",
		"",
	}, "\n"))

	env := append(os.Environ(), "HOME="+homeDir)
	first := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeCustom)
	if len(first.Packages) != 1 || len(first.Packages[0].Errors) != 0 {
		t.Fatalf("first custom load returned errors: %+v", first.Packages)
	}

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(depRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"strings\"",
		"",
		"type Logger interface { Log(string) }",
		"",
		"type NoopLogger struct{}",
		"",
		"func (NoopLogger) Log(string) {}",
		"",
		"func New(Logger) string { return strings.TrimSpace(\" ok \") }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "app", "app.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/dep\"",
		"",
		"func Use() string {",
		"\tvar logger dep.Logger = dep.NoopLogger{}",
		"\treturn dep.New(logger)",
		"}",
		"",
	}, "\n"))

	custom := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeCustom)
	fallback := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeFallback)
	compareRootPackagesOnly(t, custom.Packages, fallback.Packages, true)
	comparePackageByPath(t, custom.Packages, fallback.Packages, "example.com/dep", false)
}

func TestLoadTypedPackageGraphCustomCrossWorkspaceModuleIsolation(t *testing.T) {
	cacheHome := t.TempDir()
	repoOne := filepath.Join(t.TempDir(), "repo-one")
	repoTwo := filepath.Join(t.TempDir(), "repo-two")

	writeTestFile(t, filepath.Join(repoOne, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(repoOne, "dep", "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"func Message() string { return \"one\" }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(repoOne, "app", "wire.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/app/dep\"",
		"",
		"func Init() string { return dep.Message() }",
		"",
	}, "\n"))

	writeTestFile(t, filepath.Join(repoTwo, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(repoTwo, "dep", "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"strings\"",
		"",
		"func Message() string { return strings.ToUpper(\"two\") }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(repoTwo, "app", "wire.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/app/dep\"",
		"",
		"func Init() string { return dep.Message() }",
		"",
	}, "\n"))

	env := append(os.Environ(), "HOME="+cacheHome)
	warm := loadTypedPackageGraphForTest(t, repoOne, env, "example.com/app/app", ModeCustom)
	if len(warm.Packages) != 1 || len(warm.Packages[0].Errors) != 0 {
		t.Fatalf("repo one warm custom load returned errors: %+v", warm.Packages)
	}

	custom := loadTypedPackageGraphForTest(t, repoTwo, env, "example.com/app/app", ModeCustom)
	fallback := loadTypedPackageGraphForTest(t, repoTwo, env, "example.com/app/app", ModeFallback)
	compareRootPackagesOnly(t, custom.Packages, fallback.Packages, true)
	comparePackageByPath(t, custom.Packages, fallback.Packages, "example.com/app/dep", true)
}

func TestDiscoveryCacheInvalidatesOnLocalImportChangeEndToEnd(t *testing.T) {
	root := t.TempDir()
	homeDir := t.TempDir()

	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"func Base() string { return \"base\" }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(root, "extra", "extra.go"), strings.Join([]string{
		"package extra",
		"",
		"func Value() string { return \"extra\" }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(root, "app", "wire.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/app/dep\"",
		"",
		"func Init() string { return dep.Base() }",
		"",
	}, "\n"))

	env := append(os.Environ(), "HOME="+homeDir)
	first := loadTypedPackageGraphForTest(t, root, env, "example.com/app/app", ModeCustom)
	if len(first.Packages) != 1 || len(first.Packages[0].Errors) != 0 {
		t.Fatalf("first custom load returned errors: %+v", first.Packages)
	}

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"example.com/app/extra\"",
		"",
		"func Base() string { return extra.Value() }",
		"",
	}, "\n"))

	custom := loadTypedPackageGraphForTest(t, root, env, "example.com/app/app", ModeCustom)
	fallback := loadTypedPackageGraphForTest(t, root, env, "example.com/app/app", ModeFallback)
	comparePackageGraphs(t, custom.Packages, fallback.Packages, true)
}

func TestLoadTypedPackageGraphCustomLocalShapeChangeWarmParity(t *testing.T) {
	root := t.TempDir()
	homeDir := t.TempDir()

	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"type T struct{}",
		"",
		"func New() *T { return &T{} }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(root, "app", "wire.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/app/dep\"",
		"",
		"func Init() *dep.T { return dep.New() }",
		"",
	}, "\n"))

	env := append(os.Environ(), "HOME="+homeDir)
	first := loadTypedPackageGraphForTest(t, root, env, "example.com/app/app", ModeCustom)
	if len(first.Packages) != 1 || len(first.Packages[0].Errors) != 0 {
		t.Fatalf("first custom load returned errors: %+v", first.Packages)
	}

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"type Config struct{}",
		"",
		"type T struct{}",
		"",
		"func New(Config) *T { return &T{} }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(root, "app", "wire.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/app/dep\"",
		"",
		"func Init() *dep.T { return dep.New(dep.Config{}) }",
		"",
	}, "\n"))

	custom := loadTypedPackageGraphForTest(t, root, env, "example.com/app/app", ModeCustom)
	fallback := loadTypedPackageGraphForTest(t, root, env, "example.com/app/app", ModeFallback)
	comparePackageGraphs(t, custom.Packages, fallback.Packages, true)
}

func TestLoadTypedPackageGraphCustomTransitiveBodyOnlyWarmParity(t *testing.T) {
	root := t.TempDir()
	homeDir := t.TempDir()

	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(root, "b", "b.go"), strings.Join([]string{
		"package b",
		"",
		"import \"fmt\"",
		"",
		"func Message() string { return fmt.Sprint(\"before\") }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(root, "a", "a.go"), strings.Join([]string{
		"package a",
		"",
		"import \"example.com/app/b\"",
		"",
		"func Message() string { return b.Message() }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(root, "app", "wire.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/app/a\"",
		"",
		"func Init() string { return a.Message() }",
		"",
	}, "\n"))

	env := append(os.Environ(), "HOME="+homeDir)
	first := loadTypedPackageGraphForTest(t, root, env, "example.com/app/app", ModeCustom)
	if len(first.Packages) != 1 || len(first.Packages[0].Errors) != 0 {
		t.Fatalf("first custom load returned errors: %+v", first.Packages)
	}

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(root, "b", "b.go"), strings.Join([]string{
		"package b",
		"",
		"import \"fmt\"",
		"",
		"func Message() string { return fmt.Sprint(\"after\") }",
		"",
	}, "\n"))

	custom := loadTypedPackageGraphForTest(t, root, env, "example.com/app/app", ModeCustom)
	fallback := loadTypedPackageGraphForTest(t, root, env, "example.com/app/app", ModeFallback)
	compareRootPackagesOnly(t, custom.Packages, fallback.Packages, true)
	comparePackageByPath(t, custom.Packages, fallback.Packages, "example.com/app/a", true)
	comparePackageByPath(t, custom.Packages, fallback.Packages, "example.com/app/b", true)
}

func TestLoadTypedPackageGraphCustomKnownShapeToggleWarmParity(t *testing.T) {
	root := t.TempDir()
	homeDir := t.TempDir()

	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"type Config struct { Name string }",
		"",
		"func New(Config) string { return \"config\" }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(root, "app", "wire.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/app/dep\"",
		"",
		"func Init() string { return dep.New(dep.Config{Name: \"a\"}) }",
		"",
	}, "\n"))

	env := append(os.Environ(), "HOME="+homeDir)
	first := loadTypedPackageGraphForTest(t, root, env, "example.com/app/app", ModeCustom)
	if len(first.Packages) != 1 || len(first.Packages[0].Errors) != 0 {
		t.Fatalf("first custom load returned errors: %+v", first.Packages)
	}

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"type Logger interface { Log(string) }",
		"",
		"type NoopLogger struct{}",
		"",
		"func (NoopLogger) Log(string) {}",
		"",
		"func New(Logger) string { return \"logger\" }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(root, "app", "wire.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/app/dep\"",
		"",
		"func Init() string {",
		"\tvar logger dep.Logger = dep.NoopLogger{}",
		"\treturn dep.New(logger)",
		"}",
		"",
	}, "\n"))

	custom := loadTypedPackageGraphForTest(t, root, env, "example.com/app/app", ModeCustom)
	fallback := loadTypedPackageGraphForTest(t, root, env, "example.com/app/app", ModeFallback)
	comparePackageGraphs(t, custom.Packages, fallback.Packages, true)
}

func TestLoadTypedPackageGraphCustomNewShapeWarmParity(t *testing.T) {
	root := t.TempDir()
	homeDir := t.TempDir()

	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"func New() string { return \"ok\" }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(root, "app", "wire.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/app/dep\"",
		"",
		"func Init() string { return dep.New() }",
		"",
	}, "\n"))

	env := append(os.Environ(), "HOME="+homeDir)
	first := loadTypedPackageGraphForTest(t, root, env, "example.com/app/app", ModeCustom)
	if len(first.Packages) != 1 || len(first.Packages[0].Errors) != 0 {
		t.Fatalf("first custom load returned errors: %+v", first.Packages)
	}

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"type Config struct{}",
		"",
		"func New() string { return \"ok\" }",
		"",
		"func NewWithConfig(Config) string { return \"ok\" }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(root, "app", "wire.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/app/dep\"",
		"",
		"func Init() string { return dep.NewWithConfig(dep.Config{}) }",
		"",
	}, "\n"))

	custom := loadTypedPackageGraphForTest(t, root, env, "example.com/app/app", ModeCustom)
	fallback := loadTypedPackageGraphForTest(t, root, env, "example.com/app/app", ModeFallback)
	comparePackageGraphs(t, custom.Packages, fallback.Packages, true)
}

func TestLoadTypedPackageGraphCustomReplaceTargetBodyOnlyWarmParity(t *testing.T) {
	root := t.TempDir()
	depRoot := filepath.Join(root, "depmod")
	appRoot := filepath.Join(root, "appmod")
	homeDir := t.TempDir()

	writeTestFile(t, filepath.Join(depRoot, "go.mod"), "module example.com/dep\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(depRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"fmt\"",
		"",
		"func Message() string { return fmt.Sprint(\"before\") }",
		"",
	}, "\n"))

	writeTestFile(t, filepath.Join(appRoot, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require example.com/dep v0.0.0",
		"",
		"replace example.com/dep => " + depRoot,
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "app", "app.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/dep\"",
		"",
		"func Use() string { return dep.Message() }",
		"",
	}, "\n"))

	env := append(os.Environ(), "HOME="+homeDir)
	first := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeCustom)
	if len(first.Packages) != 1 || len(first.Packages[0].Errors) != 0 {
		t.Fatalf("first custom load returned errors: %+v", first.Packages)
	}

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(depRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"fmt\"",
		"",
		"func Message() string { return fmt.Sprint(\"after\") }",
		"",
	}, "\n"))

	custom := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeCustom)
	fallback := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeFallback)
	compareRootPackagesOnly(t, custom.Packages, fallback.Packages, true)
	comparePackageByPath(t, custom.Packages, fallback.Packages, "example.com/dep", false)
}

func TestLoadTypedPackageGraphCustomReplaceTargetShapeChangeWarmParity(t *testing.T) {
	root := t.TempDir()
	depRoot := filepath.Join(root, "depmod")
	appRoot := filepath.Join(root, "appmod")
	homeDir := t.TempDir()

	writeTestFile(t, filepath.Join(depRoot, "go.mod"), "module example.com/dep\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(depRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"func New() string { return \"ok\" }",
		"",
	}, "\n"))

	writeTestFile(t, filepath.Join(appRoot, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require example.com/dep v0.0.0",
		"",
		"replace example.com/dep => " + depRoot,
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "app", "app.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/dep\"",
		"",
		"func Use() string { return dep.New() }",
		"",
	}, "\n"))

	env := append(os.Environ(), "HOME="+homeDir)
	first := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeCustom)
	if len(first.Packages) != 1 || len(first.Packages[0].Errors) != 0 {
		t.Fatalf("first custom load returned errors: %+v", first.Packages)
	}

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(depRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"type Logger interface { Log(string) }",
		"",
		"type NoopLogger struct{}",
		"",
		"func (NoopLogger) Log(string) {}",
		"",
		"func New(Logger) string { return \"ok\" }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "app", "app.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/dep\"",
		"",
		"func Use() string {",
		"\tvar logger dep.Logger = dep.NoopLogger{}",
		"\treturn dep.New(logger)",
		"}",
		"",
	}, "\n"))

	custom := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeCustom)
	fallback := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeFallback)
	compareRootPackagesOnly(t, custom.Packages, fallback.Packages, true)
	comparePackageByPath(t, custom.Packages, fallback.Packages, "example.com/dep", false)
}

func TestLoadTypedPackageGraphCustomFixtureAppWarmMutationParity(t *testing.T) {
	root := t.TempDir()
	appRoot := filepath.Join(root, "appmod")
	depRoot := filepath.Join(root, "depmod")
	homeDir := t.TempDir()

	writeTestFile(t, filepath.Join(depRoot, "go.mod"), "module example.com/dep\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(depRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"fmt\"",
		"",
		"func Message() string { return fmt.Sprint(\"dep\") }",
		"",
	}, "\n"))

	writeTestFile(t, filepath.Join(appRoot, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require example.com/dep v0.0.0",
		"",
		"replace example.com/dep => " + depRoot,
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "base", "base.go"), strings.Join([]string{
		"package base",
		"",
		"import \"fmt\"",
		"",
		"func Prefix() string { return fmt.Sprint(\"base:\") }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "gen", "zz_generated.go"), strings.Join([]string{
		"package gen",
		"",
		"func Value() string { return \"generated\" }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "feature", "feature.go"), strings.Join([]string{
		"package feature",
		"",
		"import (",
		"\t\"example.com/app/base\"",
		"\t\"example.com/app/gen\"",
		"\t\"example.com/dep\"",
		")",
		"",
		"func Message() string {",
		"\treturn base.Prefix() + dep.Message() + gen.Value()",
		"}",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "app", "wire.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/app/feature\"",
		"",
		"func Init() string { return feature.Message() }",
		"",
	}, "\n"))

	env := append(os.Environ(), "HOME="+homeDir)
	coldCustom := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeCustom)
	if len(coldCustom.Packages) != 1 || len(coldCustom.Packages[0].Errors) != 0 {
		t.Fatalf("cold custom load returned errors: %+v", coldCustom.Packages)
	}
	coldFallback := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeFallback)
	compareRootPackagesOnly(t, coldCustom.Packages, coldFallback.Packages, true)
	comparePackageByPath(t, coldCustom.Packages, coldFallback.Packages, "example.com/app/feature", true)
	comparePackageByPath(t, coldCustom.Packages, coldFallback.Packages, "example.com/app/gen", true)
	comparePackageByPath(t, coldCustom.Packages, coldFallback.Packages, "example.com/dep", false)

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(depRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"fmt\"",
		"",
		"type Logger interface { Log(string) }",
		"",
		"type NoopLogger struct{}",
		"",
		"func (NoopLogger) Log(string) {}",
		"",
		"func Message(Logger) string { return fmt.Sprint(\"dep2\") }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "gen", "zz_generated.go"), strings.Join([]string{
		"package gen",
		"",
		"func Value() string { return \"generated2\" }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "feature", "feature.go"), strings.Join([]string{
		"package feature",
		"",
		"import (",
		"\t\"example.com/app/base\"",
		"\t\"example.com/app/gen\"",
		"\t\"example.com/dep\"",
		")",
		"",
		"func Message() string {",
		"\tvar logger dep.Logger = dep.NoopLogger{}",
		"\treturn base.Prefix() + dep.Message(logger) + gen.Value()",
		"}",
		"",
	}, "\n"))

	warmCustom := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeCustom)
	warmFallback := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeFallback)
	compareRootPackagesOnly(t, warmCustom.Packages, warmFallback.Packages, true)
	comparePackageByPath(t, warmCustom.Packages, warmFallback.Packages, "example.com/app/feature", true)
	comparePackageByPath(t, warmCustom.Packages, warmFallback.Packages, "example.com/app/gen", true)
	comparePackageByPath(t, warmCustom.Packages, warmFallback.Packages, "example.com/dep", false)
}

func TestLoadTypedPackageGraphCustomSequentialMutationsParity(t *testing.T) {
	root := t.TempDir()
	depRoot := filepath.Join(root, "depmod")
	appRoot := filepath.Join(root, "appmod")
	homeDir := t.TempDir()

	writeTestFile(t, filepath.Join(depRoot, "go.mod"), "module example.com/dep\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(depRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"fmt\"",
		"",
		"func Message() string { return fmt.Sprint(\"dep\") }",
		"",
	}, "\n"))

	writeTestFile(t, filepath.Join(appRoot, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require example.com/dep v0.0.0",
		"",
		"replace example.com/dep => " + depRoot,
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "helper", "helper.go"), strings.Join([]string{
		"package helper",
		"",
		"func Prefix() string { return \"prefix:\" }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "app", "wire.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/dep\"",
		"",
		"func Init() string { return dep.Message() }",
		"",
	}, "\n"))

	env := append(os.Environ(), "HOME="+homeDir)

	assertParity := func() {
		custom := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeCustom)
		fallback := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeFallback)
		compareRootPackagesOnly(t, custom.Packages, fallback.Packages, true)
		comparePackageByPath(t, custom.Packages, fallback.Packages, "example.com/dep", false)
	}

	initial := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeCustom)
	if len(initial.Packages) != 1 || len(initial.Packages[0].Errors) != 0 {
		t.Fatalf("initial custom load returned errors: %+v", initial.Packages)
	}
	assertParity()

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(depRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"fmt\"",
		"",
		"func Message() string { return fmt.Sprint(\"dep-body\") }",
		"",
	}, "\n"))
	assertParity()

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(appRoot, "app", "wire.go"), strings.Join([]string{
		"package app",
		"",
		"import (",
		"\t\"example.com/app/helper\"",
		"\t\"example.com/dep\"",
		")",
		"",
		"func Init() string { return helper.Prefix() + dep.Message() }",
		"",
	}, "\n"))
	assertParity()

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(depRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"fmt\"",
		"",
		"type Logger interface { Log(string) }",
		"",
		"type NoopLogger struct{}",
		"",
		"func (NoopLogger) Log(string) {}",
		"",
		"func Message(Logger) string { return fmt.Sprint(\"dep-shape\") }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "app", "wire.go"), strings.Join([]string{
		"package app",
		"",
		"import (",
		"\t\"example.com/app/helper\"",
		"\t\"example.com/dep\"",
		")",
		"",
		"func Init() string {",
		"\tvar logger dep.Logger = dep.NoopLogger{}",
		"\treturn helper.Prefix() + dep.Message(logger)",
		"}",
		"",
	}, "\n"))
	assertParity()

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(depRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"fmt\"",
		"",
		"func Message() string { return fmt.Sprint(\"dep\") }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "app", "wire.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/dep\"",
		"",
		"func Init() string { return dep.Message() }",
		"",
	}, "\n"))
	assertParity()
}

func TestDiscoveryCacheInvalidatesOnGoSumResolutionChange(t *testing.T) {
	root := t.TempDir()
	proxyDir := t.TempDir()
	homeDir := t.TempDir()
	goCacheDir := tempCacheDirForTest(t, "wire-gocache-")
	goModCacheDir := tempCacheDirForTest(t, "wire-gomodcache-")

	writeModuleProxyVersion(t, proxyDir, "example.com/extdep", "v1.0.0", map[string]string{
		"pkg/pkg.go": "package pkg\n\nfunc Version() string { return \"v1.0.0\" }\n",
	})

	writeTestFile(t, filepath.Join(root, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require example.com/extdep v1.0.0",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(root, "app", "wire.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/extdep/pkg\"",
		"",
		"func Init() string { return pkg.Version() }",
		"",
	}, "\n"))

	env := append(os.Environ(),
		"HOME="+homeDir,
		"GOPROXY="+fileURLForTest(t, proxyDir),
		"GOSUMDB=off",
		"GOCACHE="+goCacheDir,
		"GOMODCACHE="+goModCacheDir,
	)
	runGoModTidyForTest(t, root, env)

	first := loadTypedPackageGraphForTest(t, root, env, "example.com/app/app", ModeCustom)
	if len(first.Packages) != 1 {
		t.Fatalf("first custom packages len = %d, want 1", len(first.Packages))
	}
	if got := comparableErrors(first.Packages[0].Errors); len(got) != 0 {
		t.Fatalf("first custom load returned errors: %v", got)
	}

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(root, "go.sum"), "")

	custom := loadTypedPackageGraphForTest(t, root, env, "example.com/app/app", ModeCustom)
	fallback := loadTypedPackageGraphForTest(t, root, env, "example.com/app/app", ModeFallback)
	compareRootPackagesOnly(t, custom.Packages, fallback.Packages, true)
}

func TestLoadTypedPackageGraphCustomExternalVersionChangeBustsCache(t *testing.T) {
	root := t.TempDir()
	proxyDir := t.TempDir()
	artifactDir := t.TempDir()
	homeDir := t.TempDir()
	goCacheDir := tempCacheDirForTest(t, "wire-gocache-")
	goModCacheDir := tempCacheDirForTest(t, "wire-gomodcache-")

	writeModuleProxyVersion(t, proxyDir, "example.com/extdep", "v1.0.0", map[string]string{
		"pkg/pkg.go": "package pkg\n\nfunc Version() string { return \"v1.0.0\" }\n",
	})
	writeModuleProxyVersion(t, proxyDir, "example.com/extdep", "v1.1.0", map[string]string{
		"pkg/pkg.go": "package pkg\n\nimport \"strings\"\n\nfunc Version() string { return strings.TrimSpace(\"v1.1.0\") }\n",
	})

	writeTestFile(t, filepath.Join(root, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require example.com/extdep v1.0.0",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(root, "app", "wire.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/extdep/pkg\"",
		"",
		"func Init() string { return pkg.Version() }",
		"",
	}, "\n"))

	env := append(os.Environ(),
		"HOME="+homeDir,
		"GOPROXY="+fileURLForTest(t, proxyDir),
		"GOSUMDB=off",
		"GOCACHE="+goCacheDir,
		"GOMODCACHE="+goModCacheDir,
		loaderArtifactEnv+"=1",
		loaderArtifactDirEnv+"="+artifactDir,
	)
	runGoModTidyForTest(t, root, env)

	first := loadPackagesForTest(t, root, env, []string{"./app"}, ModeCustom)
	if len(first.Packages) != 1 || len(first.Packages[0].Errors) != 0 {
		t.Fatalf("first custom load returned errors: %+v", first.Packages)
	}
	firstDep := collectGraph(first.Packages)["example.com/extdep/pkg"]
	if firstDep == nil {
		t.Fatal("expected dependency package for example.com/extdep/pkg")
	}
	if !containsPathSubstring(firstDep.CompiledGoFiles, "example.com/extdep@v1.0.0") {
		t.Fatalf("first dependency files = %v, want version v1.0.0", firstDep.CompiledGoFiles)
	}

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(root, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require example.com/extdep v1.1.0",
		"",
	}, "\n"))
	runGoModTidyForTest(t, root, env)

	custom := loadPackagesForTest(t, root, env, []string{"./app"}, ModeCustom)
	fallback := loadPackagesForTest(t, root, env, []string{"./app"}, ModeFallback)
	compareRootPackagesOnly(t, custom.Packages, fallback.Packages, true)
	comparePackageByPath(t, custom.Packages, fallback.Packages, "example.com/extdep/pkg", false)

	secondDep := collectGraph(custom.Packages)["example.com/extdep/pkg"]
	if secondDep == nil {
		t.Fatal("expected dependency package for example.com/extdep/pkg after version change")
	}
	if !containsPathSubstring(secondDep.CompiledGoFiles, "example.com/extdep@v1.1.0") {
		t.Fatalf("second dependency files = %v, want version v1.1.0", secondDep.CompiledGoFiles)
	}
}

func TestLoaderArtifactKeyExternalChangesWhenExportFileChanges(t *testing.T) {
	exportPath := filepath.Join(t.TempDir(), "dep.a")
	writeTestFile(t, exportPath, "first export payload")

	meta := &packageMeta{
		ImportPath: "example.com/dep",
		Name:       "dep",
		Export:     exportPath,
	}

	first, err := loaderArtifactKey(meta, false)
	if err != nil {
		t.Fatalf("loaderArtifactKey(first) error = %v", err)
	}

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, exportPath, "second export payload with different contents")

	second, err := loaderArtifactKey(meta, false)
	if err != nil {
		t.Fatalf("loaderArtifactKey(second) error = %v", err)
	}

	if first == second {
		t.Fatalf("loaderArtifactKey did not change after export file update: %q", first)
	}
}

func TestLoaderArtifactKeyExternalWithoutExportChangesWhenSourceChanges(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "dep.go")
	writeTestFile(t, sourcePath, "package dep\n\nconst Name = \"first\"\n")

	meta := &packageMeta{
		ImportPath:      "example.com/dep",
		Name:            "dep",
		GoFiles:         []string{sourcePath},
		CompiledGoFiles: []string{sourcePath},
	}

	first, err := loaderArtifactKey(meta, false)
	if err != nil {
		t.Fatalf("loaderArtifactKey(first) error = %v", err)
	}

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, sourcePath, "package dep\n\nconst Name = \"second\"\n")

	second, err := loaderArtifactKey(meta, false)
	if err != nil {
		t.Fatalf("loaderArtifactKey(second) error = %v", err)
	}

	if first == second {
		t.Fatalf("loaderArtifactKey did not change after external source update without export data: %q", first)
	}
}

func TestRunGoListIncludesExportDataForReplacedModule(t *testing.T) {
	root := t.TempDir()
	depRoot := filepath.Join(root, "depmod")
	appRoot := filepath.Join(root, "appmod")
	goCacheDir := tempCacheDirForTest(t, "wire-gocache-")
	goModCacheDir := tempCacheDirForTest(t, "wire-gomodcache-")

	writeTestFile(t, filepath.Join(depRoot, "go.mod"), "module example.com/dep\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(depRoot, "dep.go"), "package dep\n\nfunc New() string { return \"ok\" }\n")

	writeTestFile(t, filepath.Join(appRoot, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require example.com/dep v0.0.0",
		"",
		"replace example.com/dep => " + depRoot,
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "app", "app.go"), "package app\n\nimport \"example.com/dep\"\n\nfunc Use() string { return dep.New() }\n")

	meta, err := runGoList(context.Background(), goListRequest{
		WD:       appRoot,
		Env:      append(os.Environ(), "GOCACHE="+goCacheDir, "GOMODCACHE="+goModCacheDir),
		Patterns: []string{"example.com/app/app"},
		NeedDeps: true,
	})
	if err != nil {
		t.Fatalf("runGoList() error = %v", err)
	}
	depMeta := meta["example.com/dep"]
	if depMeta == nil {
		t.Fatal("expected metadata for example.com/dep")
	}
	if depMeta.Export == "" {
		t.Fatalf("expected export data path for replaced module metadata: %+v", depMeta)
	}
}

func TestLoadTypedPackageGraphCustomReplaceTargetWithExportDataWarmParity(t *testing.T) {
	root := t.TempDir()
	depRoot := filepath.Join(root, "depmod")
	appRoot := filepath.Join(root, "appmod")
	artifactDir := t.TempDir()
	homeDir := t.TempDir()
	goCacheDir := tempCacheDirForTest(t, "wire-gocache-")
	goModCacheDir := tempCacheDirForTest(t, "wire-gomodcache-")

	writeTestFile(t, filepath.Join(depRoot, "go.mod"), "module example.com/dep\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(depRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"func New() string { return \"ok\" }",
		"",
	}, "\n"))

	writeTestFile(t, filepath.Join(appRoot, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require example.com/dep v0.0.0",
		"",
		"replace example.com/dep => " + depRoot,
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "app", "app.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/dep\"",
		"",
		"func Use() string { return dep.New() }",
		"",
	}, "\n"))

	env := append(os.Environ(),
		"HOME="+homeDir,
		"GOCACHE="+goCacheDir,
		"GOMODCACHE="+goModCacheDir,
		loaderArtifactEnv+"=1",
		loaderArtifactDirEnv+"="+artifactDir,
	)

	meta, err := runGoList(context.Background(), goListRequest{
		WD:       appRoot,
		Env:      env,
		Patterns: []string{"example.com/app/app"},
		NeedDeps: true,
	})
	if err != nil {
		t.Fatalf("runGoList() error = %v", err)
	}
	depMeta := meta["example.com/dep"]
	if depMeta == nil || depMeta.Export == "" {
		t.Fatalf("expected export-backed metadata for example.com/dep: %+v", depMeta)
	}

	first := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeCustom)
	if len(first.Packages) != 1 || len(first.Packages[0].Errors) != 0 {
		t.Fatalf("first custom load returned errors: %+v", first.Packages)
	}

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(depRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"type Logger interface { Log(string) }",
		"",
		"type NoopLogger struct{}",
		"",
		"func (NoopLogger) Log(string) {}",
		"",
		"func New(Logger) string { return \"ok\" }",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "app", "app.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/dep\"",
		"",
		"func Use() string {",
		"\tvar logger dep.Logger = dep.NoopLogger{}",
		"\treturn dep.New(logger)",
		"}",
		"",
	}, "\n"))

	custom := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeCustom)
	fallback := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeFallback)
	compareRootPackagesOnly(t, custom.Packages, fallback.Packages, true)
	comparePackageByPath(t, custom.Packages, fallback.Packages, "example.com/dep", false)
}

func TestLoadTypedPackageGraphCustomReplaceTargetWithoutExportDataWarmParity(t *testing.T) {
	root := t.TempDir()
	depRoot := filepath.Join(root, "depmod")
	appRoot := filepath.Join(root, "appmod")
	homeDir := t.TempDir()
	goCacheDir := tempCacheDirForTest(t, "wire-gocache-")
	goModCacheDir := tempCacheDirForTest(t, "wire-gomodcache-")

	writeTestFile(t, filepath.Join(depRoot, "go.mod"), "module example.com/dep\n\ngo 1.19\n")
	writeTestFile(t, filepath.Join(depRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"//go:build never",
		"",
		"func New() string { return \"ok\" }",
		"",
	}, "\n"))

	writeTestFile(t, filepath.Join(appRoot, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require example.com/dep v0.0.0",
		"",
		"replace example.com/dep => " + depRoot,
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(appRoot, "app", "app.go"), strings.Join([]string{
		"package app",
		"",
		"import \"example.com/dep\"",
		"",
		"func Use() string { return dep.New() }",
		"",
	}, "\n"))

	env := append(os.Environ(),
		"HOME="+homeDir,
		"GOCACHE="+goCacheDir,
		"GOMODCACHE="+goModCacheDir,
	)

	meta, err := runGoList(context.Background(), goListRequest{
		WD:       appRoot,
		Env:      env,
		Patterns: []string{"example.com/app/app"},
		NeedDeps: true,
	})
	if err != nil {
		t.Fatalf("runGoList(first) error = %v", err)
	}
	depMeta := meta["example.com/dep"]
	if depMeta == nil || depMeta.Export != "" {
		t.Fatalf("expected no export data for incomplete replaced module: %+v", depMeta)
	}

	first := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeCustom)
	if len(first.Packages) != 1 {
		t.Fatalf("first custom packages len = %d, want 1", len(first.Packages))
	}

	time.Sleep(10 * time.Millisecond)
	writeTestFile(t, filepath.Join(depRoot, "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"var _ missing",
		"",
		"func New() string { return \"ok\" }",
		"",
	}, "\n"))

	meta, err = runGoList(context.Background(), goListRequest{
		WD:       appRoot,
		Env:      env,
		Patterns: []string{"example.com/app/app"},
		NeedDeps: true,
	})
	if err != nil {
		t.Fatalf("runGoList(second) error = %v", err)
	}
	depMeta = meta["example.com/dep"]
	if depMeta == nil || depMeta.Export != "" {
		t.Fatalf("expected no export data for second incomplete replaced module state: %+v", depMeta)
	}

	custom := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeCustom)
	fallback := loadTypedPackageGraphForTest(t, appRoot, env, "example.com/app/app", ModeFallback)
	compareRootPackagesOnly(t, custom.Packages, fallback.Packages, true)
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

func comparePackageByPath(t *testing.T, got []*packages.Package, want []*packages.Package, pkgPath string, requireTyped bool) {
	t.Helper()
	gotPkg := collectGraph(got)[pkgPath]
	if gotPkg == nil {
		t.Fatalf("missing package %q in custom graph", pkgPath)
	}
	wantPkg := collectGraph(want)[pkgPath]
	if wantPkg == nil {
		t.Fatalf("missing package %q in fallback graph", pkgPath)
	}
	if gotPkg.Name != wantPkg.Name {
		t.Fatalf("package %q name = %q, want %q", pkgPath, gotPkg.Name, wantPkg.Name)
	}
	if !equalStrings(gotPkg.CompiledGoFiles, wantPkg.CompiledGoFiles) {
		t.Fatalf("package %q compiled files = %v, want %v", pkgPath, gotPkg.CompiledGoFiles, wantPkg.CompiledGoFiles)
	}
	if !equalImportPaths(gotPkg.Imports, wantPkg.Imports) {
		t.Fatalf("package %q imports = %v, want %v", pkgPath, sortedImportPaths(gotPkg.Imports), sortedImportPaths(wantPkg.Imports))
	}
	gotErrs := comparableErrors(gotPkg.Errors)
	wantErrs := comparableErrors(wantPkg.Errors)
	if len(gotErrs) != len(wantErrs) {
		t.Fatalf("package %q comparable errors len = %d, want %d; got=%v want=%v", pkgPath, len(gotErrs), len(wantErrs), gotErrs, wantErrs)
	}
	for i := range gotErrs {
		if gotErrs[i] != wantErrs[i] {
			t.Fatalf("package %q comparable error[%d] = %q, want %q", pkgPath, i, gotErrs[i], wantErrs[i])
		}
	}
	if requireTyped {
		gotTyped := gotPkg.Types != nil && gotPkg.TypesInfo != nil && len(gotPkg.Syntax) > 0
		wantTyped := wantPkg.Types != nil && wantPkg.TypesInfo != nil && len(wantPkg.Syntax) > 0
		if gotTyped != wantTyped {
			t.Fatalf("package %q typed state = %v, want %v", pkgPath, gotTyped, wantTyped)
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

func loadTypedPackageGraphForTest(t *testing.T, wd string, env []string, pkg string, mode Mode) *LazyLoadResult {
	return loadTypedPackageGraphWithDiscoveryForTest(t, wd, env, pkg, mode, nil)
}

func loadPackagesForTest(t *testing.T, wd string, env []string, patterns []string, mode Mode) *PackageLoadResult {
	t.Helper()
	l := New()
	got, err := l.LoadPackages(context.Background(), PackageLoadRequest{
		WD:         wd,
		Env:        env,
		Patterns:   patterns,
		Mode:       packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax | packages.NeedExportFile,
		LoaderMode: mode,
		Fset:       token.NewFileSet(),
		ParseFile: func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
			return parser.ParseFile(fset, filename, src, parser.ParseComments|parser.SkipObjectResolution)
		},
	})
	if err != nil {
		t.Fatalf("LoadPackages(%q, %q) error = %v", wd, mode, err)
	}
	return got
}

func loadTypedPackageGraphWithDiscoveryForTest(t *testing.T, wd string, env []string, pkg string, mode Mode, discovery *DiscoverySnapshot) *LazyLoadResult {
	t.Helper()
	l := New()
	got, err := l.LoadTypedPackageGraph(context.Background(), LazyLoadRequest{
		WD:         wd,
		Env:        env,
		Package:    pkg,
		Mode:       packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax | packages.NeedExportFile,
		LoaderMode: mode,
		Fset:       token.NewFileSet(),
		ParseFile: func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
			return parser.ParseFile(fset, filename, src, parser.ParseComments|parser.SkipObjectResolution)
		},
		Discovery: discovery,
	})
	if err != nil {
		t.Fatalf("LoadTypedPackageGraph(%q, %q) error = %v", wd, mode, err)
	}
	return got
}

func loadRootGraphForTest(t *testing.T, wd string, env []string, patterns []string, mode Mode) *RootLoadResult {
	t.Helper()
	l := New()
	got, err := l.LoadRootGraph(context.Background(), RootLoadRequest{
		WD:       wd,
		Env:      env,
		Patterns: patterns,
		NeedDeps: true,
		Mode:     mode,
		Fset:     token.NewFileSet(),
	})
	if err != nil {
		t.Fatalf("LoadRootGraph(%q, %q) error = %v", wd, mode, err)
	}
	return got
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

func containsPathSubstring(paths []string, needle string) bool {
	for _, path := range paths {
		if strings.Contains(normalizePathForCompare(path), needle) {
			return true
		}
	}
	return false
}

func runGoModTidyForTest(t *testing.T, wd string, env []string) {
	t.Helper()
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = wd
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go mod tidy in %q error = %v: %s", wd, err, out)
	}
}

func writeModuleProxyVersion(t *testing.T, proxyDir string, modulePath string, version string, files map[string]string) {
	t.Helper()
	base := filepath.Join(proxyDir, filepath.FromSlash(modulePath), "@v")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("mkdir proxy dir: %v", err)
	}
	listPath := filepath.Join(base, "list")
	appendLineIfMissing(t, listPath, version)

	modFile := "module " + modulePath + "\n\ngo 1.19\n"
	writeTestFile(t, filepath.Join(base, version+".mod"), modFile)
	writeTestFile(t, filepath.Join(base, version+".info"), fmt.Sprintf("{\"Version\":%q,\"Time\":\"2024-01-01T00:00:00Z\"}\n", version))

	zipPath := filepath.Join(base, version+".zip")
	zipFile, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create proxy zip: %v", err)
	}
	defer zipFile.Close()

	zw := zip.NewWriter(zipFile)
	moduleRoot := modulePath + "@" + version
	writeZipFile := func(name string, contents string) {
		w, err := zw.Create(moduleRoot + "/" + filepath.ToSlash(name))
		if err != nil {
			t.Fatalf("create zip entry %q: %v", name, err)
		}
		if _, err := w.Write([]byte(contents)); err != nil {
			t.Fatalf("write zip entry %q: %v", name, err)
		}
	}
	writeZipFile("go.mod", modFile)
	for name, contents := range files {
		writeZipFile(name, contents)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close proxy zip: %v", err)
	}
}

func appendLineIfMissing(t *testing.T, path string, line string) {
	t.Helper()
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read %q: %v", path, err)
	}
	for _, existingLine := range strings.Split(strings.TrimSpace(string(existing)), "\n") {
		if existingLine == line {
			return
		}
	}
	content := string(existing)
	if strings.TrimSpace(content) != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += line + "\n"
	writeTestFile(t, path, content)
}

func tempCacheDirForTest(t *testing.T, pattern string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", pattern)
	if err != nil {
		t.Fatalf("MkdirTemp(%q) error = %v", pattern, err)
	}
	t.Cleanup(func() {
		_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				_ = os.Chmod(path, 0o755)
				return nil
			}
			_ = os.Chmod(path, 0o644)
			return nil
		})
		_ = os.RemoveAll(dir)
	})
	return dir
}

func fileURLForTest(t *testing.T, path string) string {
	t.Helper()
	slashed := filepath.ToSlash(path)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return "file://" + slashed
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
	last := strings.LastIndex(pos, ":")
	if last == -1 {
		return shortenComparablePath(normalizePathForCompare(pos))
	}
	prev := strings.LastIndex(pos[:last], ":")
	if prev == -1 {
		return shortenComparablePath(normalizePathForCompare(pos))
	}
	path := shortenComparablePath(normalizePathForCompare(pos[:prev]))
	return path + pos[prev:]
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
