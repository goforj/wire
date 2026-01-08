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
	"time"

	"go/token"
	"golang.org/x/tools/go/packages"
)

// Serve watches for Go file changes and regenerates wire output on change.
func Serve(ctx context.Context, wd string, env []string, patterns []string, opts *GenerateOptions, interval time.Duration) error {
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	var nextRetry time.Time
	state, err := serveStateFor(ctx, wd, env, patterns, opts)
	if err != nil {
		reportServeError(err)
		nextRetry = time.Now().Add(2 * time.Second)
	}
	if err := generateAndCommit(ctx, wd, env, patterns, opts); err != nil {
		reportServeError(err)
		nextRetry = time.Now().Add(2 * time.Second)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if state == nil {
				if err := generateAndCommit(ctx, wd, env, patterns, opts); err != nil {
					reportServeError(err)
				}
				next, err := serveStateFor(ctx, wd, env, patterns, opts)
				if err != nil {
					reportServeError(err)
					continue
				}
				state = next
				continue
			}
			changedFiles, unknown, err := state.watch.changed(wd)
			if err != nil {
				reportServeError(err)
				unknown = true
			}
			if len(changedFiles) == 0 && !unknown {
				continue
			}
			if len(changedFiles) == 0 && unknown && time.Now().Before(nextRetry) {
				continue
			}
			if unknown || state.manifest == nil {
				if err := generateAndCommit(ctx, wd, env, patterns, opts); err != nil {
					reportServeError(err)
					nextRetry = time.Now().Add(2 * time.Second)
				}
				next, err := serveStateFor(ctx, wd, env, patterns, opts)
				if err != nil {
					reportServeError(err)
					nextRetry = time.Now().Add(2 * time.Second)
					continue
				}
				state = next
				continue
			}
			changedPkgs := state.packagesForFiles(changedFiles)
			if len(changedPkgs) == 0 {
				if err := generateAndCommit(ctx, wd, env, patterns, opts); err != nil {
					reportServeError(err)
				}
				next, err := serveStateFor(ctx, wd, env, patterns, opts)
				if err != nil {
					reportServeError(err)
					continue
				}
				state = next
				continue
			}
			for _, pkgPath := range changedPkgs {
				if state.manifest != nil {
					if ok, err := state.tryCachedWrite(pkgPath, opts); err != nil {
						reportServeError(err)
						nextRetry = time.Now().Add(2 * time.Second)
					} else if ok {
						continue
					}
				}
				meta, err := generateAndCommitPackage(ctx, state.loader, pkgPath, opts)
				if err != nil {
					reportServeError(err)
					nextRetry = time.Now().Add(2 * time.Second)
				} else {
					state.updateManifestPackage(meta)
				}
			}
			state.rebuildWatch()
		}
	}
}

func generateAndCommit(ctx context.Context, wd string, env []string, patterns []string, opts *GenerateOptions) error {
	outs, errs := Generate(ctx, wd, env, patterns, opts)
	if len(errs) > 0 {
		return fmt.Errorf("generate failed: %w", errs[0])
	}
	for _, out := range outs {
		if len(out.Errs) > 0 {
			return fmt.Errorf("generate failed: %w", out.Errs[0])
		}
		if len(out.Content) == 0 {
			continue
		}
		if err := out.Commit(); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wire: %s: wrote %s\n", out.PkgPath, out.OutputPath)
	}
	return nil
}

func reportServeError(err error) {
	fmt.Fprintf(os.Stderr, "wire: serve error: %v\n", err)
}

type watchState struct {
	files map[string]cacheFile
	dirs  map[string]int64
}

type serveState struct {
	manifest  *cacheManifest
	fileToPkg map[string]string
	watch     *watchState
	loader    *lazyLoader
}

func buildWatchState(files []cacheFile) (*watchState, error) {
	state := &watchState{
		files: make(map[string]cacheFile, len(files)),
		dirs:  make(map[string]int64),
	}
	for _, file := range files {
		state.files[file.Path] = file
		dir := filepath.Dir(file.Path)
		for {
			if _, ok := state.dirs[dir]; !ok {
				info, err := os.Stat(dir)
				if err != nil {
					return nil, err
				}
				state.dirs[dir] = info.ModTime().UnixNano()
			}
			if dir == string(filepath.Separator) || dir == "." {
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return state, nil
}

func (ws *watchState) changed(wd string) ([]string, bool, error) {
	var changedFiles []string
	for path, old := range ws.files {
		info, err := os.Stat(path)
		if err != nil {
			return []string{path}, false, nil
		}
		if info.Size() != old.Size || info.ModTime().UnixNano() != old.ModTime {
			changedFiles = append(changedFiles, path)
		}
	}
	if len(changedFiles) > 0 {
		return changedFiles, false, nil
	}
	for dir, old := range ws.dirs {
		info, err := os.Stat(dir)
		if err != nil {
			return nil, true, nil
		}
		if info.ModTime().UnixNano() != old {
			return nil, true, nil
		}
	}
	return nil, false, nil
}

func packageFilesFromList(pkgs []*packages.Package) []string {
	seen := make(map[string]struct{})
	var files []string
	stack := append([]*packages.Package(nil), pkgs...)
	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if p == nil {
			continue
		}
		if _, ok := seen[p.PkgPath]; ok {
			continue
		}
		seen[p.PkgPath] = struct{}{}
		if len(p.CompiledGoFiles) > 0 {
			files = append(files, p.CompiledGoFiles...)
		} else if len(p.GoFiles) > 0 {
			files = append(files, p.GoFiles...)
		}
		for _, imp := range p.Imports {
			stack = append(stack, imp)
		}
	}
	return files
}

func serveStateFor(ctx context.Context, wd string, env []string, patterns []string, opts *GenerateOptions) (*serveState, error) {
	key := manifestKey(wd, env, patterns, opts)
	manifest, ok := readManifest(key)
	if ok && manifestValid(manifest) {
		return serveStateFromManifest(ctx, manifest, wd, env, opts), nil
	}
	pkgs, _, errs := load(ctx, wd, env, opts.Tags, patterns)
	if len(errs) > 0 {
		return nil, errs[0]
	}
	files := packageFilesFromList(pkgs)
	metaFiles, err := buildCacheFiles(files)
	if err != nil {
		return nil, err
	}
	metaFiles = append(metaFiles, extraCacheFiles(wd)...)
	watch, err := buildWatchState(metaFiles)
	if err != nil {
		return nil, err
	}
	fileToPkg := make(map[string]string)
	for _, pkg := range pkgs {
		for _, name := range packageFiles(pkg) {
			fileToPkg[filepath.Clean(name)] = pkg.PkgPath
		}
	}
	loader := &lazyLoader{
		ctx:       ctx,
		wd:        wd,
		env:       env,
		tags:      opts.Tags,
		fset:      token.NewFileSet(),
		baseFiles: buildBaseFilesFromPackages(pkgs),
	}
	return &serveState{
		manifest:  nil,
		fileToPkg: fileToPkg,
		watch:     watch,
		loader:    loader,
	}, nil
}

func serveStateFromManifest(ctx context.Context, manifest *cacheManifest, wd string, env []string, opts *GenerateOptions) *serveState {
	fileToPkg := make(map[string]string)
	baseFiles := make(map[string]map[string]struct{})
	var files []cacheFile
	for _, pkg := range manifest.Packages {
		files = append(files, pkg.Files...)
		for _, file := range pkg.Files {
			path := filepath.Clean(file.Path)
			fileToPkg[path] = pkg.PkgPath
			if baseFiles[pkg.PkgPath] == nil {
				baseFiles[pkg.PkgPath] = make(map[string]struct{})
			}
			baseFiles[pkg.PkgPath][path] = struct{}{}
		}
	}
	files = append(files, manifest.ExtraFiles...)
	watch, err := buildWatchState(files)
	if err != nil {
		watch = nil
	}
	loader := &lazyLoader{
		ctx:       ctx,
		wd:        wd,
		env:       env,
		tags:      opts.Tags,
		fset:      token.NewFileSet(),
		baseFiles: baseFiles,
	}
	return &serveState{
		manifest:  manifest,
		fileToPkg: fileToPkg,
		watch:     watch,
		loader:    loader,
	}
}

func buildBaseFilesFromPackages(pkgs []*packages.Package) map[string]map[string]struct{} {
	baseFiles := make(map[string]map[string]struct{})
	for _, pkg := range pkgs {
		if pkg == nil {
			continue
		}
		files := make(map[string]struct{})
		for _, name := range pkg.CompiledGoFiles {
			files[filepath.Clean(name)] = struct{}{}
		}
		if len(files) == 0 {
			for _, name := range pkg.GoFiles {
				files[filepath.Clean(name)] = struct{}{}
			}
		}
		if len(files) > 0 {
			baseFiles[pkg.PkgPath] = files
		}
	}
	return baseFiles
}

func (ss *serveState) packagesForFiles(files []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, file := range files {
		if pkgPath, ok := ss.fileToPkg[filepath.Clean(file)]; ok {
			if _, exists := seen[pkgPath]; exists {
				continue
			}
			seen[pkgPath] = struct{}{}
			out = append(out, pkgPath)
		}
	}
	return out
}

func (ss *serveState) updateManifestPackage(meta manifestPackage) {
	if ss.manifest == nil {
		return
	}
	for i := range ss.manifest.Packages {
		if ss.manifest.Packages[i].PkgPath == meta.PkgPath {
			ss.manifest.Packages[i] = meta
			writeManifestFile(manifestKeyFromManifest(ss.manifest), ss.manifest)
			ss.updateFileToPkg(meta)
			return
		}
	}
	ss.manifest.Packages = append(ss.manifest.Packages, meta)
	writeManifestFile(manifestKeyFromManifest(ss.manifest), ss.manifest)
	ss.updateFileToPkg(meta)
}

func (ss *serveState) rebuildWatch() {
	if ss.manifest == nil {
		return
	}
	var files []cacheFile
	for _, pkg := range ss.manifest.Packages {
		files = append(files, pkg.Files...)
	}
	watch, err := buildWatchState(files)
	if err == nil {
		ss.watch = watch
	}
}

func (ss *serveState) updateFileToPkg(meta manifestPackage) {
	if ss.fileToPkg == nil {
		ss.fileToPkg = make(map[string]string)
	}
	for _, file := range meta.Files {
		ss.fileToPkg[filepath.Clean(file.Path)] = meta.PkgPath
	}
}

func (ss *serveState) tryCachedWrite(pkgPath string, opts *GenerateOptions) (bool, error) {
	if ss.manifest == nil {
		return false, nil
	}
	var pkg manifestPackage
	found := false
	for _, entry := range ss.manifest.Packages {
		if entry.PkgPath == pkgPath {
			pkg = entry
			found = true
			break
		}
	}
	if !found {
		return false, nil
	}
	files := make([]string, 0, len(pkg.Files))
	for _, f := range pkg.Files {
		files = append(files, filepath.Clean(f.Path))
	}
	sort.Strings(files)
	contentHash, err := contentHashForPaths(pkg.PkgPath, opts, files)
	if err != nil {
		return false, err
	}
	content, ok := readCache(contentHash)
	if !ok {
		return false, nil
	}
	metaFiles, err := buildCacheFiles(files)
	if err != nil {
		return false, err
	}
	pkg.Files = metaFiles
	pkg.ContentHash = contentHash
	if err := os.WriteFile(pkg.OutputPath, content, 0666); err != nil {
		return false, err
	}
	fmt.Fprintf(os.Stderr, "wire: %s: wrote %s\n", pkg.PkgPath, pkg.OutputPath)
	ss.updateManifestPackage(pkg)
	return true, nil
}

func generateAndCommitPackage(ctx context.Context, loader *lazyLoader, pkgPath string, opts *GenerateOptions) (manifestPackage, error) {
	if loader == nil {
		return manifestPackage{}, fmt.Errorf("no loader available")
	}
	pkgs, errs := loader.load(pkgPath)
	if len(errs) > 0 {
		return manifestPackage{}, fmt.Errorf("generate failed: %w", errs[0])
	}
	if len(pkgs) == 0 {
		return manifestPackage{}, fmt.Errorf("generate failed: no package loaded for %s", pkgPath)
	}
	pkg := pkgs[0]
	res := generateForPackage(ctx, pkg, loader, opts)
	if len(res.Errs) > 0 {
		return manifestPackage{}, fmt.Errorf("generate failed: %w", res.Errs[0])
	}
	if len(res.Content) == 0 {
		return manifestPackage{}, nil
	}
	if err := res.Commit(); err != nil {
		return manifestPackage{}, err
	}
	fmt.Fprintf(os.Stderr, "wire: %s: wrote %s\n", res.PkgPath, res.OutputPath)
	meta, err := manifestPackageFromLoaded(pkg, opts)
	if err != nil {
		return manifestPackage{}, err
	}
	return meta, nil
}

func manifestPackageFromLoaded(pkg *packages.Package, opts *GenerateOptions) (manifestPackage, error) {
	files := packageFiles(pkg)
	if len(files) == 0 {
		return manifestPackage{}, fmt.Errorf("no files for package %s", pkg.PkgPath)
	}
	sort.Strings(files)
	metaFiles, err := buildCacheFiles(files)
	if err != nil {
		return manifestPackage{}, err
	}
	contentHash, err := cacheKeyForPackage(pkg, opts)
	if err != nil || contentHash == "" {
		return manifestPackage{}, err
	}
	outDir, err := detectOutputDir(pkg.GoFiles)
	if err != nil {
		return manifestPackage{}, err
	}
	outputPath := filepath.Join(outDir, opts.PrefixOutputFile+"wire_gen.go")
	return manifestPackage{
		PkgPath:     pkg.PkgPath,
		OutputPath:  outputPath,
		Files:       metaFiles,
		ContentHash: contentHash,
	}, nil
}
