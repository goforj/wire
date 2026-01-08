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
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLazyLoaderParseFileFor(t *testing.T) {
	t.Helper()
	fset := token.NewFileSet()
	pkgPath := "example.com/pkg"
	root := t.TempDir()
	primary := filepath.Join(root, "primary.go")
	secondary := filepath.Join(root, "secondary.go")
	ll := &lazyLoader{
		fset: fset,
		baseFiles: map[string]map[string]struct{}{
			pkgPath: {filepath.Clean(primary): {}},
		},
	}
	src := strings.Join([]string{
		"package pkg",
		"",
		"// Doc comment",
		"func Foo() {",
		"\tprintln(\"hi\")",
		"}",
		"",
	}, "\n")

	parse := ll.parseFileFor(pkgPath)
	file, err := parse(fset, primary, []byte(src))
	if err != nil {
		t.Fatalf("parse primary: %v", err)
	}
	fn := firstFuncDecl(t, file)
	if fn.Body == nil {
		t.Fatal("expected primary file to keep function body")
	}
	if fn.Doc == nil {
		t.Fatal("expected primary file to keep doc comment")
	}

	file, err = parse(fset, secondary, []byte(src))
	if err != nil {
		t.Fatalf("parse secondary: %v", err)
	}
	fn = firstFuncDecl(t, file)
	if fn.Body != nil {
		t.Fatal("expected secondary file to strip function body")
	}
	if fn.Doc != nil {
		t.Fatal("expected secondary file to strip doc comment")
	}
}

func TestLoadModuleUsesWireinjectTagsForDeps(t *testing.T) {
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
	}, "\n"))

	writeFile(t, filepath.Join(root, "dep", "dep_inject.go"), strings.Join([]string{
		"//go:build wireinject",
		"// +build wireinject",
		"",
		"package dep",
		"",
		"func New() *Foo {",
		"\treturn &Foo{}",
		"}",
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
}

func firstFuncDecl(t *testing.T, file *ast.File) *ast.FuncDecl {
	t.Helper()
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			return fn
		}
	}
	t.Fatal("expected function declaration in file")
	return nil
}
