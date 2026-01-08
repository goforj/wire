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
	"go/parser"
	"go/token"
	"path/filepath"
	"time"

	"golang.org/x/tools/go/packages"
)

type lazyLoader struct {
	ctx       context.Context
	wd        string
	env       []string
	tags      string
	fset      *token.FileSet
	baseFiles map[string]map[string]struct{}
}

func collectPackageFiles(pkgs []*packages.Package) map[string]map[string]struct{} {
	all := collectAllPackages(pkgs)
	out := make(map[string]map[string]struct{}, len(all))
	for path, pkg := range all {
		if pkg == nil {
			continue
		}
		files := make(map[string]struct{}, len(pkg.CompiledGoFiles))
		for _, name := range pkg.CompiledGoFiles {
			files[filepath.Clean(name)] = struct{}{}
		}
		if len(files) > 0 {
			out[path] = files
		}
	}
	return out
}

func collectAllPackages(pkgs []*packages.Package) map[string]*packages.Package {
	all := make(map[string]*packages.Package)
	stack := append([]*packages.Package(nil), pkgs...)
	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if p == nil || all[p.PkgPath] != nil {
			continue
		}
		all[p.PkgPath] = p
		for _, imp := range p.Imports {
			stack = append(stack, imp)
		}
	}
	return all
}

func (ll *lazyLoader) load(pkgPath string) ([]*packages.Package, []error) {
	return ll.loadWithMode(pkgPath, ll.fullMode(), "load.packages.lazy.load")
}

func (ll *lazyLoader) fullMode() packages.LoadMode {
	return packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax
}

func (ll *lazyLoader) loadWithMode(pkgPath string, mode packages.LoadMode, timingLabel string) ([]*packages.Package, []error) {
	cfg := &packages.Config{
		Context:    ll.ctx,
		Mode:       mode,
		Dir:        ll.wd,
		Env:        ll.env,
		BuildFlags: []string{"-tags=wireinject"},
		Fset:       ll.fset,
		ParseFile:  ll.parseFileFor(pkgPath),
	}
	if len(ll.tags) > 0 {
		cfg.BuildFlags[0] += " " + ll.tags
	}
	loadStart := time.Now()
	pkgs, err := packages.Load(cfg, "pattern="+pkgPath)
	logTiming(ll.ctx, timingLabel, loadStart)
	if err != nil {
		return nil, []error{err}
	}
	errs := collectLoadErrors(pkgs)
	if len(errs) > 0 {
		return nil, errs
	}
	return pkgs, nil
}

func (ll *lazyLoader) parseFileFor(pkgPath string) func(*token.FileSet, string, []byte) (*ast.File, error) {
	primary := ll.baseFiles[pkgPath]
	return func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
		mode := parser.SkipObjectResolution
		if primary != nil {
			if _, ok := primary[filepath.Clean(filename)]; ok {
				mode = parser.ParseComments | parser.SkipObjectResolution
			}
		}
		file, err := parser.ParseFile(fset, filename, src, mode)
		if err != nil {
			return nil, err
		}
		if primary == nil {
			return file, nil
		}
		if _, ok := primary[filepath.Clean(filename)]; ok {
			return file, nil
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				fn.Body = nil
				fn.Doc = nil
			}
		}
		return file, nil
	}
}
