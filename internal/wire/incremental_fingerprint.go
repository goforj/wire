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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

const incrementalFingerprintVersion = "wire-incremental-v1"

type packageFingerprint struct {
	Version      string
	WD           string
	Tags         string
	PkgPath      string
	Files        []cacheFile
	ShapeHash    string
	LocalImports []string
}

type fingerprintStats struct {
	localPackages int
	metaHits      int
	metaMisses    int
	unchanged     int
	changed       int
}

type incrementalFingerprintSnapshot struct {
	stats        fingerprintStats
	changed      []string
	fingerprints map[string]*packageFingerprint
}

func analyzeIncrementalFingerprints(ctx context.Context, wd string, env []string, tags string, pkgs []*packages.Package) *incrementalFingerprintSnapshot {
	if !IncrementalEnabled(ctx, env) {
		return nil
	}
	start := timeNow()
	snapshot := collectIncrementalFingerprints(wd, tags, pkgs)
	debugf(ctx, "incremental.fingerprint local_pkgs=%d meta_hits=%d meta_misses=%d unchanged=%d changed=%d total=%s",
		snapshot.stats.localPackages,
		snapshot.stats.metaHits,
		snapshot.stats.metaMisses,
		snapshot.stats.unchanged,
		snapshot.stats.changed,
		timeSince(start),
	)
	if len(snapshot.changed) > 0 {
		debugf(ctx, "incremental.fingerprint changed_pkgs=%s", strings.Join(snapshot.changed, ", "))
	}
	return snapshot
}

func collectIncrementalFingerprints(wd string, tags string, pkgs []*packages.Package) *incrementalFingerprintSnapshot {
	all := collectAllPackages(pkgs)
	moduleRoot := findModuleRoot(wd)
	snapshot := &incrementalFingerprintSnapshot{
		fingerprints: make(map[string]*packageFingerprint),
	}
	for _, pkg := range all {
		if classifyPackageLocation(moduleRoot, pkg) != "local" {
			continue
		}
		snapshot.stats.localPackages++
		files := packageFingerprintFiles(pkg)
		if len(files) == 0 {
			continue
		}
		sort.Strings(files)
		metaFiles, err := buildCacheFiles(files)
		if err != nil {
			snapshot.stats.metaMisses++
			continue
		}
		key := incrementalFingerprintKey(wd, tags, pkg.PkgPath)
		if prev, ok := readIncrementalFingerprint(key); ok && incrementalFingerprintMetaMatches(prev, wd, tags, pkg.PkgPath, metaFiles) {
			snapshot.stats.metaHits++
			snapshot.stats.unchanged++
			snapshot.fingerprints[pkg.PkgPath] = prev
			continue
		}
		snapshot.stats.metaMisses++
		fp, err := buildPackageFingerprint(wd, tags, pkg, metaFiles)
		if err != nil {
			continue
		}
		prev, hadPrev := readIncrementalFingerprint(key)
		writeIncrementalFingerprint(key, fp)
		snapshot.fingerprints[pkg.PkgPath] = fp
		if hadPrev && incrementalFingerprintEquivalent(prev, fp) {
			snapshot.stats.unchanged++
			continue
		}
		snapshot.stats.changed++
		snapshot.changed = append(snapshot.changed, pkg.PkgPath)
	}
	sort.Strings(snapshot.changed)
	return snapshot
}

func packageFingerprintFiles(pkg *packages.Package) []string {
	if pkg == nil {
		return nil
	}
	if len(pkg.CompiledGoFiles) > 0 {
		return append([]string(nil), pkg.CompiledGoFiles...)
	}
	return append([]string(nil), pkg.GoFiles...)
}

func incrementalFingerprintEquivalent(a, b *packageFingerprint) bool {
	if a == nil || b == nil {
		return false
	}
	if a.ShapeHash != b.ShapeHash || a.PkgPath != b.PkgPath || a.Tags != b.Tags || filepath.Clean(a.WD) != filepath.Clean(b.WD) {
		return false
	}
	if len(a.LocalImports) != len(b.LocalImports) {
		return false
	}
	for i := range a.LocalImports {
		if a.LocalImports[i] != b.LocalImports[i] {
			return false
		}
	}
	return true
}

func incrementalFingerprintMetaMatches(prev *packageFingerprint, wd string, tags string, pkgPath string, files []cacheFile) bool {
	if prev == nil || prev.Version != incrementalFingerprintVersion {
		return false
	}
	if filepath.Clean(prev.WD) != filepath.Clean(wd) || prev.Tags != tags || prev.PkgPath != pkgPath {
		return false
	}
	if len(prev.Files) != len(files) {
		return false
	}
	for i := range prev.Files {
		if prev.Files[i] != files[i] {
			return false
		}
	}
	return true
}

func buildPackageFingerprint(wd string, tags string, pkg *packages.Package, files []cacheFile) (*packageFingerprint, error) {
	shapeHash, err := packageShapeHash(packageFingerprintFiles(pkg))
	if err != nil {
		return nil, err
	}
	localImports := make([]string, 0, len(pkg.Imports))
	moduleRoot := findModuleRoot(wd)
	for _, imp := range pkg.Imports {
		if classifyPackageLocation(moduleRoot, imp) == "local" {
			localImports = append(localImports, imp.PkgPath)
		}
	}
	sort.Strings(localImports)
	return &packageFingerprint{
		Version:      incrementalFingerprintVersion,
		WD:           filepath.Clean(wd),
		Tags:         tags,
		PkgPath:      pkg.PkgPath,
		Files:        append([]cacheFile(nil), files...),
		ShapeHash:    shapeHash,
		LocalImports: localImports,
	}, nil
}

func packageShapeHash(files []string) (string, error) {
	fset := token.NewFileSet()
	var buf bytes.Buffer
	for _, name := range files {
		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			return "", err
		}
		stripFunctionBodies(file)
		if err := printer.Fprint(&buf, fset, file); err != nil {
			return "", err
		}
		buf.WriteByte(0)
	}
	sum := sha256.Sum256(buf.Bytes())
	return fmt.Sprintf("%x", sum[:]), nil
}

func stripFunctionBodies(file *ast.File) {
	if file == nil {
		return
	}
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			fn.Body = nil
			fn.Doc = nil
		}
	}
}

func incrementalFingerprintKey(wd string, tags string, pkgPath string) string {
	h := sha256.New()
	h.Write([]byte(incrementalFingerprintVersion))
	h.Write([]byte{0})
	h.Write([]byte(filepath.Clean(wd)))
	h.Write([]byte{0})
	h.Write([]byte(tags))
	h.Write([]byte{0})
	h.Write([]byte(pkgPath))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func incrementalFingerprintPath(key string) string {
	return filepath.Join(cacheDir(), key+".ifp")
}

func readIncrementalFingerprint(key string) (*packageFingerprint, bool) {
	data, err := osReadFile(incrementalFingerprintPath(key))
	if err != nil {
		return nil, false
	}
	fp, err := decodeIncrementalFingerprint(data)
	if err != nil {
		return nil, false
	}
	return fp, true
}

func writeIncrementalFingerprint(key string, fp *packageFingerprint) {
	data, err := encodeIncrementalFingerprint(fp)
	if err != nil {
		return
	}
	dir := cacheDir()
	if err := osMkdirAll(dir, 0755); err != nil {
		return
	}
	tmp, err := osCreateTemp(dir, key+".ifp-")
	if err != nil {
		return
	}
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		osRemove(tmp.Name())
		return
	}
	if err := osRename(tmp.Name(), incrementalFingerprintPath(key)); err != nil {
		osRemove(tmp.Name())
	}
}

func encodeIncrementalFingerprint(fp *packageFingerprint) ([]byte, error) {
	var buf bytes.Buffer
	writeString := func(s string) error {
		if err := binary.Write(&buf, binary.LittleEndian, uint32(len(s))); err != nil {
			return err
		}
		_, err := buf.WriteString(s)
		return err
	}
	writeCacheFiles := func(files []cacheFile) error {
		if err := binary.Write(&buf, binary.LittleEndian, uint32(len(files))); err != nil {
			return err
		}
		for _, f := range files {
			if err := writeString(f.Path); err != nil {
				return err
			}
			if err := binary.Write(&buf, binary.LittleEndian, f.Size); err != nil {
				return err
			}
			if err := binary.Write(&buf, binary.LittleEndian, f.ModTime); err != nil {
				return err
			}
		}
		return nil
	}
	writeStrings := func(items []string) error {
		if err := binary.Write(&buf, binary.LittleEndian, uint32(len(items))); err != nil {
			return err
		}
		for _, item := range items {
			if err := writeString(item); err != nil {
				return err
			}
		}
		return nil
	}
	if fp == nil {
		return nil, fmt.Errorf("nil fingerprint")
	}
	for _, s := range []string{fp.Version, fp.WD, fp.Tags, fp.PkgPath, fp.ShapeHash} {
		if err := writeString(s); err != nil {
			return nil, err
		}
	}
	if err := writeCacheFiles(fp.Files); err != nil {
		return nil, err
	}
	if err := writeStrings(fp.LocalImports); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeIncrementalFingerprint(data []byte) (*packageFingerprint, error) {
	r := bytes.NewReader(data)
	readString := func() (string, error) {
		var n uint32
		if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
			return "", err
		}
		buf := make([]byte, n)
		if _, err := r.Read(buf); err != nil {
			return "", err
		}
		return string(buf), nil
	}
	readCacheFiles := func() ([]cacheFile, error) {
		var n uint32
		if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
			return nil, err
		}
		out := make([]cacheFile, 0, n)
		for i := uint32(0); i < n; i++ {
			path, err := readString()
			if err != nil {
				return nil, err
			}
			var size int64
			if err := binary.Read(r, binary.LittleEndian, &size); err != nil {
				return nil, err
			}
			var modTime int64
			if err := binary.Read(r, binary.LittleEndian, &modTime); err != nil {
				return nil, err
			}
			out = append(out, cacheFile{Path: path, Size: size, ModTime: modTime})
		}
		return out, nil
	}
	readStrings := func() ([]string, error) {
		var n uint32
		if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
			return nil, err
		}
		out := make([]string, 0, n)
		for i := uint32(0); i < n; i++ {
			item, err := readString()
			if err != nil {
				return nil, err
			}
			out = append(out, item)
		}
		return out, nil
	}
	version, err := readString()
	if err != nil {
		return nil, err
	}
	wd, err := readString()
	if err != nil {
		return nil, err
	}
	tags, err := readString()
	if err != nil {
		return nil, err
	}
	pkgPath, err := readString()
	if err != nil {
		return nil, err
	}
	shapeHash, err := readString()
	if err != nil {
		return nil, err
	}
	files, err := readCacheFiles()
	if err != nil {
		return nil, err
	}
	localImports, err := readStrings()
	if err != nil {
		return nil, err
	}
	return &packageFingerprint{
		Version:      version,
		WD:           wd,
		Tags:         tags,
		PkgPath:      pkgPath,
		ShapeHash:    shapeHash,
		Files:        files,
		LocalImports: localImports,
	}, nil
}
