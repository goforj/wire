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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/tools/go/packages"
)

type parseFileStats struct {
	mu           sync.Mutex
	calls        int
	primaryCalls int
	depCalls     int
	cacheHits    int
	cacheMisses  int
	errors       int
	total        time.Duration
}

func (ps *parseFileStats) record(primary bool, dur time.Duration, err error, cacheHit bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.calls++
	if primary {
		ps.primaryCalls++
	} else {
		ps.depCalls++
	}
	if cacheHit {
		ps.cacheHits++
	} else {
		ps.cacheMisses++
	}
	ps.total += dur
	if err != nil {
		ps.errors++
	}
}

func (ps *parseFileStats) snapshot() parseFileStats {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return parseFileStats{
		calls:        ps.calls,
		primaryCalls: ps.primaryCalls,
		depCalls:     ps.depCalls,
		cacheHits:    ps.cacheHits,
		cacheMisses:  ps.cacheMisses,
		errors:       ps.errors,
		total:        ps.total,
	}
}

type loadScopeStats struct {
	roots                 int
	totalPackages         int
	compiledFiles         int
	syntaxFiles           int
	packagesWithSyntax    int
	packagesWithTypes     int
	packagesWithTypesInfo int
	localPackages         int
	localSyntaxPackages   int
	externalPackages      int
	externalSyntaxPkgs    int
	unknownPackages       int
	topCompiled           []string
	topSyntax             []string
}

type packageMetric struct {
	path  string
	count int
}

func logLoadDebug(ctx context.Context, scope string, mode packages.LoadMode, subject string, wd string, pkgs []*packages.Package, parseStats *parseFileStats) {
	if timing(ctx) == nil {
		return
	}
	stats := summarizeLoadScope(wd, pkgs)
	debugf(ctx, "load.debug scope=%s subject=%s mode=%s roots=%d total_pkgs=%d compiled_files=%d syntax_files=%d syntax_pkgs=%d typed_pkgs=%d types_info_pkgs=%d local_pkgs=%d local_syntax_pkgs=%d external_pkgs=%d external_syntax_pkgs=%d unknown_pkgs=%d",
		scope,
		subject,
		formatLoadMode(mode),
		stats.roots,
		stats.totalPackages,
		stats.compiledFiles,
		stats.syntaxFiles,
		stats.packagesWithSyntax,
		stats.packagesWithTypes,
		stats.packagesWithTypesInfo,
		stats.localPackages,
		stats.localSyntaxPackages,
		stats.externalPackages,
		stats.externalSyntaxPkgs,
		stats.unknownPackages,
	)
	if len(stats.topCompiled) > 0 {
		debugf(ctx, "load.debug scope=%s top_compiled_files=%s", scope, strings.Join(stats.topCompiled, ", "))
	}
	if len(stats.topSyntax) > 0 {
		debugf(ctx, "load.debug scope=%s top_syntax_files=%s", scope, strings.Join(stats.topSyntax, ", "))
	}
	if parseStats != nil {
		snap := parseStats.snapshot()
		debugf(ctx, "load.debug scope=%s parse.calls=%d parse.primary=%d parse.deps=%d parse.cache_hits=%d parse.cache_misses=%d parse.errors=%d parse.cumulative=%s",
			scope,
			snap.calls,
			snap.primaryCalls,
			snap.depCalls,
			snap.cacheHits,
			snap.cacheMisses,
			snap.errors,
			snap.total,
		)
	}
}

func summarizeLoadScope(wd string, pkgs []*packages.Package) loadScopeStats {
	all := collectAllPackages(pkgs)
	stats := loadScopeStats{
		roots:         len(pkgs),
		totalPackages: len(all),
	}
	moduleRoot := findModuleRoot(wd)
	var compiled []packageMetric
	var syntax []packageMetric
	for _, pkg := range all {
		if pkg == nil {
			continue
		}
		compiledCount := len(pkg.CompiledGoFiles)
		syntaxCount := len(pkg.Syntax)
		stats.compiledFiles += compiledCount
		stats.syntaxFiles += syntaxCount
		if syntaxCount > 0 {
			stats.packagesWithSyntax++
		}
		if pkg.Types != nil {
			stats.packagesWithTypes++
		}
		if pkg.TypesInfo != nil {
			stats.packagesWithTypesInfo++
		}
		class := classifyPackageLocation(moduleRoot, pkg)
		switch class {
		case "local":
			stats.localPackages++
			if syntaxCount > 0 {
				stats.localSyntaxPackages++
			}
		case "external":
			stats.externalPackages++
			if syntaxCount > 0 {
				stats.externalSyntaxPkgs++
			}
		default:
			stats.unknownPackages++
		}
		if compiledCount > 0 {
			compiled = append(compiled, packageMetric{path: pkg.PkgPath, count: compiledCount})
		}
		if syntaxCount > 0 {
			syntax = append(syntax, packageMetric{path: pkg.PkgPath, count: syntaxCount})
		}
	}
	stats.topCompiled = topPackageMetrics(compiled)
	stats.topSyntax = topPackageMetrics(syntax)
	return stats
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

func classifyPackageLocation(moduleRoot string, pkg *packages.Package) string {
	if moduleRoot == "" || pkg == nil {
		return "unknown"
	}
	for _, name := range pkg.CompiledGoFiles {
		if isWithinRoot(moduleRoot, name) {
			return "local"
		}
		return "external"
	}
	for _, name := range pkg.GoFiles {
		if isWithinRoot(moduleRoot, name) {
			return "local"
		}
		return "external"
	}
	return "unknown"
}

func isWithinRoot(root, name string) bool {
	cleanRoot := canonicalPath(root)
	cleanName := canonicalPath(name)
	if cleanName == cleanRoot {
		return true
	}
	rel, err := filepath.Rel(cleanRoot, cleanName)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func canonicalPath(path string) string {
	clean := filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil && resolved != "" {
		return filepath.Clean(resolved)
	}
	return clean
}

func topPackageMetrics(metrics []packageMetric) []string {
	sort.Slice(metrics, func(i, j int) bool {
		if metrics[i].count == metrics[j].count {
			return metrics[i].path < metrics[j].path
		}
		return metrics[i].count > metrics[j].count
	})
	if len(metrics) > 5 {
		metrics = metrics[:5]
	}
	out := make([]string, 0, len(metrics))
	for _, m := range metrics {
		out = append(out, fmt.Sprintf("%s(%d)", m.path, m.count))
	}
	return out
}

func findModuleRoot(wd string) string {
	dir := filepath.Clean(wd)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func formatLoadMode(mode packages.LoadMode) string {
	flags := []struct {
		bit  packages.LoadMode
		name string
	}{
		{packages.NeedName, "NeedName"},
		{packages.NeedFiles, "NeedFiles"},
		{packages.NeedCompiledGoFiles, "NeedCompiledGoFiles"},
		{packages.NeedImports, "NeedImports"},
		{packages.NeedDeps, "NeedDeps"},
		{packages.NeedExportsFile, "NeedExportsFile"},
		{packages.NeedTypes, "NeedTypes"},
		{packages.NeedSyntax, "NeedSyntax"},
		{packages.NeedTypesInfo, "NeedTypesInfo"},
		{packages.NeedTypesSizes, "NeedTypesSizes"},
		{packages.NeedModule, "NeedModule"},
		{packages.NeedEmbedFiles, "NeedEmbedFiles"},
		{packages.NeedEmbedPatterns, "NeedEmbedPatterns"},
	}
	var parts []string
	for _, flag := range flags {
		if mode&flag.bit != 0 {
			parts = append(parts, flag.name)
		}
	}
	if len(parts) == 0 {
		return "0"
	}
	return strings.Join(parts, "|")
}

func primaryFileSet(files map[string]struct{}) map[string]struct{} {
	if len(files) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(files))
	for name := range files {
		out[filepath.Clean(name)] = struct{}{}
	}
	return out
}

func isPrimaryFile(primary map[string]struct{}, filename string) bool {
	if len(primary) == 0 {
		return false
	}
	_, ok := primary[filepath.Clean(filename)]
	return ok
}
