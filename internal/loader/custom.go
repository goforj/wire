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
	"fmt"
	"go/ast"
	importerpkg "go/importer"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/tools/go/gcexportdata"
	"golang.org/x/tools/go/packages"

	"github.com/goforj/wire/internal/semanticcache"
)

type unsupportedError struct {
	reason string
}

func (e unsupportedError) Error() string { return e.reason }

type packageMeta struct {
	ImportPath      string
	Name            string
	Dir             string
	DepOnly         bool
	Export          string
	GoFiles         []string
	CompiledGoFiles []string
	Imports         []string
	ImportMap       map[string]string
	Module          *goListModule
	Error           *goListError
}

type goListModule struct {
	Path    string
	Version string
	Main    bool
	Dir     string
	GoMod   string
	Replace *goListModule
}

type goListError struct {
	Err string
}

type customValidator struct {
	fset     *token.FileSet
	meta     map[string]*packageMeta
	touched  map[string]struct{}
	packages map[string]*types.Package
	importer types.Importer
	loading  map[string]bool
}

type customTypedGraphLoader struct {
	workspace        string
	ctx              context.Context
	env              []string
	fset             *token.FileSet
	meta             map[string]*packageMeta
	targets          map[string]struct{}
	parseFile        ParseFileFunc
	packages         map[string]*packages.Package
	typesPkgs        map[string]*types.Package
	importer         types.Importer
	loading          map[string]bool
	isLocalCache     map[string]bool
	localSemanticOK  map[string]bool
	artifactPrefetch map[string]artifactPrefetchEntry
	stats            typedLoadStats
}

type artifactPrefetchEntry struct {
	path string
	data []byte
	err  error
	ok   bool
}

type typedLoadStats struct {
	read               time.Duration
	parse              time.Duration
	typecheck          time.Duration
	localRead          time.Duration
	externalRead       time.Duration
	localParse         time.Duration
	externalParse      time.Duration
	localTypecheck     time.Duration
	externalTypecheck  time.Duration
	filesRead          int
	packages           int
	localPackages      int
	externalPackages   int
	localFilesRead     int
	externalFilesRead  int
	artifactRead       time.Duration
	artifactPath       time.Duration
	artifactDecode     time.Duration
	artifactImportLink time.Duration
	artifactWrite      time.Duration
	artifactPrefetch   time.Duration
	rootLoad           time.Duration
	discovery          time.Duration
	artifactHits       int
	artifactMisses     int
	artifactWrites     int
}

func validateTouchedPackagesCustom(ctx context.Context, req TouchedValidationRequest) (*TouchedValidationResult, error) {
	if len(req.Touched) == 0 {
		return &TouchedValidationResult{Backend: ModeCustom}, nil
	}
	meta, err := discoverTouchedMetadata(ctx, req)
	if err != nil {
		return nil, err
	}
	validator := &customValidator{
		fset:     token.NewFileSet(),
		meta:     meta,
		touched:  make(map[string]struct{}, len(req.Touched)),
		packages: make(map[string]*types.Package, len(meta)),
		importer: importerpkg.ForCompiler(token.NewFileSet(), "gc", nil),
		loading:  make(map[string]bool, len(meta)),
	}
	for _, path := range req.Touched {
		if !metadataMatchesFingerprint(path, meta, req.Local) {
			return nil, unsupportedError{reason: "metadata fingerprint mismatch"}
		}
		validator.touched[path] = struct{}{}
	}
	out := make([]*packages.Package, 0, len(req.Touched))
	for _, path := range req.Touched {
		pkg, err := validator.validatePackage(path)
		if err != nil {
			return nil, err
		}
		out = append(out, pkg)
	}
	return &TouchedValidationResult{
		Packages: out,
		Backend:  ModeCustom,
	}, nil
}

func loadRootGraphCustom(ctx context.Context, req RootLoadRequest) (*RootLoadResult, error) {
	discoveryStart := time.Now()
	meta, err := runGoList(ctx, goListRequest{
		WD:       req.WD,
		Env:      req.Env,
		Tags:     req.Tags,
		Patterns: req.Patterns,
		NeedDeps: req.NeedDeps,
	})
	if err != nil {
		return nil, err
	}
	logTiming(ctx, "loader.custom.root.discovery", discoveryStart)
	if len(meta) == 0 {
		return nil, unsupportedError{reason: "empty go list result"}
	}
	pkgs := make(map[string]*packages.Package, len(meta))
	for path, m := range meta {
		pkgs[path] = &packages.Package{
			ID:              m.ImportPath,
			Name:            m.Name,
			PkgPath:         m.ImportPath,
			GoFiles:         append([]string(nil), metaFiles(m)...),
			CompiledGoFiles: append([]string(nil), metaFiles(m)...),
			ExportFile:      m.Export,
			Imports:         make(map[string]*packages.Package),
		}
		if m.Error != nil && strings.TrimSpace(m.Error.Err) != "" {
			pkgs[path].Errors = append(pkgs[path].Errors, packages.Error{
				Pos:  "-",
				Msg:  m.Error.Err,
				Kind: packages.ListError,
			})
		}
	}
	for path, m := range meta {
		pkg := pkgs[path]
		for _, imp := range m.Imports {
			target := imp
			if mapped := m.ImportMap[imp]; mapped != "" {
				target = mapped
			}
			if dep := pkgs[target]; dep != nil {
				pkg.Imports[imp] = dep
			}
		}
	}
	roots := make([]*packages.Package, 0, len(req.Patterns))
	for _, m := range meta {
		if m.DepOnly {
			continue
		}
		if pkg := pkgs[m.ImportPath]; pkg != nil {
			roots = append(roots, pkg)
		}
	}
	if len(roots) == 0 {
		return nil, unsupportedError{reason: "no root packages from metadata"}
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].PkgPath < roots[j].PkgPath })
	return &RootLoadResult{
		Packages:  roots,
		Backend:   ModeCustom,
		Discovery: discoverySnapshotForMeta(meta, req.NeedDeps),
	}, nil
}

func loadTypedPackageGraphCustom(ctx context.Context, req LazyLoadRequest) (*LazyLoadResult, error) {
	stopProfile, profileErr := startLoaderCPUProfile(req.Env)
	if profileErr != nil {
		return nil, profileErr
	}
	if stopProfile != nil {
		defer stopProfile()
	}
	var (
		meta map[string]*packageMeta
		err  error
	)
	discoveryStart := time.Now()
	if req.Discovery != nil && len(req.Discovery.meta) > 0 {
		meta = req.Discovery.meta
	} else {
		meta, err = runGoList(ctx, goListRequest{
			WD:       req.WD,
			Env:      req.Env,
			Tags:     req.Tags,
			Patterns: []string{req.Package},
			NeedDeps: true,
		})
		if err != nil {
			return nil, err
		}
	}
	discoveryDuration := time.Since(discoveryStart)
	if len(meta) == 0 {
		return nil, unsupportedError{reason: "empty go list result"}
	}
	fset := req.Fset
	if fset == nil {
		fset = token.NewFileSet()
	}
	l := &customTypedGraphLoader{
		workspace:        detectModuleRoot(req.WD),
		ctx:              ctx,
		env:              append([]string(nil), req.Env...),
		fset:             fset,
		meta:             meta,
		targets:          map[string]struct{}{req.Package: {}},
		parseFile:        req.ParseFile,
		packages:         make(map[string]*packages.Package, len(meta)),
		typesPkgs:        make(map[string]*types.Package, len(meta)),
		importer:         importerpkg.ForCompiler(token.NewFileSet(), "gc", nil),
		loading:          make(map[string]bool, len(meta)),
		isLocalCache:     make(map[string]bool, len(meta)),
		localSemanticOK:  make(map[string]bool, len(meta)),
		artifactPrefetch: make(map[string]artifactPrefetchEntry, len(meta)),
		stats:            typedLoadStats{discovery: discoveryDuration},
	}
	prefetchStart := time.Now()
	l.prefetchArtifacts()
	l.stats.artifactPrefetch = time.Since(prefetchStart)
	rootLoadStart := time.Now()
	root, err := l.loadPackage(req.Package)
	if err != nil {
		return nil, err
	}
	l.stats.rootLoad = time.Since(rootLoadStart)
	logDuration(ctx, "loader.custom.lazy.read_files.cumulative", l.stats.read)
	logDuration(ctx, "loader.custom.lazy.parse_files.cumulative", l.stats.parse)
	logDuration(ctx, "loader.custom.lazy.typecheck.cumulative", l.stats.typecheck)
	logDuration(ctx, "loader.custom.lazy.read_files.local.cumulative", l.stats.localRead)
	logDuration(ctx, "loader.custom.lazy.read_files.external.cumulative", l.stats.externalRead)
	logDuration(ctx, "loader.custom.lazy.parse_files.local.cumulative", l.stats.localParse)
	logDuration(ctx, "loader.custom.lazy.parse_files.external.cumulative", l.stats.externalParse)
	logDuration(ctx, "loader.custom.lazy.typecheck.local.cumulative", l.stats.localTypecheck)
	logDuration(ctx, "loader.custom.lazy.typecheck.external.cumulative", l.stats.externalTypecheck)
	logDuration(ctx, "loader.custom.lazy.artifact_read", l.stats.artifactRead)
	logDuration(ctx, "loader.custom.lazy.artifact_path", l.stats.artifactPath)
	logDuration(ctx, "loader.custom.lazy.artifact_decode", l.stats.artifactDecode)
	logDuration(ctx, "loader.custom.lazy.artifact_import_link", l.stats.artifactImportLink)
	logDuration(ctx, "loader.custom.lazy.artifact_write", l.stats.artifactWrite)
	logDuration(ctx, "loader.custom.lazy.artifact_prefetch.wall", l.stats.artifactPrefetch)
	logDuration(ctx, "loader.custom.lazy.root_load.wall", l.stats.rootLoad)
	logDuration(ctx, "loader.custom.lazy.discovery.wall", l.stats.discovery)
	logInt(ctx, "loader.custom.lazy.artifact_hits", l.stats.artifactHits)
	logInt(ctx, "loader.custom.lazy.artifact_misses", l.stats.artifactMisses)
	logInt(ctx, "loader.custom.lazy.artifact_writes", l.stats.artifactWrites)
	return &LazyLoadResult{
		Packages: []*packages.Package{root},
		Backend:  ModeCustom,
	}, nil
}

func loadPackagesCustom(ctx context.Context, req PackageLoadRequest) (*PackageLoadResult, error) {
	discoveryStart := time.Now()
	meta, err := runGoList(ctx, goListRequest{
		WD:       req.WD,
		Env:      req.Env,
		Tags:     req.Tags,
		Patterns: req.Patterns,
		NeedDeps: true,
	})
	if err != nil {
		return nil, err
	}
	discoveryDuration := time.Since(discoveryStart)
	if len(meta) == 0 {
		return nil, unsupportedError{reason: "empty go list result"}
	}
	fset := req.Fset
	if fset == nil {
		fset = token.NewFileSet()
	}
	targets := make(map[string]struct{})
	for _, m := range meta {
		if m.DepOnly {
			continue
		}
		targets[m.ImportPath] = struct{}{}
	}
	if len(targets) == 0 {
		return nil, unsupportedError{reason: "no root packages from metadata"}
	}
	l := &customTypedGraphLoader{
		workspace:        detectModuleRoot(req.WD),
		ctx:              ctx,
		env:              append([]string(nil), req.Env...),
		fset:             fset,
		meta:             meta,
		targets:          targets,
		parseFile:        req.ParseFile,
		packages:         make(map[string]*packages.Package, len(meta)),
		typesPkgs:        make(map[string]*types.Package, len(meta)),
		importer:         importerpkg.ForCompiler(token.NewFileSet(), "gc", nil),
		loading:          make(map[string]bool, len(meta)),
		isLocalCache:     make(map[string]bool, len(meta)),
		localSemanticOK:  make(map[string]bool, len(meta)),
		artifactPrefetch: make(map[string]artifactPrefetchEntry, len(meta)),
		stats:            typedLoadStats{discovery: discoveryDuration},
	}
	prefetchStart := time.Now()
	l.prefetchArtifacts()
	l.stats.artifactPrefetch = time.Since(prefetchStart)
	rootLoadStart := time.Now()
	roots := make([]*packages.Package, 0, len(targets))
	for _, m := range meta {
		if m.DepOnly {
			continue
		}
		root, err := l.loadPackage(m.ImportPath)
		if err != nil {
			return nil, err
		}
		roots = append(roots, root)
	}
	l.stats.rootLoad = time.Since(rootLoadStart)
	sort.Slice(roots, func(i, j int) bool { return roots[i].PkgPath < roots[j].PkgPath })
	logDuration(ctx, "loader.custom.typed.read_files.cumulative", l.stats.read)
	logDuration(ctx, "loader.custom.typed.parse_files.cumulative", l.stats.parse)
	logDuration(ctx, "loader.custom.typed.typecheck.cumulative", l.stats.typecheck)
	logDuration(ctx, "loader.custom.typed.read_files.local.cumulative", l.stats.localRead)
	logDuration(ctx, "loader.custom.typed.read_files.external.cumulative", l.stats.externalRead)
	logDuration(ctx, "loader.custom.typed.parse_files.local.cumulative", l.stats.localParse)
	logDuration(ctx, "loader.custom.typed.parse_files.external.cumulative", l.stats.externalParse)
	logDuration(ctx, "loader.custom.typed.typecheck.local.cumulative", l.stats.localTypecheck)
	logDuration(ctx, "loader.custom.typed.typecheck.external.cumulative", l.stats.externalTypecheck)
	logDuration(ctx, "loader.custom.typed.artifact_read", l.stats.artifactRead)
	logDuration(ctx, "loader.custom.typed.artifact_path", l.stats.artifactPath)
	logDuration(ctx, "loader.custom.typed.artifact_decode", l.stats.artifactDecode)
	logDuration(ctx, "loader.custom.typed.artifact_import_link", l.stats.artifactImportLink)
	logDuration(ctx, "loader.custom.typed.artifact_write", l.stats.artifactWrite)
	logDuration(ctx, "loader.custom.typed.artifact_prefetch.wall", l.stats.artifactPrefetch)
	logDuration(ctx, "loader.custom.typed.root_load.wall", l.stats.rootLoad)
	logDuration(ctx, "loader.custom.typed.discovery.wall", l.stats.discovery)
	logInt(ctx, "loader.custom.typed.artifact_hits", l.stats.artifactHits)
	logInt(ctx, "loader.custom.typed.artifact_misses", l.stats.artifactMisses)
	logInt(ctx, "loader.custom.typed.artifact_writes", l.stats.artifactWrites)
	return &PackageLoadResult{
		Packages: roots,
		Backend:  ModeCustom,
	}, nil
}

func (v *customValidator) validatePackage(path string) (*packages.Package, error) {
	meta := v.meta[path]
	if meta == nil {
		return nil, unsupportedError{reason: "missing metadata for touched package"}
	}
	if v.loading[path] {
		return nil, unsupportedError{reason: "touched package cycle"}
	}
	v.loading[path] = true
	defer delete(v.loading, path)
	pkg := &packages.Package{
		ID:              meta.ImportPath,
		Name:            meta.Name,
		PkgPath:         meta.ImportPath,
		Fset:            v.fset,
		GoFiles:         append([]string(nil), metaFiles(meta)...),
		CompiledGoFiles: append([]string(nil), metaFiles(meta)...),
		Imports:         make(map[string]*packages.Package),
		ExportFile:      meta.Export,
	}
	if meta.Error != nil && strings.TrimSpace(meta.Error.Err) != "" {
		pkg.Errors = append(pkg.Errors, packages.Error{
			Pos:  "-",
			Msg:  meta.Error.Err,
			Kind: packages.ListError,
		})
		return pkg, nil
	}
	files, errs := v.parseFiles(metaFiles(meta))
	pkg.Errors = append(pkg.Errors, errs...)
	if len(files) == 0 {
		return pkg, nil
	}

	tpkg := types.NewPackage(meta.ImportPath, meta.Name)
	v.packages[meta.ImportPath] = tpkg
	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Implicits:  make(map[ast.Node]types.Object),
		Scopes:     make(map[ast.Node]*types.Scope),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	importer := importerFunc(func(importPath string) (*types.Package, error) {
		if importPath == "unsafe" {
			return types.Unsafe, nil
		}
		target := importPath
		if mapped := meta.ImportMap[importPath]; mapped != "" {
			target = mapped
		}
		if _, ok := v.touched[target]; ok {
			if typed := v.packages[target]; typed != nil && typed.Complete() {
				if depMeta := v.meta[target]; depMeta != nil {
					pkg.Imports[importPath] = touchedPackageStub(v.fset, depMeta)
				}
				return typed, nil
			}
			checked, err := v.validatePackage(target)
			if err != nil {
				return nil, err
			}
			pkg.Imports[importPath] = checked
			if len(checked.Errors) > 0 {
				return nil, fmt.Errorf("touched dependency %s has errors", target)
			}
			if typed := v.packages[target]; typed != nil {
				return typed, nil
			}
			return nil, unsupportedError{reason: "missing typed touched dependency"}
		}
		dep, err := v.importFromExport(target)
		if err == nil {
			if depMeta := v.meta[target]; depMeta != nil {
				pkg.Imports[importPath] = touchedPackageStub(v.fset, depMeta)
			} else {
				pkg.Imports[importPath] = &packages.Package{PkgPath: target, Name: dep.Name()}
			}
		}
		return dep, err
	})
	var typeErrors []packages.Error
	cfg := &types.Config{
		Importer: importer,
		Sizes:    types.SizesFor("gc", runtime.GOARCH),
		Error: func(err error) {
			typeErrors = append(typeErrors, toPackagesError(v.fset, err))
		},
	}
	checker := types.NewChecker(cfg, v.fset, tpkg, info)
	if err := checker.Files(files); err != nil && len(typeErrors) == 0 {
		typeErrors = append(typeErrors, toPackagesError(v.fset, err))
	}
	pkg.Syntax = files
	pkg.Types = tpkg
	pkg.TypesInfo = info
	typeErrors = append(typeErrors, v.validateDeclaredImports(meta, files)...)
	pkg.Errors = append(pkg.Errors, typeErrors...)
	return pkg, nil
}

func (l *customTypedGraphLoader) loadPackage(path string) (*packages.Package, error) {
	if path == "C" {
		if pkg := l.packages[path]; pkg != nil {
			return pkg, nil
		}
		tpkg := l.typesPkgs[path]
		if tpkg == nil {
			tpkg = types.NewPackage("C", "C")
			l.typesPkgs[path] = tpkg
		}
		pkg := &packages.Package{
			ID:      "C",
			Name:    "C",
			PkgPath: "C",
			Fset:    l.fset,
			Imports: make(map[string]*packages.Package),
			Types:   tpkg,
		}
		l.packages[path] = pkg
		return pkg, nil
	}
	meta := l.meta[path]
	if meta == nil {
		return nil, unsupportedError{reason: "missing lazy-load metadata for " + path}
	}
	pkg := l.packages[path]
	if l.loading[path] {
		if pkg != nil {
			return pkg, nil
		}
		return nil, unsupportedError{reason: "lazy-load cycle"}
	}
	if pkg != nil && (pkg.Types != nil || len(pkg.Errors) > 0) {
		return pkg, nil
	}
	l.loading[path] = true
	defer delete(l.loading, path)
	l.stats.packages++
	_, isTarget := l.targets[path]
	isLocal := l.isLocalPackage(path, meta)
	if isLocal {
		l.stats.localPackages++
	} else {
		l.stats.externalPackages++
	}

	if pkg == nil {
		pkg = &packages.Package{
			ID:              meta.ImportPath,
			Name:            meta.Name,
			PkgPath:         meta.ImportPath,
			Fset:            l.fset,
			GoFiles:         append([]string(nil), metaFiles(meta)...),
			CompiledGoFiles: append([]string(nil), metaFiles(meta)...),
			Imports:         make(map[string]*packages.Package),
			ExportFile:      meta.Export,
		}
		l.packages[path] = pkg
	}
	useArtifact := l.shouldUseArtifact(path, meta, isTarget, isLocal)
	if useArtifact {
		if typed, ok := l.readArtifact(path, meta, isLocal); ok {
			linkStart := time.Now()
			for _, imp := range meta.Imports {
				target := imp
				if mapped := meta.ImportMap[imp]; mapped != "" {
					target = mapped
				}
				dep, err := l.loadPackage(target)
				if err != nil {
					return nil, err
				}
				pkg.Imports[imp] = dep
			}
			l.stats.artifactImportLink += time.Since(linkStart)
			pkg.Types = typed
			pkg.TypesInfo = nil
			pkg.Syntax = nil
			return pkg, nil
		}
	}
	files, parseErrs := l.parseFiles(metaFiles(meta), isLocal)
	pkg.Errors = append(pkg.Errors, parseErrs...)
	if len(files) == 0 {
		if meta.Error != nil && strings.TrimSpace(meta.Error.Err) != "" {
			pkg.Errors = append(pkg.Errors, packages.Error{
				Pos:  "-",
				Msg:  meta.Error.Err,
				Kind: packages.ListError,
			})
		}
		return pkg, nil
	}

	tpkg := l.typesPkgs[path]
	if tpkg == nil || tpkg.Complete() || (tpkg.Scope() != nil && len(tpkg.Scope().Names()) > 0) {
		tpkg = types.NewPackage(meta.ImportPath, meta.Name)
		l.typesPkgs[path] = tpkg
	}
	needFullState := isTarget || isLocal
	var info *types.Info
	if needFullState {
		info = &types.Info{
			Types:      make(map[ast.Expr]types.TypeAndValue),
			Defs:       make(map[*ast.Ident]types.Object),
			Uses:       make(map[*ast.Ident]types.Object),
			Implicits:  make(map[ast.Node]types.Object),
			Scopes:     make(map[ast.Node]*types.Scope),
			Selections: make(map[*ast.SelectorExpr]*types.Selection),
		}
	}
	var typeErrors []packages.Error
	cfg := &types.Config{
		Sizes:            types.SizesFor("gc", runtime.GOARCH),
		IgnoreFuncBodies: !isLocal,
		Importer: importerFunc(func(importPath string) (*types.Package, error) {
			if importPath == "unsafe" {
				return types.Unsafe, nil
			}
			target := importPath
			if mapped := meta.ImportMap[importPath]; mapped != "" {
				target = mapped
			}
			dep, err := l.loadPackage(target)
			if err != nil {
				return nil, err
			}
			pkg.Imports[importPath] = dep
			if dep.Types != nil {
				return dep.Types, nil
			}
			if typed := l.typesPkgs[target]; typed != nil {
				return typed, nil
			}
			if len(dep.Errors) > 0 {
				return nil, dependencyImportError(dep)
			}
			return nil, unsupportedError{reason: "missing typed lazy-load dependency"}
		}),
		Error: func(err error) {
			typeErrors = append(typeErrors, toPackagesError(l.fset, err))
		},
	}
	checker := types.NewChecker(cfg, l.fset, tpkg, info)
	typecheckStart := time.Now()
	if err := l.checkFiles(path, checker, files); err != nil && len(typeErrors) == 0 {
		typeErrors = append(typeErrors, toPackagesError(l.fset, err))
	}
	typecheckDuration := time.Since(typecheckStart)
	l.stats.typecheck += typecheckDuration
	if isLocal {
		l.stats.localTypecheck += typecheckDuration
	} else {
		l.stats.externalTypecheck += typecheckDuration
	}
	if needFullState {
		pkg.Syntax = files
	} else {
		pkg.Syntax = nil
	}
	pkg.Types = tpkg
	pkg.TypesInfo = info
	pkg.Errors = append(pkg.Errors, typeErrors...)
	if shouldWriteArtifact(l.env, isTarget) && len(pkg.Errors) == 0 {
		_ = l.writeArtifact(meta, tpkg, isLocal)
	}
	return pkg, nil
}

func (l *customTypedGraphLoader) useLocalSemanticArtifact(meta *packageMeta) bool {
	if meta == nil {
		return false
	}
	if ok, exists := l.localSemanticOK[meta.ImportPath]; exists {
		return ok
	}
	art, err := semanticcache.Read(l.env, meta.ImportPath, meta.Name, metaFiles(meta))
	if err != nil || art == nil {
		l.localSemanticOK[meta.ImportPath] = false
		return false
	}
	l.localSemanticOK[meta.ImportPath] = art.Supported
	return art.Supported
}

func (l *customTypedGraphLoader) checkFiles(path string, checker *types.Checker, files []*ast.File) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = unsupportedError{reason: fmt.Sprintf("typecheck panic in %s: %v", path, r)}
		}
	}()
	return checker.Files(files)
}

func (l *customTypedGraphLoader) readArtifact(path string, meta *packageMeta, isLocal bool) (*types.Package, bool) {
	start := time.Now()
	entry, prefetched := l.artifactPrefetch[path]
	artifactPath := ""
	if prefetched {
		artifactPath = entry.path
		if entry.err != nil {
			debugf(l.ctx, "loader.artifact.read_miss pkg=%s local=%t reason=prefetch_error err=%v", path, isLocal, entry.err)
			l.stats.artifactMisses++
			return nil, false
		}
	} else {
		pathStart := time.Now()
		var err error
		artifactPath, err = loaderArtifactPath(l.env, meta, isLocal)
		l.stats.artifactPath += time.Since(pathStart)
		if err != nil {
			debugf(l.ctx, "loader.artifact.read_miss pkg=%s local=%t reason=path_error err=%v", path, isLocal, err)
			l.stats.artifactRead += time.Since(start)
			l.stats.artifactMisses++
			return nil, false
		}
	}
	if isLocal {
		preloadStart := time.Now()
		for _, imp := range meta.Imports {
			target := imp
			if mapped := meta.ImportMap[imp]; mapped != "" {
				target = mapped
			}
			dep, err := l.loadPackage(target)
			if err != nil {
				debugf(l.ctx, "loader.artifact.read_miss pkg=%s local=%t reason=preload_dep_error dep=%s err=%v", path, isLocal, target, err)
				l.stats.artifactRead += time.Since(start)
				l.stats.artifactMisses++
				return nil, false
			}
			if dep != nil && dep.Types != nil {
				l.typesPkgs[target] = dep.Types
			}
		}
		l.stats.artifactImportLink += time.Since(preloadStart)
	}
	var tpkg *types.Package
	decodeStart := time.Now()
	var err error
	if prefetched {
		tpkg, err = readLoaderArtifactData(entry.data, l.fset, l.typesPkgs, path)
	} else {
		tpkg, err = readLoaderArtifact(artifactPath, l.fset, l.typesPkgs, path)
	}
	l.stats.artifactDecode += time.Since(decodeStart)
	if err != nil {
		debugf(l.ctx, "loader.artifact.read_miss pkg=%s local=%t reason=decode_error err=%v", path, isLocal, err)
		l.stats.artifactRead += time.Since(start)
		l.stats.artifactMisses++
		return nil, false
	}
	if !prefetched {
		l.stats.artifactRead += time.Since(start)
	}
	l.stats.artifactHits++
	l.typesPkgs[path] = tpkg
	return tpkg, true
}

func (l *customTypedGraphLoader) shouldUseArtifact(path string, meta *packageMeta, isTarget, isLocal bool) bool {
	if !loaderArtifactEnabled(l.env) || isTarget {
		return false
	}
	if !isLocal {
		return true
	}
	return l.useLocalSemanticArtifact(meta)
}

func (l *customTypedGraphLoader) prefetchArtifacts() {
	if !loaderArtifactEnabled(l.env) {
		return
	}
	candidates := make([]string, 0, len(l.meta))
	for path, meta := range l.meta {
		_, isTarget := l.targets[path]
		isLocal := l.isLocalPackage(path, meta)
		if l.shouldUseArtifact(path, meta, isTarget, isLocal) {
			candidates = append(candidates, path)
		}
	}
	sort.Strings(candidates)
	if len(candidates) == 0 {
		return
	}
	type result struct {
		pkg   string
		entry artifactPrefetchEntry
		dur   time.Duration
	}
	jobs := make(chan string, len(candidates))
	results := make(chan result, len(candidates))
	workers := 8
	if len(candidates) < workers {
		workers = len(candidates)
	}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				start := time.Now()
				meta := l.meta[path]
				isLocal := l.isLocalPackage(path, meta)
				artifactPath, err := loaderArtifactPath(l.env, meta, isLocal)
				entry := artifactPrefetchEntry{path: artifactPath}
				if err == nil {
					data, readErr := os.ReadFile(artifactPath)
					if readErr != nil {
						entry.err = readErr
					} else {
						entry.data = data
						entry.ok = true
					}
				} else {
					entry.err = err
				}
				results <- result{pkg: path, entry: entry, dur: time.Since(start)}
			}
		}()
	}
	for _, path := range candidates {
		jobs <- path
	}
	close(jobs)
	wg.Wait()
	close(results)
	for res := range results {
		l.artifactPrefetch[res.pkg] = res.entry
		l.stats.artifactRead += res.dur
		pathStart := time.Now()
		_ = res.entry.path
		l.stats.artifactPath += time.Since(pathStart)
	}
}

func (l *customTypedGraphLoader) writeArtifact(meta *packageMeta, pkg *types.Package, isLocal bool) error {
	start := time.Now()
	artifactPath, err := loaderArtifactPath(l.env, meta, isLocal)
	if err != nil {
		debugf(l.ctx, "loader.artifact.write_skip pkg=%s local=%t reason=path_error err=%v", meta.ImportPath, isLocal, err)
		l.stats.artifactWrite += time.Since(start)
		return err
	}
	if artifactUpToDate(l.env, artifactPath, meta, isLocal) {
		debugf(l.ctx, "loader.artifact.write_skip pkg=%s local=%t reason=up_to_date", meta.ImportPath, isLocal)
		l.stats.artifactWrite += time.Since(start)
		return nil
	}
	writeErr := writeLoaderArtifact(artifactPath, l.fset, pkg)
	l.stats.artifactWrite += time.Since(start)
	if writeErr == nil {
		l.stats.artifactWrites++
		debugf(l.ctx, "loader.artifact.write_ok pkg=%s local=%t path=%s", meta.ImportPath, isLocal, artifactPath)
	} else {
		debugf(l.ctx, "loader.artifact.write_fail pkg=%s local=%t err=%v", meta.ImportPath, isLocal, writeErr)
	}
	if writeErr != nil {
		return writeErr
	}
	return nil
}

func shouldWriteArtifact(env []string, isTarget bool) bool {
	if !loaderArtifactEnabled(env) || isTarget {
		return false
	}
	return true
}

func (l *customTypedGraphLoader) isLocalPackage(importPath string, meta *packageMeta) bool {
	if local, ok := l.isLocalCache[importPath]; ok {
		return local
	}
	local := isLocalSourcePackage(l.workspace, meta)
	l.isLocalCache[importPath] = local
	return local
}

func (v *customValidator) importFromExport(path string) (*types.Package, error) {
	if typed := v.packages[path]; typed != nil && typed.Complete() {
		return typed, nil
	}
	if v.importer != nil {
		if imported, err := v.importer.Import(path); err == nil {
			v.packages[path] = imported
			return imported, nil
		}
	}
	meta := v.meta[path]
	if meta == nil {
		return nil, unsupportedError{reason: "missing dependency metadata"}
	}
	if meta.Export == "" {
		return v.loadDependencyFromSource(path)
	}
	exportPath := meta.Export
	if !filepath.IsAbs(exportPath) {
		exportPath = filepath.Join(meta.Dir, exportPath)
	}
	f, err := os.Open(exportPath)
	if err != nil {
		return nil, unsupportedError{reason: "open export data"}
	}
	defer f.Close()
	r, err := gcexportdata.NewReader(f)
	if err != nil {
		return nil, unsupportedError{reason: "read export data"}
	}
	view := make(map[string]*types.Package, len(v.packages))
	for pkgPath, pkg := range v.packages {
		view[pkgPath] = pkg
	}
	tpkg, err := gcexportdata.Read(r, v.fset, view, path)
	if err != nil {
		return v.loadDependencyFromSource(path)
	}
	v.packages[path] = tpkg
	return tpkg, nil
}

func (v *customValidator) loadDependencyFromSource(path string) (*types.Package, error) {
	if typed := v.packages[path]; typed != nil && typed.Complete() {
		return typed, nil
	}
	meta := v.meta[path]
	if meta == nil {
		return nil, unsupportedError{reason: "missing source dependency metadata"}
	}
	if v.loading[path] {
		if typed := v.packages[path]; typed != nil {
			return typed, nil
		}
		return nil, unsupportedError{reason: "dependency cycle"}
	}
	v.loading[path] = true
	defer delete(v.loading, path)

	tpkg := v.packages[path]
	if tpkg == nil {
		tpkg = types.NewPackage(meta.ImportPath, meta.Name)
		v.packages[path] = tpkg
	}
	files, errs := v.parseFiles(metaFiles(meta))
	if len(errs) > 0 {
		return nil, unsupportedError{reason: "dependency parse error"}
	}
	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Implicits:  make(map[ast.Node]types.Object),
		Scopes:     make(map[ast.Node]*types.Scope),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	cfg := &types.Config{
		Importer: importerFunc(func(importPath string) (*types.Package, error) {
			if importPath == "unsafe" {
				return types.Unsafe, nil
			}
			target := importPath
			if mapped := meta.ImportMap[importPath]; mapped != "" {
				target = mapped
			}
			if _, ok := v.touched[target]; ok {
				checked, err := v.validatePackage(target)
				if err != nil {
					return nil, err
				}
				if len(checked.Errors) > 0 {
					return nil, unsupportedError{reason: "touched dependency has validation errors"}
				}
				return v.packages[target], nil
			}
			return v.importFromExport(target)
		}),
		Sizes:            types.SizesFor("gc", runtime.GOARCH),
		IgnoreFuncBodies: true,
	}
	if err := types.NewChecker(cfg, v.fset, tpkg, info).Files(files); err != nil {
		return nil, unsupportedError{reason: "dependency typecheck error"}
	}
	return tpkg, nil
}

func (v *customValidator) parseFiles(names []string) ([]*ast.File, []packages.Error) {
	files := make([]*ast.File, 0, len(names))
	var errs []packages.Error
	for _, name := range names {
		src, err := os.ReadFile(name)
		if err != nil {
			errs = append(errs, packages.Error{Pos: name + ":1", Msg: err.Error(), Kind: packages.ParseError})
			continue
		}
		f, err := parser.ParseFile(v.fset, name, src, parser.AllErrors|parser.ParseComments)
		if err != nil {
			switch typed := err.(type) {
			case scanner.ErrorList:
				for _, parseErr := range typed {
					errs = append(errs, packages.Error{
						Pos:  parseErr.Pos.String(),
						Msg:  parseErr.Msg,
						Kind: packages.ParseError,
					})
				}
			default:
				errs = append(errs, packages.Error{Pos: name + ":1", Msg: err.Error(), Kind: packages.ParseError})
			}
		}
		if f != nil {
			files = append(files, f)
		}
	}
	return files, errs
}

func (l *customTypedGraphLoader) parseFiles(names []string, isLocal bool) ([]*ast.File, []packages.Error) {
	files := make([]*ast.File, 0, len(names))
	var errs []packages.Error
	for _, name := range names {
		readStart := time.Now()
		src, err := os.ReadFile(name)
		readDuration := time.Since(readStart)
		l.stats.read += readDuration
		l.stats.filesRead++
		if isLocal {
			l.stats.localRead += readDuration
			l.stats.localFilesRead++
		} else {
			l.stats.externalRead += readDuration
			l.stats.externalFilesRead++
		}
		if err != nil {
			errs = append(errs, packages.Error{Pos: name + ":1", Msg: err.Error(), Kind: packages.ParseError})
			continue
		}
		var f *ast.File
		parseStart := time.Now()
		if l.parseFile != nil {
			f, err = l.parseFile(l.fset, name, src)
		} else {
			f, err = parser.ParseFile(l.fset, name, src, parser.AllErrors|parser.ParseComments)
		}
		parseDuration := time.Since(parseStart)
		l.stats.parse += parseDuration
		if isLocal {
			l.stats.localParse += parseDuration
		} else {
			l.stats.externalParse += parseDuration
		}
		if err != nil {
			switch typed := err.(type) {
			case scanner.ErrorList:
				for _, parseErr := range typed {
					errs = append(errs, packages.Error{
						Pos:  parseErr.Pos.String(),
						Msg:  parseErr.Msg,
						Kind: packages.ParseError,
					})
				}
			default:
				errs = append(errs, packages.Error{Pos: name + ":1", Msg: err.Error(), Kind: packages.ParseError})
			}
		}
		if f != nil {
			files = append(files, f)
		}
	}
	return files, errs
}

func toPackagesError(fset *token.FileSet, err error) packages.Error {
	switch typed := err.(type) {
	case packages.Error:
		return typed
	case types.Error:
		return packages.Error{
			Pos:  typed.Fset.Position(typed.Pos).String(),
			Msg:  typed.Msg,
			Kind: packages.TypeError,
		}
	default:
		pos := "-"
		if fset != nil {
			if te, ok := err.(interface{ Pos() token.Pos }); ok {
				pos = fset.Position(te.Pos()).String()
			}
		}
		return packages.Error{Pos: pos, Msg: err.Error(), Kind: packages.UnknownError}
	}
}

func dependencyImportError(pkg *packages.Package) error {
	if pkg == nil {
		return unsupportedError{reason: "lazy-load dependency has errors"}
	}
	if pkg.Name == "" {
		return fmt.Errorf("invalid package name: %q", pkg.Name)
	}
	for _, err := range pkg.Errors {
		if strings.TrimSpace(err.Msg) == "" {
			continue
		}
		return fmt.Errorf("%s", err.Msg)
	}
	return unsupportedError{reason: "lazy-load dependency has errors"}
}

type importerFunc func(path string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }

func (v *customValidator) validateDeclaredImports(meta *packageMeta, files []*ast.File) []packages.Error {
	var errs []packages.Error
	for _, file := range files {
		used := usedImportsInFile(file)
		for _, spec := range file.Imports {
			if spec == nil || spec.Path == nil {
				continue
			}
			path := strings.Trim(spec.Path.Value, "\"")
			if path == "" {
				continue
			}
			target := path
			if mapped := meta.ImportMap[path]; mapped != "" {
				target = mapped
			}
			name := importName(spec)
			if name != "_" && name != "." {
				if _, ok := used[name]; !ok {
					errs = append(errs, packages.Error{
						Pos:  v.fset.Position(spec.Pos()).String(),
						Msg:  fmt.Sprintf("%q imported and not used", path),
						Kind: packages.TypeError,
					})
					continue
				}
			}
			if _, err := v.importFromExport(target); err != nil {
				errs = append(errs, packages.Error{
					Pos:  v.fset.Position(spec.Pos()).String(),
					Msg:  fmt.Sprintf("could not import %s", path),
					Kind: packages.TypeError,
				})
			}
		}
	}
	return errs
}

func usedImportsInFile(file *ast.File) map[string]struct{} {
	used := make(map[string]struct{})
	ast.Inspect(file, func(node ast.Node) bool {
		sel, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok || ident.Name == "" {
			return true
		}
		used[ident.Name] = struct{}{}
		return true
	})
	return used
}

func importName(spec *ast.ImportSpec) string {
	if spec == nil || spec.Path == nil {
		return ""
	}
	if spec.Name != nil && spec.Name.Name != "" {
		return spec.Name.Name
	}
	path := strings.Trim(spec.Path.Value, "\"")
	if path == "" {
		return ""
	}
	if slash := strings.LastIndex(path, "/"); slash >= 0 {
		path = path[slash+1:]
	}
	return path
}

func discoverTouchedMetadata(ctx context.Context, req TouchedValidationRequest) (map[string]*packageMeta, error) {
	metas, err := runGoList(ctx, goListRequest{
		WD:       req.WD,
		Env:      req.Env,
		Tags:     req.Tags,
		Patterns: req.Touched,
		NeedDeps: true,
	})
	if err != nil {
		return nil, err
	}
	if len(metas) == 0 {
		return nil, unsupportedError{reason: "empty go list result"}
	}
	for _, touched := range req.Touched {
		if _, ok := metas[touched]; !ok {
			return nil, unsupportedError{reason: "missing touched package in metadata"}
		}
	}
	return metas, nil
}

func normalizeImports(imports []string, importMap map[string]string) []string {
	if len(imports) == 0 {
		return nil
	}
	out := make([]string, 0, len(imports))
	for _, imp := range imports {
		if mapped := importMap[imp]; mapped != "" {
			out = append(out, mapped)
			continue
		}
		out = append(out, imp)
	}
	sort.Strings(out)
	return out
}

func metaFiles(meta *packageMeta) []string {
	if meta == nil {
		return nil
	}
	if len(meta.CompiledGoFiles) > 0 {
		return meta.CompiledGoFiles
	}
	return meta.GoFiles
}

func discoverySnapshotForMeta(meta map[string]*packageMeta, complete bool) *DiscoverySnapshot {
	if !complete || len(meta) == 0 {
		return nil
	}
	return &DiscoverySnapshot{meta: meta}
}

func isWorkspacePackage(workspaceRoot, dir string) bool {
	if workspaceRoot == "" || dir == "" {
		return false
	}
	if dir == workspaceRoot {
		return true
	}
	rel, err := filepath.Rel(workspaceRoot, dir)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func isLocalSourcePackage(workspaceRoot string, meta *packageMeta) bool {
	if meta == nil {
		return false
	}
	if isWorkspacePackage(workspaceRoot, meta.Dir) {
		return true
	}
	mod := localSourceModule(meta.Module)
	if mod == nil {
		return false
	}
	if mod.Main {
		return true
	}
	return canonicalLoaderPath(mod.Dir) == canonicalLoaderPath(meta.Dir) || isWorkspacePackage(canonicalLoaderPath(mod.Dir), meta.Dir)
}

func localSourceModule(mod *goListModule) *goListModule {
	if mod == nil {
		return nil
	}
	if mod.Replace != nil {
		if local := localSourceModule(mod.Replace); local != nil {
			return local
		}
	}
	if mod.Main && mod.Dir != "" {
		return mod
	}
	if mod.Replace != nil && mod.Replace.Dir != "" {
		return mod.Replace
	}
	return nil
}

func detectModuleRoot(start string) string {
	start = canonicalLoaderPath(start)
	for dir := start; dir != "" && dir != "." && dir != string(filepath.Separator); dir = filepath.Dir(dir) {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
	}
	return start
}

func canonicalLoaderPath(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil && resolved != "" {
		return filepath.Clean(resolved)
	}
	return path
}

func startLoaderCPUProfile(env []string) (func(), error) {
	path := envValue(env, "WIRE_LOADER_CPU_PROFILE")
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		pprof.StopCPUProfile()
		_ = f.Close()
	}, nil
}

func envValue(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		name, value, ok := strings.Cut(env[i], "=")
		if ok && name == key {
			return value
		}
	}
	return ""
}

func touchedPackageStub(fset *token.FileSet, meta *packageMeta) *packages.Package {
	if meta == nil {
		return nil
	}
	return &packages.Package{
		ID:              meta.ImportPath,
		Name:            meta.Name,
		PkgPath:         meta.ImportPath,
		Fset:            fset,
		GoFiles:         append([]string(nil), metaFiles(meta)...),
		CompiledGoFiles: append([]string(nil), metaFiles(meta)...),
		Imports:         make(map[string]*packages.Package),
		ExportFile:      meta.Export,
	}
}

func metadataMatchesFingerprint(pkgPath string, meta map[string]*packageMeta, local []LocalPackageFingerprint) bool {
	for _, fp := range local {
		if fp.PkgPath != pkgPath {
			continue
		}
		pm := meta[pkgPath]
		if pm == nil {
			return false
		}
		want := append([]string(nil), fp.Files...)
		got := append([]string(nil), metaFiles(pm)...)
		sort.Strings(want)
		sort.Strings(got)
		if len(want) != len(got) {
			return false
		}
		for i := range want {
			if filepath.Clean(want[i]) != filepath.Clean(got[i]) {
				return false
			}
		}
		return true
	}
	return true
}
