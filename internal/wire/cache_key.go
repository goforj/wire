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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"golang.org/x/tools/go/packages"
)

// cacheVersion is the schema/version identifier for cache entries.
const cacheVersion = "wire-cache-v3"

// cacheFile captures file metadata used to validate cached content.
type cacheFile struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mod_time"`
}

// cacheMeta tracks inputs and outputs for a single package cache entry.
type cacheMeta struct {
	Version     string      `json:"version"`
	PkgPath     string      `json:"pkg_path"`
	Tags        string      `json:"tags"`
	Prefix      string      `json:"prefix"`
	HeaderHash  string      `json:"header_hash"`
	Files       []cacheFile `json:"files"`
	ContentHash string      `json:"content_hash"`
	RootHash    string      `json:"root_hash"`
}

// cacheKeyForPackage returns the content hash for a package, if cacheable.
func cacheKeyForPackage(pkg *packages.Package, opts *GenerateOptions) (string, error) {
	files := packageFiles(pkg)
	if len(files) == 0 {
		return "", nil
	}
	sort.Strings(files)
	metaKey := cacheMetaKey(pkg, opts)
	if meta, ok := readCacheMeta(metaKey); ok {
		if cacheMetaMatches(meta, pkg, opts, files) {
			return meta.ContentHash, nil
		}
	}
	contentHash, err := contentHashForFiles(pkg, opts, files)
	if err != nil {
		return "", err
	}
	rootFiles := rootPackageFiles(pkg)
	sort.Strings(rootFiles)
	rootHash, err := hashFiles(rootFiles)
	if err != nil {
		return "", err
	}
	metaFiles, err := buildCacheFiles(files)
	if err != nil {
		return "", err
	}
	meta := &cacheMeta{
		Version:     cacheVersion,
		PkgPath:     pkg.PkgPath,
		Tags:        opts.Tags,
		Prefix:      opts.PrefixOutputFile,
		HeaderHash:  headerHash(opts.Header),
		Files:       metaFiles,
		ContentHash: contentHash,
		RootHash:    rootHash,
	}
	writeCacheMeta(metaKey, meta)
	return contentHash, nil
}

// packageFiles returns the transitive Go files for a package graph.
func packageFiles(root *packages.Package) []string {
	seen := make(map[string]struct{})
	var files []string
	stack := []*packages.Package{root}
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

// cacheMetaKey builds the key for a package's cache metadata entry.
func cacheMetaKey(pkg *packages.Package, opts *GenerateOptions) string {
	h := sha256.New()
	h.Write([]byte(cacheVersion))
	h.Write([]byte{0})
	h.Write([]byte(pkg.PkgPath))
	h.Write([]byte{0})
	h.Write([]byte(opts.Tags))
	h.Write([]byte{0})
	h.Write([]byte(opts.PrefixOutputFile))
	h.Write([]byte{0})
	h.Write([]byte(headerHash(opts.Header)))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// cacheMetaPath returns the on-disk path for a cache metadata key.
func cacheMetaPath(key string) string {
	return filepath.Join(cacheDir(), key+".json")
}

// readCacheMeta loads a cached metadata entry if it exists.
func readCacheMeta(key string) (*cacheMeta, bool) {
	data, err := os.ReadFile(cacheMetaPath(key))
	if err != nil {
		return nil, false
	}
	var meta cacheMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, false
	}
	return &meta, true
}

// writeCacheMeta persists cache metadata to disk.
func writeCacheMeta(key string, meta *cacheMeta) {
	dir := cacheDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, key+".meta-")
	if err != nil {
		return
	}
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(tmp.Name())
		return
	}
	path := cacheMetaPath(key)
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
	}
}

// cacheMetaMatches reports whether metadata matches the current package inputs.
func cacheMetaMatches(meta *cacheMeta, pkg *packages.Package, opts *GenerateOptions, files []string) bool {
	if meta.Version != cacheVersion {
		return false
	}
	if meta.PkgPath != pkg.PkgPath || meta.Tags != opts.Tags || meta.Prefix != opts.PrefixOutputFile {
		return false
	}
	if meta.HeaderHash != headerHash(opts.Header) {
		return false
	}
	if len(meta.Files) != len(files) {
		return false
	}
	current, err := buildCacheFiles(files)
	if err != nil {
		return false
	}
	for i := range meta.Files {
		if meta.Files[i] != current[i] {
			return false
		}
	}
	rootFiles := rootPackageFiles(pkg)
	if len(rootFiles) == 0 || meta.RootHash == "" {
		return false
	}
	sort.Strings(rootFiles)
	rootHash, err := hashFiles(rootFiles)
	if err != nil || rootHash != meta.RootHash {
		return false
	}
	return meta.ContentHash != ""
}

// buildCacheFiles converts file paths into cache metadata entries.
func buildCacheFiles(files []string) ([]cacheFile, error) {
	out := make([]cacheFile, 0, len(files))
	for _, name := range files {
		info, err := os.Stat(name)
		if err != nil {
			return nil, err
		}
		out = append(out, cacheFile{
			Path:    filepath.Clean(name),
			Size:    info.Size(),
			ModTime: info.ModTime().UnixNano(),
		})
	}
	return out, nil
}

// headerHash returns a stable hash of the generated header content.
func headerHash(header []byte) string {
	if len(header) == 0 {
		return ""
	}
	sum := sha256.Sum256(header)
	return fmt.Sprintf("%x", sum[:])
}

// contentHashForFiles hashes the current package inputs using file paths.
func contentHashForFiles(pkg *packages.Package, opts *GenerateOptions, files []string) (string, error) {
	return contentHashForPaths(pkg.PkgPath, opts, files)
}

// contentHashForPaths hashes the provided file contents and options.
func contentHashForPaths(pkgPath string, opts *GenerateOptions, files []string) (string, error) {
	h := sha256.New()
	h.Write([]byte(cacheVersion))
	h.Write([]byte{0})
	h.Write([]byte(pkgPath))
	h.Write([]byte{0})
	h.Write([]byte(opts.Tags))
	h.Write([]byte{0})
	h.Write([]byte(opts.PrefixOutputFile))
	h.Write([]byte{0})
	h.Write([]byte(headerHash(opts.Header)))
	h.Write([]byte{0})
	for _, name := range files {
		h.Write([]byte(name))
		h.Write([]byte{0})
		data, err := os.ReadFile(name)
		if err != nil {
			return "", err
		}
		h.Write(data)
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// rootPackageFiles returns the direct Go files for the root package.
func rootPackageFiles(pkg *packages.Package) []string {
	if pkg == nil {
		return nil
	}
	if len(pkg.CompiledGoFiles) > 0 {
		return append([]string(nil), pkg.CompiledGoFiles...)
	}
	if len(pkg.GoFiles) > 0 {
		return append([]string(nil), pkg.GoFiles...)
	}
	return nil
}

// hashFiles returns a combined content hash for the provided paths.
func hashFiles(files []string) (string, error) {
	if len(files) == 0 {
		return "", nil
	}
	h := sha256.New()
	for _, name := range files {
		h.Write([]byte(name))
		h.Write([]byte{0})
		data, err := os.ReadFile(name)
		if err != nil {
			return "", err
		}
		h.Write(data)
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
