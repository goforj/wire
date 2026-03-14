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
	"fmt"
	"go/ast"
	"go/format"
	importerpkg "go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"golang.org/x/tools/go/packages"
)

func tryIncrementalLocalFastPath(ctx context.Context, wd string, env []string, patterns []string, opts *GenerateOptions, state *incrementalPreloadState) ([]GenerateResult, bool, bool, []error) {
	if state == nil || state.manifest == nil {
		return nil, false, false, nil
	}
	if !strings.HasSuffix(state.reason, ".shape_mismatch") {
		return nil, false, false, nil
	}
	roots := manifestOutputPkgPaths(state.manifest)
	if len(roots) != 1 {
		return nil, false, false, nil
	}
	changed := changedPackagePaths(state.manifest.LocalPackages, state.currentLocal)
	if len(changed) != 1 {
		return nil, false, false, nil
	}
	graph, ok := readIncrementalGraph(incrementalGraphKey(wd, opts.Tags, roots))
	if !ok {
		return nil, false, false, nil
	}
	affected := affectedRoots(graph, changed)
	if len(affected) != 1 || affected[0] != roots[0] {
		return nil, false, false, nil
	}

	fastPathStart := time.Now()
	loaded, err := loadLocalPackagesForFastPath(ctx, wd, opts.Tags, roots[0], state.currentLocal, state.manifest.ExternalPkgs)
	if err != nil {
		debugf(ctx, "incremental.local_fastpath miss reason=%v", err)
		if shouldBypassIncrementalManifestAfterFastPathError(err) {
			return nil, true, true, []error{err}
		}
		return nil, false, false, nil
	}
	logTiming(ctx, "incremental.local_fastpath.load", fastPathStart)

	generated, errs := generateFromTypedPackages(ctx, loaded.root, loaded.allPackages, opts)
	logTiming(ctx, "incremental.local_fastpath.generate", fastPathStart)
	if len(errs) > 0 {
		return nil, true, true, errs
	}

	snapshot := &incrementalFingerprintSnapshot{
		fingerprints: loaded.fingerprints,
		changed:      append([]string(nil), changed...),
	}
	loader := &lazyLoader{
		ctx:          ctx,
		wd:           wd,
		env:          env,
		tags:         opts.Tags,
		fset:         loaded.fset,
		fingerprints: snapshot,
		loaded:       make(map[string]*packages.Package, len(loaded.byPath)),
	}
	for path, pkg := range loaded.byPath {
		loader.loaded[path] = pkg
	}
	writeIncrementalFingerprints(snapshot, wd, opts.Tags)
	writeIncrementalPackageSummaries(loader, loaded.allPackages)
	writeIncrementalManifestFromState(wd, env, patterns, opts, state, snapshot, generated)
	writeIncrementalGraphFromSnapshot(wd, opts.Tags, roots, loaded.fingerprints)

	debugf(ctx, "incremental.local_fastpath hit root=%s changed=%s", roots[0], strings.Join(changed, ","))
	return generated, true, false, nil
}

func shouldBypassIncrementalManifestAfterFastPathError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "type-check failed for ")
}

func formatLocalTypeCheckError(wd string, pkgPath string, errs []packages.Error) error {
	if len(errs) == 0 {
		return fmt.Errorf("type-check failed for %s", pkgPath)
	}
	root := findModuleRoot(wd)
	lines := []string{}
	for _, pkgErr := range errs {
		details := normalizeErrorLines(pkgErr.Msg, root)
		if len(details) == 0 {
			continue
		}
		lines = append(lines, fmt.Sprintf("type-check failed for %s: %s", pkgPath, details[0]))
		for _, line := range details[1:] {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		lines = append(lines, fmt.Sprintf("type-check failed for %s", pkgPath))
	}
	return fmt.Errorf("%s", strings.Join(lines, "\n"))
}

func normalizeErrorLines(msg string, root string) []string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return []string{"unknown error"}
	}
	lines := unfoldTypeCheckChain(msg)
	for i := range lines {
		lines[i] = relativizeErrorLine(lines[i], root)
	}
	if len(lines) == 0 {
		return []string{"unknown error"}
	}
	return lines
}

func relativizeErrorLine(line string, root string) string {
	if root == "" {
		return line
	}
	cleanRoot := filepath.Clean(root)
	prefix := cleanRoot + string(os.PathSeparator)
	return strings.ReplaceAll(line, prefix, "")
}

func unfoldTypeCheckChain(msg string) []string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return nil
	}
	if inner, outer, ok := splitNestedTypeCheck(msg); ok {
		lines := []string{strings.TrimSpace(outer)}
		return append(lines, unfoldTypeCheckChain(inner)...)
	}
	parts := strings.Split(msg, "\n")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lines = append(lines, part)
	}
	return lines
}

func splitNestedTypeCheck(msg string) (inner string, outer string, ok bool) {
	msg = strings.TrimSpace(msg)
	if len(msg) < 2 || msg[len(msg)-1] != ')' {
		return "", "", false
	}
	depth := 0
	for i := len(msg) - 1; i >= 0; i-- {
		switch msg[i] {
		case ')':
			depth++
		case '(':
			depth--
			if depth == 0 {
				inner = strings.TrimSpace(msg[i+1 : len(msg)-1])
				if strings.HasPrefix(inner, "type-check failed for ") {
					return inner, strings.TrimSpace(msg[:i]), true
				}
				return "", "", false
			}
		}
	}
	return "", "", false
}

type localFastPathLoaded struct {
	fset         *token.FileSet
	root         *packages.Package
	allPackages  []*packages.Package
	byPath       map[string]*packages.Package
	fingerprints map[string]*packageFingerprint
}

type localFastPathLoader struct {
	ctx          context.Context
	wd           string
	tags         string
	fset         *token.FileSet
	rootPkgPath  string
	meta         map[string]*packageFingerprint
	pkgs         map[string]*packages.Package
	externalMeta map[string]externalPackageExport
	externalImp  types.Importer
}

func loadLocalPackagesForFastPath(ctx context.Context, wd string, tags string, rootPkgPath string, current []packageFingerprint, external []externalPackageExport) (*localFastPathLoaded, error) {
	meta := fingerprintsFromSlice(current)
	if len(meta) == 0 {
		return nil, fmt.Errorf("no local fingerprints")
	}
	if meta[rootPkgPath] == nil {
		return nil, fmt.Errorf("missing root package fingerprint")
	}
	externalMeta := make(map[string]externalPackageExport, len(external))
	for _, item := range external {
		if item.PkgPath == "" || item.ExportFile == "" {
			continue
		}
		externalMeta[item.PkgPath] = item
	}
	loader := &localFastPathLoader{
		ctx:          ctx,
		wd:           wd,
		tags:         tags,
		fset:         token.NewFileSet(),
		rootPkgPath:  rootPkgPath,
		meta:         meta,
		pkgs:         make(map[string]*packages.Package, len(meta)),
		externalMeta: externalMeta,
	}
	loader.externalImp = importerpkg.ForCompiler(loader.fset, "gc", loader.openExternalExport)
	root, err := loader.load(rootPkgPath)
	if err != nil {
		return nil, err
	}
	all := make([]*packages.Package, 0, len(loader.pkgs))
	for _, pkg := range loader.pkgs {
		all = append(all, pkg)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].PkgPath < all[j].PkgPath })
	return &localFastPathLoaded{
		fset:         loader.fset,
		root:         root,
		allPackages:  all,
		byPath:       loader.pkgs,
		fingerprints: loader.meta,
	}, nil
}

func (l *localFastPathLoader) load(pkgPath string) (*packages.Package, error) {
	if pkg := l.pkgs[pkgPath]; pkg != nil {
		return pkg, nil
	}
	fp := l.meta[pkgPath]
	if fp == nil {
		return nil, fmt.Errorf("package %s not tracked as local", pkgPath)
	}
	files := filesFromMeta(fp.Files)
	if len(files) == 0 {
		return nil, fmt.Errorf("package %s has no files", pkgPath)
	}
	mode := parser.ParseComments | parser.SkipObjectResolution
	syntax := make([]*ast.File, 0, len(files))
	for _, name := range files {
		file, err := parser.ParseFile(l.fset, name, nil, mode)
		if err != nil {
			return nil, err
		}
		syntax = append(syntax, file)
	}
	if len(syntax) == 0 {
		return nil, fmt.Errorf("package %s parsed no files", pkgPath)
	}

	pkgName := syntax[0].Name.Name
	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Implicits:  make(map[ast.Node]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Scopes:     make(map[ast.Node]*types.Scope),
		Instances:  make(map[*ast.Ident]types.Instance),
	}
	pkg := &packages.Package{
		Fset:            l.fset,
		Name:            pkgName,
		PkgPath:         pkgPath,
		GoFiles:         append([]string(nil), files...),
		CompiledGoFiles: append([]string(nil), files...),
		Syntax:          syntax,
		TypesInfo:       info,
		Imports:         make(map[string]*packages.Package),
	}
	l.pkgs[pkgPath] = pkg

	conf := &types.Config{
		Importer: importerFunc(func(path string) (*types.Package, error) {
			return l.importPackage(path)
		}),
		Sizes: types.SizesFor("gc", runtime.GOARCH),
		Error: func(err error) {
			pkg.Errors = append(pkg.Errors, packages.Error{Msg: err.Error()})
		},
	}
	checkedPkg, err := conf.Check(pkgPath, l.fset, syntax, info)
	if checkedPkg != nil {
		pkg.Types = checkedPkg
	}
	if err != nil && len(pkg.Errors) == 0 {
		return nil, err
	}
	if len(pkg.Errors) > 0 {
		return nil, formatLocalTypeCheckError(l.wd, pkgPath, pkg.Errors)
	}

	imports := packageImportPaths(syntax)
	localImports := make([]string, 0, len(imports))
	for _, path := range imports {
		if dep := l.pkgs[path]; dep != nil {
			pkg.Imports[path] = dep
			localImports = append(localImports, path)
		}
	}
	sort.Strings(localImports)
	updated := *fp
	updated.LocalImports = localImports
	updated.Tags = l.tags
	updated.WD = filepath.Clean(l.wd)
	l.meta[pkgPath] = &updated
	return pkg, nil
}

func (l *localFastPathLoader) importPackage(path string) (*types.Package, error) {
	if l.meta[path] != nil {
		pkg, err := l.load(path)
		if err != nil {
			return nil, err
		}
		return pkg.Types, nil
	}
	if l.externalImp == nil {
		return nil, fmt.Errorf("missing external importer")
	}
	return l.externalImp.Import(path)
}

func (l *localFastPathLoader) openExternalExport(path string) (io.ReadCloser, error) {
	meta, ok := l.externalMeta[path]
	if !ok || meta.ExportFile == "" {
		return nil, fmt.Errorf("missing export data for %s", path)
	}
	return os.Open(meta.ExportFile)
}

type importerFunc func(string) (*types.Package, error)

func (fn importerFunc) Import(path string) (*types.Package, error) {
	return fn(path)
}

func packageImportPaths(files []*ast.File) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, file := range files {
		for _, spec := range file.Imports {
			path := strings.Trim(spec.Path.Value, "\"")
			if path == "" {
				continue
			}
			if _, ok := seen[path]; ok {
				continue
			}
			seen[path] = struct{}{}
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

func generateFromTypedPackages(ctx context.Context, root *packages.Package, allPkgs []*packages.Package, opts *GenerateOptions) ([]GenerateResult, []error) {
	if root == nil {
		return nil, []error{fmt.Errorf("missing root package")}
	}
	if opts == nil {
		opts = &GenerateOptions{}
	}
	pkgStart := time.Now()
	res := GenerateResult{PkgPath: root.PkgPath}
	outDir, err := detectOutputDir(root.GoFiles)
	logTiming(ctx, "generate.package."+root.PkgPath+".output_dir", pkgStart)
	if err != nil {
		res.Errs = append(res.Errs, err)
		return []GenerateResult{res}, nil
	}
	res.OutputPath = filepath.Join(outDir, opts.PrefixOutputFile+"wire_gen.go")

	oc := newObjectCache(allPkgs, nil)
	g := newGen(root)
	injectorStart := time.Now()
	injectorFiles, errs := generateInjectors(oc, g, root)
	logTiming(ctx, "generate.package."+root.PkgPath+".injectors", injectorStart)
	if len(errs) > 0 {
		res.Errs = errs
		return []GenerateResult{res}, nil
	}
	copyStart := time.Now()
	copyNonInjectorDecls(g, injectorFiles, root.TypesInfo)
	logTiming(ctx, "generate.package."+root.PkgPath+".copy_non_injectors", copyStart)
	frameStart := time.Now()
	goSrc := g.frame(opts.Tags)
	logTiming(ctx, "generate.package."+root.PkgPath+".frame", frameStart)
	if len(opts.Header) > 0 {
		goSrc = append(opts.Header, goSrc...)
	}
	formatStart := time.Now()
	fmtSrc, err := format.Source(goSrc)
	logTiming(ctx, "generate.package."+root.PkgPath+".format", formatStart)
	if err != nil {
		res.Errs = append(res.Errs, err)
	} else {
		goSrc = fmtSrc
	}
	res.Content = goSrc
	logTiming(ctx, "generate.package."+root.PkgPath+".total", pkgStart)
	return []GenerateResult{res}, nil
}

func writeIncrementalFingerprints(snapshot *incrementalFingerprintSnapshot, wd string, tags string) {
	if snapshot == nil {
		return
	}
	for _, fp := range snapshotPackageFingerprints(snapshot) {
		fp := fp
		writeIncrementalFingerprint(incrementalFingerprintKey(wd, tags, fp.PkgPath), &fp)
	}
}

func writeIncrementalManifestFromState(wd string, env []string, patterns []string, opts *GenerateOptions, state *incrementalPreloadState, snapshot *incrementalFingerprintSnapshot, generated []GenerateResult) {
	if snapshot == nil || len(generated) == 0 || state == nil || state.manifest == nil {
		return
	}
	manifest := &incrementalManifest{
		Version:       incrementalManifestVersion,
		WD:            filepath.Clean(wd),
		Tags:          opts.Tags,
		Prefix:        opts.PrefixOutputFile,
		HeaderHash:    headerHash(opts.Header),
		EnvHash:       envHash(env),
		Patterns:      sortedStrings(patterns),
		LocalPackages: snapshotPackageFingerprints(snapshot),
		ExternalPkgs:  append([]externalPackageExport(nil), state.manifest.ExternalPkgs...),
		ExternalFiles: append([]cacheFile(nil), state.manifest.ExternalFiles...),
		ExtraFiles:    extraCacheFiles(wd),
	}
	for _, out := range generated {
		if len(out.Content) == 0 || out.OutputPath == "" {
			continue
		}
		contentKey := incrementalContentKey(out.Content)
		writeCache(contentKey, out.Content)
		manifest.Outputs = append(manifest.Outputs, incrementalOutput{
			PkgPath:    out.PkgPath,
			OutputPath: out.OutputPath,
			ContentKey: contentKey,
		})
	}
	if len(manifest.Outputs) == 0 {
		return
	}
	selectorKey := incrementalManifestSelectorKey(wd, env, patterns, opts)
	writeIncrementalManifestFile(selectorKey, manifest)
	writeIncrementalManifestFile(incrementalManifestStateKey(selectorKey, manifest.LocalPackages), manifest)
}

func writeIncrementalGraphFromSnapshot(wd string, tags string, roots []string, fps map[string]*packageFingerprint) {
	if len(roots) == 0 || len(fps) == 0 {
		return
	}
	graph := &incrementalGraph{
		Version:      incrementalGraphVersion,
		WD:           filepath.Clean(wd),
		Tags:         tags,
		Roots:        append([]string(nil), roots...),
		LocalReverse: make(map[string][]string),
	}
	sort.Strings(graph.Roots)
	for _, fp := range fps {
		if fp == nil {
			continue
		}
		for _, imp := range fp.LocalImports {
			graph.LocalReverse[imp] = append(graph.LocalReverse[imp], fp.PkgPath)
		}
	}
	for path := range graph.LocalReverse {
		sort.Strings(graph.LocalReverse[path])
	}
	writeIncrementalGraph(incrementalGraphKey(wd, tags, graph.Roots), graph)
}

func manifestOutputPkgPaths(manifest *incrementalManifest) []string {
	if manifest == nil || len(manifest.Outputs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(manifest.Outputs))
	paths := make([]string, 0, len(manifest.Outputs))
	for _, out := range manifest.Outputs {
		if out.PkgPath == "" {
			continue
		}
		if _, ok := seen[out.PkgPath]; ok {
			continue
		}
		seen[out.PkgPath] = struct{}{}
		paths = append(paths, out.PkgPath)
	}
	sort.Strings(paths)
	return paths
}

func changedPackagePaths(previous []packageFingerprint, current []packageFingerprint) []string {
	if len(current) == 0 {
		return nil
	}
	prevByPath := make(map[string]packageFingerprint, len(previous))
	for _, fp := range previous {
		prevByPath[fp.PkgPath] = fp
	}
	changed := make([]string, 0, len(current))
	for _, fp := range current {
		prev, ok := prevByPath[fp.PkgPath]
		if !ok || !incrementalFingerprintEquivalent(&prev, &fp) {
			changed = append(changed, fp.PkgPath)
		}
	}
	sort.Strings(changed)
	return changed
}
