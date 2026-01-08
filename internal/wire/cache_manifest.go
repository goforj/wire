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
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"sort"

	"golang.org/x/tools/go/packages"
)

// cacheManifest stores per-run cache metadata for generated packages.
type cacheManifest struct {
	Version    string            `json:"version"`
	WD         string            `json:"wd"`
	Tags       string            `json:"tags"`
	Prefix     string            `json:"prefix"`
	HeaderHash string            `json:"header_hash"`
	EnvHash    string            `json:"env_hash"`
	Patterns   []string          `json:"patterns"`
	Packages   []manifestPackage `json:"packages"`
	ExtraFiles []cacheFile       `json:"extra_files"`
}

// manifestPackage captures cached output for a single package.
type manifestPackage struct {
	PkgPath     string      `json:"pkg_path"`
	OutputPath  string      `json:"output_path"`
	Files       []cacheFile `json:"files"`
	ContentHash string      `json:"content_hash"`
	RootFiles   []cacheFile `json:"root_files"`
	RootHash    string      `json:"root_hash"`
}

var extraCachePathsFunc = extraCachePaths

// readManifestResults loads cached generation results if still valid.
func readManifestResults(wd string, env []string, patterns []string, opts *GenerateOptions) ([]GenerateResult, bool) {
	key := manifestKey(wd, env, patterns, opts)
	manifest, ok := readManifest(key)
	if !ok {
		return nil, false
	}
	if !manifestValid(manifest) {
		return nil, false
	}
	results := make([]GenerateResult, 0, len(manifest.Packages))
	for _, pkg := range manifest.Packages {
		content, ok := readCache(pkg.ContentHash)
		if !ok {
			return nil, false
		}
		results = append(results, GenerateResult{
			PkgPath:    pkg.PkgPath,
			OutputPath: pkg.OutputPath,
			Content:    content,
		})
	}
	return results, true
}

// writeManifest persists cache metadata for a successful run.
func writeManifest(wd string, env []string, patterns []string, opts *GenerateOptions, pkgs []*packages.Package) {
	if len(pkgs) == 0 {
		return
	}
	key := manifestKey(wd, env, patterns, opts)
	manifest := &cacheManifest{
		Version:    cacheVersion,
		WD:         wd,
		Tags:       opts.Tags,
		Prefix:     opts.PrefixOutputFile,
		HeaderHash: headerHash(opts.Header),
		EnvHash:    envHash(env),
		Patterns:   sortedStrings(patterns),
	}
	manifest.ExtraFiles = extraCacheFiles(wd)
	for _, pkg := range pkgs {
		if pkg == nil {
			continue
		}
		files := packageFiles(pkg)
		if len(files) == 0 {
			continue
		}
		sort.Strings(files)
		contentHash, err := cacheKeyForPackageFunc(pkg, opts)
		if err != nil || contentHash == "" {
			continue
		}
		outDir, err := detectOutputDirFunc(pkg.GoFiles)
		if err != nil {
			continue
		}
		outputPath := filepath.Join(outDir, opts.PrefixOutputFile+"wire_gen.go")
		metaFiles, err := buildCacheFilesFunc(files)
		if err != nil {
			continue
		}
		rootFiles := rootPackageFilesFunc(pkg)
		sort.Strings(rootFiles)
		rootMeta, err := buildCacheFilesFunc(rootFiles)
		if err != nil {
			continue
		}
		rootHash, err := hashFilesFunc(rootFiles)
		if err != nil {
			continue
		}
		manifest.Packages = append(manifest.Packages, manifestPackage{
			PkgPath:     pkg.PkgPath,
			OutputPath:  outputPath,
			Files:       metaFiles,
			ContentHash: contentHash,
			RootFiles:   rootMeta,
			RootHash:    rootHash,
		})
	}
	writeManifestFile(key, manifest)
}

// manifestKey builds the cache key for a given run configuration.
func manifestKey(wd string, env []string, patterns []string, opts *GenerateOptions) string {
	h := sha256.New()
	h.Write([]byte(cacheVersion))
	h.Write([]byte{0})
	h.Write([]byte(filepath.Clean(wd)))
	h.Write([]byte{0})
	h.Write([]byte(envHash(env)))
	h.Write([]byte{0})
	h.Write([]byte(opts.Tags))
	h.Write([]byte{0})
	h.Write([]byte(opts.PrefixOutputFile))
	h.Write([]byte{0})
	h.Write([]byte(headerHash(opts.Header)))
	h.Write([]byte{0})
	for _, p := range sortedStrings(patterns) {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// manifestKeyFromManifest rebuilds the cache key from stored metadata.
func manifestKeyFromManifest(manifest *cacheManifest) string {
	if manifest == nil {
		return ""
	}
	h := sha256.New()
	h.Write([]byte(cacheVersion))
	h.Write([]byte{0})
	h.Write([]byte(filepath.Clean(manifest.WD)))
	h.Write([]byte{0})
	h.Write([]byte(manifest.EnvHash))
	h.Write([]byte{0})
	h.Write([]byte(manifest.Tags))
	h.Write([]byte{0})
	h.Write([]byte(manifest.Prefix))
	h.Write([]byte{0})
	h.Write([]byte(manifest.HeaderHash))
	h.Write([]byte{0})
	for _, p := range sortedStrings(manifest.Patterns) {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// readManifest loads the cached manifest by key.
func readManifest(key string) (*cacheManifest, bool) {
	data, err := osReadFile(cacheManifestPath(key))
	if err != nil {
		return nil, false
	}
	var manifest cacheManifest
	if err := jsonUnmarshal(data, &manifest); err != nil {
		return nil, false
	}
	return &manifest, true
}

// writeManifestFile writes the manifest to disk.
func writeManifestFile(key string, manifest *cacheManifest) {
	dir := cacheDir()
	if err := osMkdirAll(dir, 0755); err != nil {
		return
	}
	data, err := jsonMarshal(manifest)
	if err != nil {
		return
	}
	tmp, err := osCreateTemp(dir, key+".manifest-")
	if err != nil {
		return
	}
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		osRemove(tmp.Name())
		return
	}
	path := cacheManifestPath(key)
	if err := osRename(tmp.Name(), path); err != nil {
		osRemove(tmp.Name())
	}
}

// cacheManifestPath returns the on-disk path for a manifest key.
func cacheManifestPath(key string) string {
	return filepath.Join(cacheDir(), key+".manifest.json")
}

// manifestValid reports whether the manifest still matches current inputs.
func manifestValid(manifest *cacheManifest) bool {
	if manifest == nil || manifest.Version != cacheVersion {
		return false
	}
	if manifest.EnvHash == "" || len(manifest.Packages) == 0 {
		return false
	}
	if len(manifest.ExtraFiles) > 0 {
		current, err := buildCacheFilesFromMetaFunc(manifest.ExtraFiles)
		if err != nil {
			return false
		}
		if len(current) != len(manifest.ExtraFiles) {
			return false
		}
		for i := range manifest.ExtraFiles {
			if manifest.ExtraFiles[i] != current[i] {
				return false
			}
		}
	}
	for i := range manifest.Packages {
		pkg := manifest.Packages[i]
		if pkg.ContentHash == "" {
			return false
		}
		if len(pkg.RootFiles) == 0 || pkg.RootHash == "" {
			return false
		}
		current, err := buildCacheFilesFromMetaFunc(pkg.Files)
		if err != nil {
			return false
		}
		if len(current) != len(pkg.Files) {
			return false
		}
		for j := range pkg.Files {
			if pkg.Files[j] != current[j] {
				return false
			}
		}
		rootCurrent, err := buildCacheFilesFromMetaFunc(pkg.RootFiles)
		if err != nil {
			return false
		}
		if len(rootCurrent) != len(pkg.RootFiles) {
			return false
		}
		for j := range pkg.RootFiles {
			if pkg.RootFiles[j] != rootCurrent[j] {
				return false
			}
		}
		rootPaths := make([]string, 0, len(pkg.RootFiles))
		for _, file := range pkg.RootFiles {
			rootPaths = append(rootPaths, file.Path)
		}
		sort.Strings(rootPaths)
		rootHash, err := hashFiles(rootPaths)
		if err != nil || rootHash != pkg.RootHash {
			return false
		}
	}
	return true
}

// buildCacheFilesFromMeta re-stats files to compare metadata.
func buildCacheFilesFromMeta(files []cacheFile) ([]cacheFile, error) {
	out := make([]cacheFile, 0, len(files))
	for _, file := range files {
		info, err := osStat(file.Path)
		if err != nil {
			return nil, err
		}
		out = append(out, cacheFile{
			Path:    filepath.Clean(file.Path),
			Size:    info.Size(),
			ModTime: info.ModTime().UnixNano(),
		})
	}
	return out, nil
}

// extraCacheFiles returns Go module/workspace files affecting builds.
func extraCacheFiles(wd string) []cacheFile {
	paths := extraCachePathsFunc(wd)
	if len(paths) == 0 {
		return nil
	}
	out := make([]cacheFile, 0, len(paths))
	seen := make(map[string]struct{})
	for _, path := range paths {
		path = filepath.Clean(path)
		if _, ok := seen[path]; ok {
			continue
		}
		info, err := osStat(path)
		if err != nil {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, cacheFile{
			Path:    path,
			Size:    info.Size(),
			ModTime: info.ModTime().UnixNano(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Path < out[j].Path
	})
	return out
}

// extraCachePaths finds go.mod/go.sum/go.work files for a working dir.
func extraCachePaths(wd string) []string {
	var paths []string
	dir := filepath.Clean(wd)
	seen := make(map[string]struct{})
	for {
		for _, name := range []string{"go.work", "go.work.sum", "go.mod", "go.sum"} {
			full := filepath.Join(dir, name)
			addExtraCachePath(&paths, seen, full)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return paths
}

// addExtraCachePath appends an existing file if it has not been seen.
func addExtraCachePath(paths *[]string, seen map[string]struct{}, full string) {
	if _, ok := seen[full]; ok {
		return
	}
	if _, err := osStat(full); err != nil {
		return
	}
	*paths = append(*paths, full)
	seen[full] = struct{}{}
}

// sortedStrings returns a sorted copy of the input slice.
func sortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

// envHash returns a stable hash of environment variables.
func envHash(env []string) string {
	if len(env) == 0 {
		return ""
	}
	sorted := sortedStrings(env)
	h := sha256.New()
	for _, v := range sorted {
		h.Write([]byte(v))
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
