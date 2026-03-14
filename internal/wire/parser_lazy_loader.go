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
	ctx          context.Context
	wd           string
	env          []string
	tags         string
	fset         *token.FileSet
	baseFiles    map[string]map[string]struct{}
	session      *incrementalSession
	fingerprints *incrementalFingerprintSnapshot
	loaded       map[string]*packages.Package
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
	return packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax | packages.NeedExportFile
}

func (ll *lazyLoader) loadWithMode(pkgPath string, mode packages.LoadMode, timingLabel string) ([]*packages.Package, []error) {
	parseStats := &parseFileStats{}
	cfg := &packages.Config{
		Context:    ll.ctx,
		Mode:       mode,
		Dir:        ll.wd,
		Env:        ll.env,
		BuildFlags: []string{"-tags=wireinject"},
		Fset:       ll.fset,
		ParseFile:  ll.parseFileFor(pkgPath, parseStats),
	}
	if len(ll.tags) > 0 {
		cfg.BuildFlags[0] += " " + ll.tags
	}
	loadStart := time.Now()
	pkgs, err := packages.Load(cfg, "pattern="+pkgPath)
	logTiming(ll.ctx, timingLabel, loadStart)
	logLoadDebug(ll.ctx, "lazy", mode, pkgPath, ll.wd, pkgs, parseStats)
	if err != nil {
		return nil, []error{err}
	}
	errs := collectLoadErrors(pkgs)
	if len(errs) > 0 {
		return nil, errs
	}
	ll.rememberPackages(pkgs)
	return pkgs, nil
}

func (ll *lazyLoader) rememberPackages(pkgs []*packages.Package) {
	if ll == nil || len(pkgs) == 0 {
		return
	}
	if ll.loaded == nil {
		ll.loaded = make(map[string]*packages.Package)
	}
	for path, pkg := range collectAllPackages(pkgs) {
		if pkg != nil {
			ll.loaded[path] = pkg
		}
	}
}

func (ll *lazyLoader) parseFileFor(pkgPath string, stats *parseFileStats) func(*token.FileSet, string, []byte) (*ast.File, error) {
	primary := primaryFileSet(ll.baseFiles[pkgPath])
	return func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
		start := time.Now()
		isPrimary := isPrimaryFile(primary, filename)
		keepBodies := ll.shouldKeepDependencyBodies(filename)
		if !isPrimary && !keepBodies && ll.session != nil {
			if file, ok := ll.session.getParsedDep(filename, src); ok {
				if stats != nil {
					stats.record(false, time.Since(start), nil, true)
				}
				return file, nil
			}
		}
		mode := parser.SkipObjectResolution
		if isPrimary {
			mode = parser.ParseComments | parser.SkipObjectResolution
		}
		file, err := parser.ParseFile(fset, filename, src, mode)
		if stats != nil {
			stats.record(isPrimary, time.Since(start), err, false)
		}
		if err != nil {
			return nil, err
		}
		if primary == nil {
			return file, nil
		}
		if isPrimary {
			return file, nil
		}
		if keepBodies {
			return file, nil
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				fn.Body = nil
				fn.Doc = nil
			}
		}
		if ll.session != nil {
			ll.session.storeParsedDep(filename, src, file)
		}
		return file, nil
	}
}

func (ll *lazyLoader) shouldKeepDependencyBodies(filename string) bool {
	if ll == nil || ll.fingerprints == nil || len(ll.fingerprints.touched) == 0 {
		return false
	}
	clean := filepath.Clean(filename)
	for _, pkgPath := range ll.fingerprints.touched {
		files := ll.baseFiles[pkgPath]
		if len(files) == 0 {
			continue
		}
		if _, ok := files[clean]; ok {
			return true
		}
	}
	return false
}
