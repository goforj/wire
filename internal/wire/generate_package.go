// Copyright 2018 The Wire Authors
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
	"errors"
	"fmt"
	"go/format"
	"path/filepath"
	"time"

	"golang.org/x/tools/go/packages"
)

// generateForPackage runs Wire code generation for a single package.
func generateForPackage(ctx context.Context, pkg *packages.Package, loader *lazyLoader, opts *GenerateOptions) GenerateResult {
	if opts == nil {
		opts = &GenerateOptions{}
	}
	pkgStart := time.Now()
	res := GenerateResult{
		PkgPath: pkg.PkgPath,
	}
	dirStart := time.Now()
	outDir, err := detectOutputDir(pkg.GoFiles)
	logTiming(ctx, "generate.package."+pkg.PkgPath+".output_dir", dirStart)
	if err != nil {
		res.Errs = append(res.Errs, err)
		return res
	}
	res.OutputPath = filepath.Join(outDir, opts.PrefixOutputFile+"wire_gen.go")
	cacheKey, err := cacheKeyForPackage(pkg, opts)
	if err != nil {
		res.Errs = append(res.Errs, err)
		return res
	}
	if cacheKey != "" {
		cacheHitStart := time.Now()
		if cached, ok := readCache(cacheKey); ok {
			res.Content = cached
			logTiming(ctx, "generate.package."+pkg.PkgPath+".cache_hit", cacheHitStart)
			logTiming(ctx, "generate.package."+pkg.PkgPath+".total", pkgStart)
			return res
		}
	}
	oc := newObjectCache([]*packages.Package{pkg}, loader)
	if loaded, errs := oc.ensurePackage(pkg.PkgPath); len(errs) > 0 {
		res.Errs = append(res.Errs, errs...)
		return res
	} else if loaded != nil {
		pkg = loaded
	}
	g := newGen(pkg)
	injectorStart := time.Now()
	injectorFiles, errs := generateInjectors(oc, g, pkg)
	logTiming(ctx, "generate.package."+pkg.PkgPath+".injectors", injectorStart)
	if len(errs) > 0 {
		res.Errs = errs
		return res
	}
	copyStart := time.Now()
	copyNonInjectorDecls(g, injectorFiles, pkg.TypesInfo)
	logTiming(ctx, "generate.package."+pkg.PkgPath+".copy_non_injectors", copyStart)
	frameStart := time.Now()
	goSrc := g.frame(opts.Tags)
	logTiming(ctx, "generate.package."+pkg.PkgPath+".frame", frameStart)
	if len(opts.Header) > 0 {
		goSrc = append(opts.Header, goSrc...)
	}
	formatStart := time.Now()
	fmtSrc, err := format.Source(goSrc)
	logTiming(ctx, "generate.package."+pkg.PkgPath+".format", formatStart)
	if err != nil {
		// This is likely a bug from a poorly generated source file.
		// Add an error but also the unformatted source.
		res.Errs = append(res.Errs, err)
	} else {
		goSrc = fmtSrc
	}
	res.Content = goSrc
	if cacheKey != "" && len(res.Errs) == 0 {
		writeCache(cacheKey, res.Content)
	}
	logTiming(ctx, "generate.package."+pkg.PkgPath+".total", pkgStart)
	return res
}

// allGeneratedOK reports whether every package result succeeded.
func allGeneratedOK(results []GenerateResult) bool {
	if len(results) == 0 {
		return false
	}
	for _, res := range results {
		if len(res.Errs) > 0 {
			return false
		}
	}
	return true
}

// detectOutputDir returns a shared directory for the provided file paths.
func detectOutputDir(paths []string) (string, error) {
	if len(paths) == 0 {
		return "", errors.New("no files to derive output directory from")
	}
	dir := filepath.Dir(paths[0])
	for _, p := range paths[1:] {
		if dir2 := filepath.Dir(p); dir2 != dir {
			return "", fmt.Errorf("found conflicting directories %q and %q", dir, dir2)
		}
	}
	return dir, nil
}
