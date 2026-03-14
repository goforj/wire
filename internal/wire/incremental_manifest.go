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
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

const incrementalManifestVersion = "wire-incremental-manifest-v1"

type incrementalManifest struct {
	Version       string
	WD            string
	Tags          string
	Prefix        string
	HeaderHash    string
	EnvHash       string
	Patterns      []string
	LocalPackages []packageFingerprint
	ExternalPkgs  []externalPackageExport
	ExternalFiles []cacheFile
	ExtraFiles    []cacheFile
	Outputs       []incrementalOutput
}

type externalPackageExport struct {
	PkgPath    string
	ExportFile string
}

type incrementalOutput struct {
	PkgPath    string
	OutputPath string
	ContentKey string
}

type incrementalPreloadState struct {
	selectorKey  string
	manifest     *incrementalManifest
	valid        bool
	currentLocal []packageFingerprint
	reason       string
}

func readPreloadIncrementalManifestResults(ctx context.Context, wd string, env []string, patterns []string, opts *GenerateOptions) ([]GenerateResult, bool) {
	state, ok := prepareIncrementalPreloadState(ctx, wd, env, patterns, opts)
	return readPreloadIncrementalManifestResultsFromState(ctx, wd, env, patterns, opts, state, ok)
}

func readPreloadIncrementalManifestResultsFromState(ctx context.Context, wd string, env []string, patterns []string, opts *GenerateOptions, state *incrementalPreloadState, ok bool) ([]GenerateResult, bool) {
	if !ok {
		debugf(ctx, "incremental.preload_manifest miss reason=no_manifest")
		return nil, false
	}
	if state.valid {
		results, ok := incrementalManifestOutputs(state.manifest)
		if !ok {
			debugf(ctx, "incremental.preload_manifest miss reason=outputs")
			return nil, false
		}
		debugf(ctx, "incremental.preload_manifest hit outputs=%d", len(results))
		return results, true
	} else if archived := readStateIncrementalManifest(state.selectorKey, state.currentLocal); archived != nil {
		if ok, _, _ := incrementalManifestPreloadValid(ctx, archived, wd, env, patterns, opts); ok {
			results, ok := incrementalManifestOutputs(archived)
			if !ok {
				debugf(ctx, "incremental.preload_manifest miss reason=state_outputs")
				return nil, false
			}
			writeIncrementalManifestFile(state.selectorKey, archived)
			debugf(ctx, "incremental.preload_manifest state_hit outputs=%d", len(results))
			return results, true
		}
		debugf(ctx, "incremental.preload_manifest miss reason=%s", state.reason)
		return nil, false
	} else {
		debugf(ctx, "incremental.preload_manifest miss reason=%s", state.reason)
		return nil, false
	}
}

func prepareIncrementalPreloadState(ctx context.Context, wd string, env []string, patterns []string, opts *GenerateOptions) (*incrementalPreloadState, bool) {
	selectorKey := incrementalManifestSelectorKey(wd, env, patterns, opts)
	manifest, ok := readIncrementalManifest(selectorKey)
	if !ok {
		return nil, false
	}
	valid, currentLocal, reason := incrementalManifestPreloadValid(ctx, manifest, wd, env, patterns, opts)
	return &incrementalPreloadState{
		selectorKey:  selectorKey,
		manifest:     manifest,
		valid:        valid,
		currentLocal: currentLocal,
		reason:       reason,
	}, true
}

func readIncrementalManifestResults(ctx context.Context, wd string, env []string, patterns []string, opts *GenerateOptions, pkgs []*packages.Package, snapshot *incrementalFingerprintSnapshot) ([]GenerateResult, bool) {
	if snapshot == nil || snapshot.stats.changed != 0 {
		return nil, false
	}
	key := incrementalManifestSelectorKey(wd, env, patterns, opts)
	manifest, ok := readIncrementalManifest(key)
	if !ok || !incrementalManifestValid(manifest, wd, env, patterns, opts, pkgs) {
		return nil, false
	}
	results := make([]GenerateResult, 0, len(manifest.Outputs))
	for _, out := range manifest.Outputs {
		content, ok := readCache(out.ContentKey)
		if !ok {
			return nil, false
		}
		results = append(results, GenerateResult{
			PkgPath:    out.PkgPath,
			OutputPath: out.OutputPath,
			Content:    content,
		})
	}
	debugf(ctx, "incremental.manifest hit outputs=%d", len(results))
	return results, true
}

func writeIncrementalManifest(wd string, env []string, patterns []string, opts *GenerateOptions, pkgs []*packages.Package, snapshot *incrementalFingerprintSnapshot, generated []GenerateResult) {
	writeIncrementalManifestWithOptions(wd, env, patterns, opts, pkgs, snapshot, generated, true)
}

func writeIncrementalManifestWithOptions(wd string, env []string, patterns []string, opts *GenerateOptions, pkgs []*packages.Package, snapshot *incrementalFingerprintSnapshot, generated []GenerateResult, includeExternalFiles bool) {
	if snapshot == nil || len(generated) == 0 {
		return
	}
	externalPkgs := buildExternalPackageExports(wd, pkgs)
	var externalFiles []cacheFile
	if includeExternalFiles {
		var err error
		externalFiles, err = buildExternalPackageFiles(wd, pkgs)
		if err != nil {
			return
		}
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
		ExternalPkgs:  externalPkgs,
		ExternalFiles: externalFiles,
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
	stateKey := incrementalManifestStateKey(selectorKey, manifest.LocalPackages)
	writeIncrementalManifestFile(selectorKey, manifest)
	writeIncrementalManifestFile(stateKey, manifest)
}

func incrementalManifestSelectorKey(wd string, env []string, patterns []string, opts *GenerateOptions) string {
	h := sha256.New()
	h.Write([]byte(incrementalManifestVersion))
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

func snapshotPackageFingerprints(snapshot *incrementalFingerprintSnapshot) []packageFingerprint {
	if snapshot == nil || len(snapshot.fingerprints) == 0 {
		return nil
	}
	paths := make([]string, 0, len(snapshot.fingerprints))
	for path := range snapshot.fingerprints {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	out := make([]packageFingerprint, 0, len(paths))
	for _, path := range paths {
		if fp := snapshot.fingerprints[path]; fp != nil {
			out = append(out, *fp)
		}
	}
	return out
}

func buildExternalPackageFiles(wd string, pkgs []*packages.Package) ([]cacheFile, error) {
	moduleRoot := findModuleRoot(wd)
	seen := make(map[string]struct{})
	var files []string
	for _, pkg := range collectAllPackages(pkgs) {
		if classifyPackageLocation(moduleRoot, pkg) == "local" {
			continue
		}
		names := pkg.CompiledGoFiles
		if len(names) == 0 {
			names = pkg.GoFiles
		}
		for _, name := range names {
			clean := filepath.Clean(name)
			if _, ok := seen[clean]; ok {
				continue
			}
			seen[clean] = struct{}{}
			files = append(files, clean)
		}
	}
	sort.Strings(files)
	return buildCacheFiles(files)
}

func buildExternalPackageExports(wd string, pkgs []*packages.Package) []externalPackageExport {
	out := make([]externalPackageExport, 0)
	for _, pkg := range collectAllPackages(pkgs) {
		if pkg == nil || pkg.PkgPath == "" || pkg.ExportFile == "" {
			continue
		}
		out = append(out, externalPackageExport{
			PkgPath:    pkg.PkgPath,
			ExportFile: pkg.ExportFile,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PkgPath < out[j].PkgPath })
	return out
}

func incrementalManifestValid(manifest *incrementalManifest, wd string, env []string, patterns []string, opts *GenerateOptions, pkgs []*packages.Package) bool {
	if manifest == nil || manifest.Version != incrementalManifestVersion {
		return false
	}
	if filepath.Clean(manifest.WD) != filepath.Clean(wd) || manifest.Tags != opts.Tags || manifest.Prefix != opts.PrefixOutputFile {
		return false
	}
	if manifest.HeaderHash != headerHash(opts.Header) || manifest.EnvHash != envHash(env) {
		return false
	}
	if len(manifest.Patterns) != len(patterns) {
		return false
	}
	for i, p := range sortedStrings(patterns) {
		if manifest.Patterns[i] != p {
			return false
		}
	}
	currentExternal, err := buildExternalPackageFiles(wd, pkgs)
	if err != nil || len(currentExternal) != len(manifest.ExternalFiles) {
		return false
	}
	for i := range currentExternal {
		if currentExternal[i] != manifest.ExternalFiles[i] {
			return false
		}
	}
	if len(manifest.ExtraFiles) > 0 {
		current, err := buildCacheFilesFromMeta(manifest.ExtraFiles)
		if err != nil || len(current) != len(manifest.ExtraFiles) {
			return false
		}
		for i := range current {
			if current[i] != manifest.ExtraFiles[i] {
				return false
			}
		}
	}
	return len(manifest.Outputs) > 0
}

func incrementalManifestPreloadValid(ctx context.Context, manifest *incrementalManifest, wd string, env []string, patterns []string, opts *GenerateOptions) (bool, []packageFingerprint, string) {
	if manifest == nil || manifest.Version != incrementalManifestVersion {
		return false, nil, "version"
	}
	if filepath.Clean(manifest.WD) != filepath.Clean(wd) || manifest.Tags != opts.Tags || manifest.Prefix != opts.PrefixOutputFile {
		return false, nil, "config"
	}
	if manifest.HeaderHash != headerHash(opts.Header) || manifest.EnvHash != envHash(env) {
		return false, nil, "env"
	}
	if len(manifest.Patterns) != len(patterns) {
		return false, nil, "patterns.length"
	}
	for i, p := range sortedStrings(patterns) {
		if manifest.Patterns[i] != p {
			return false, nil, "patterns.value"
		}
	}
	if len(manifest.ExtraFiles) > 0 {
		current, err := buildCacheFilesFromMeta(manifest.ExtraFiles)
		if err != nil || len(current) != len(manifest.ExtraFiles) {
			return false, nil, "extra_files"
		}
		for i := range current {
			if current[i] != manifest.ExtraFiles[i] {
				return false, nil, "extra_files.diff"
			}
		}
	}
	currentLocal, ok, reason := incrementalManifestCurrentLocalPackages(ctx, manifest.LocalPackages)
	if !ok {
		return false, currentLocal, "local_packages." + reason
	}
	if len(manifest.ExternalFiles) > 0 {
		current, err := buildCacheFilesFromMeta(manifest.ExternalFiles)
		if err != nil || len(current) != len(manifest.ExternalFiles) {
			return false, currentLocal, "external_files"
		}
		for i := range current {
			if current[i] != manifest.ExternalFiles[i] {
				return false, currentLocal, "external_files.diff"
			}
		}
	}
	if len(manifest.Outputs) == 0 {
		return false, currentLocal, "outputs"
	}
	return true, currentLocal, ""
}

func incrementalManifestCurrentLocalPackages(ctx context.Context, local []packageFingerprint) ([]packageFingerprint, bool, string) {
	currentState := make([]packageFingerprint, 0, len(local))
	var firstReason string
	for _, fp := range local {
		if len(fp.Files) == 0 {
			if firstReason == "" {
				firstReason = fp.PkgPath + ".files"
			}
			continue
		}
		storedFiles := filesFromMeta(fp.Files)
		if len(storedFiles) == 0 {
			if firstReason == "" {
				firstReason = fp.PkgPath + ".stored_files"
			}
			continue
		}
		currentMeta, err := buildCacheFiles(storedFiles)
		if err != nil {
			debugf(ctx, "incremental.preload_manifest local_pkg=%s meta_error=%v", fp.PkgPath, err)
			if firstReason == "" {
				firstReason = fp.PkgPath + ".meta_error"
			}
			continue
		}
		currentFP := fp
		currentFP.Files = append([]cacheFile(nil), currentMeta...)
		sameMeta := len(currentMeta) == len(fp.Files)
		if sameMeta {
			for i := range currentMeta {
				if currentMeta[i] != fp.Files[i] {
					sameMeta = false
					break
				}
			}
		}
		if !sameMeta {
			shapeHash, err := packageShapeHash(storedFiles)
			if err != nil {
				debugf(ctx, "incremental.preload_manifest local_pkg=%s shape_error=%v", fp.PkgPath, err)
				if firstReason == "" {
					firstReason = fp.PkgPath + ".shape_error"
				}
				continue
			}
			currentFP.ShapeHash = shapeHash
			if shapeHash != fp.ShapeHash {
				debugf(ctx, "incremental.preload_manifest local_pkg=%s stored_shape=%s current_shape=%s files=%s", fp.PkgPath, fp.ShapeHash, shapeHash, strings.Join(storedFiles, ","))
				if firstReason == "" {
					firstReason = fp.PkgPath + ".shape_mismatch"
				}
			}
		}
		if changed, err := packageDirectoryIntroducedRelevantFiles(fp.Files); err != nil {
			debugf(ctx, "incremental.preload_manifest local_pkg=%s dir_scan_error=%v", fp.PkgPath, err)
			if firstReason == "" {
				firstReason = fp.PkgPath + ".dir_scan_error"
			}
			continue
		} else if changed {
			debugf(ctx, "incremental.preload_manifest local_pkg=%s introduced_relevant_files=true", fp.PkgPath)
			if firstReason == "" {
				firstReason = fp.PkgPath + ".introduced_relevant_files"
			}
		}
		currentState = append(currentState, currentFP)
	}
	if firstReason != "" {
		return currentState, false, firstReason
	}
	return currentState, true, ""
}

func incrementalManifestOutputs(manifest *incrementalManifest) ([]GenerateResult, bool) {
	results := make([]GenerateResult, 0, len(manifest.Outputs))
	for _, out := range manifest.Outputs {
		content, ok := readCache(out.ContentKey)
		if !ok {
			return nil, false
		}
		results = append(results, GenerateResult{
			PkgPath:    out.PkgPath,
			OutputPath: out.OutputPath,
			Content:    content,
		})
	}
	return results, true
}

func readStateIncrementalManifest(selectorKey string, local []packageFingerprint) *incrementalManifest {
	if len(local) == 0 {
		return nil
	}
	stateKey := incrementalManifestStateKey(selectorKey, local)
	manifest, ok := readIncrementalManifest(stateKey)
	if !ok {
		return nil
	}
	return manifest
}

func incrementalManifestStateKey(selectorKey string, local []packageFingerprint) string {
	h := sha256.New()
	h.Write([]byte(selectorKey))
	h.Write([]byte{0})
	for _, fp := range snapshotPackageFingerprints(&incrementalFingerprintSnapshot{fingerprints: fingerprintsFromSlice(local)}) {
		h.Write([]byte(fp.PkgPath))
		h.Write([]byte{0})
		h.Write([]byte(fp.ShapeHash))
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func fingerprintsFromSlice(local []packageFingerprint) map[string]*packageFingerprint {
	if len(local) == 0 {
		return nil
	}
	out := make(map[string]*packageFingerprint, len(local))
	for i := range local {
		fp := local[i]
		out[fp.PkgPath] = &fp
	}
	return out
}

func filesFromMeta(files []cacheFile) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, filepath.Clean(f.Path))
	}
	sort.Strings(out)
	return out
}

func packageDirectoryIntroducedRelevantFiles(files []cacheFile) (bool, error) {
	dirs := make(map[string]struct{})
	old := make(map[string]struct{}, len(files))
	for _, f := range files {
		path := filepath.Clean(f.Path)
		dirs[filepath.Dir(path)] = struct{}{}
		old[path] = struct{}{}
	}
	for dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".go") {
				continue
			}
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			if strings.HasSuffix(name, "wire_gen.go") {
				continue
			}
			path := filepath.Clean(filepath.Join(dir, name))
			if _, ok := old[path]; !ok {
				return true, nil
			}
		}
	}
	return false, nil
}

func incrementalManifestPath(key string) string {
	return filepath.Join(cacheDir(), key+".iman")
}

func readIncrementalManifest(key string) (*incrementalManifest, bool) {
	data, err := osReadFile(incrementalManifestPath(key))
	if err != nil {
		return nil, false
	}
	manifest, err := decodeIncrementalManifest(data)
	if err != nil {
		return nil, false
	}
	return manifest, true
}

func writeIncrementalManifestFile(key string, manifest *incrementalManifest) {
	data, err := encodeIncrementalManifest(manifest)
	if err != nil {
		return
	}
	dir := cacheDir()
	if err := osMkdirAll(dir, 0755); err != nil {
		return
	}
	tmp, err := osCreateTemp(dir, key+".iman-")
	if err != nil {
		return
	}
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		osRemove(tmp.Name())
		return
	}
	if err := osRename(tmp.Name(), incrementalManifestPath(key)); err != nil {
		osRemove(tmp.Name())
	}
}

func encodeIncrementalManifest(manifest *incrementalManifest) ([]byte, error) {
	var buf bytes.Buffer
	if manifest == nil {
		return nil, fmt.Errorf("nil incremental manifest")
	}
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
	writeExternalPkgs := func(pkgs []externalPackageExport) error {
		if err := binary.Write(&buf, binary.LittleEndian, uint32(len(pkgs))); err != nil {
			return err
		}
		for _, pkg := range pkgs {
			if err := writeString(pkg.PkgPath); err != nil {
				return err
			}
			if err := writeString(pkg.ExportFile); err != nil {
				return err
			}
		}
		return nil
	}
	writeFingerprints := func(fps []packageFingerprint) error {
		if err := binary.Write(&buf, binary.LittleEndian, uint32(len(fps))); err != nil {
			return err
		}
		for _, fp := range fps {
			for _, s := range []string{fp.Version, fp.WD, fp.Tags, fp.PkgPath, fp.ShapeHash} {
				if err := writeString(s); err != nil {
					return err
				}
			}
			if err := writeCacheFiles(fp.Files); err != nil {
				return err
			}
			if err := binary.Write(&buf, binary.LittleEndian, uint32(len(fp.LocalImports))); err != nil {
				return err
			}
			for _, imp := range fp.LocalImports {
				if err := writeString(imp); err != nil {
					return err
				}
			}
		}
		return nil
	}
	writeOutputs := func(outputs []incrementalOutput) error {
		if err := binary.Write(&buf, binary.LittleEndian, uint32(len(outputs))); err != nil {
			return err
		}
		for _, out := range outputs {
			for _, s := range []string{out.PkgPath, out.OutputPath, out.ContentKey} {
				if err := writeString(s); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, s := range []string{manifest.Version, manifest.WD, manifest.Tags, manifest.Prefix, manifest.HeaderHash, manifest.EnvHash} {
		if err := writeString(s); err != nil {
			return nil, err
		}
	}
	if err := binary.Write(&buf, binary.LittleEndian, uint32(len(manifest.Patterns))); err != nil {
		return nil, err
	}
	for _, p := range manifest.Patterns {
		if err := writeString(p); err != nil {
			return nil, err
		}
	}
	if err := writeFingerprints(manifest.LocalPackages); err != nil {
		return nil, err
	}
	if err := writeExternalPkgs(manifest.ExternalPkgs); err != nil {
		return nil, err
	}
	if err := writeCacheFiles(manifest.ExternalFiles); err != nil {
		return nil, err
	}
	if err := writeCacheFiles(manifest.ExtraFiles); err != nil {
		return nil, err
	}
	if err := writeOutputs(manifest.Outputs); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeIncrementalManifest(data []byte) (*incrementalManifest, error) {
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
	readExternalPkgs := func() ([]externalPackageExport, error) {
		var n uint32
		if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
			return nil, err
		}
		out := make([]externalPackageExport, 0, n)
		for i := uint32(0); i < n; i++ {
			pkgPath, err := readString()
			if err != nil {
				return nil, err
			}
			exportFile, err := readString()
			if err != nil {
				return nil, err
			}
			out = append(out, externalPackageExport{PkgPath: pkgPath, ExportFile: exportFile})
		}
		return out, nil
	}
	readFingerprints := func() ([]packageFingerprint, error) {
		var n uint32
		if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
			return nil, err
		}
		out := make([]packageFingerprint, 0, n)
		for i := uint32(0); i < n; i++ {
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
			var importCount uint32
			if err := binary.Read(r, binary.LittleEndian, &importCount); err != nil {
				return nil, err
			}
			localImports := make([]string, 0, importCount)
			for j := uint32(0); j < importCount; j++ {
				imp, err := readString()
				if err != nil {
					return nil, err
				}
				localImports = append(localImports, imp)
			}
			out = append(out, packageFingerprint{
				Version:      version,
				WD:           wd,
				Tags:         tags,
				PkgPath:      pkgPath,
				ShapeHash:    shapeHash,
				Files:        files,
				LocalImports: localImports,
			})
		}
		return out, nil
	}
	readOutputs := func() ([]incrementalOutput, error) {
		var n uint32
		if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
			return nil, err
		}
		out := make([]incrementalOutput, 0, n)
		for i := uint32(0); i < n; i++ {
			pkgPath, err := readString()
			if err != nil {
				return nil, err
			}
			outputPath, err := readString()
			if err != nil {
				return nil, err
			}
			contentKey, err := readString()
			if err != nil {
				return nil, err
			}
			out = append(out, incrementalOutput{PkgPath: pkgPath, OutputPath: outputPath, ContentKey: contentKey})
		}
		return out, nil
	}
	fields := make([]string, 6)
	for i := range fields {
		s, err := readString()
		if err != nil {
			return nil, err
		}
		fields[i] = s
	}
	var patternCount uint32
	if err := binary.Read(r, binary.LittleEndian, &patternCount); err != nil {
		return nil, err
	}
	patterns := make([]string, 0, patternCount)
	for i := uint32(0); i < patternCount; i++ {
		p, err := readString()
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, p)
	}
	localPackages, err := readFingerprints()
	if err != nil {
		return nil, err
	}
	externalPkgs, err := readExternalPkgs()
	if err != nil {
		return nil, err
	}
	externalFiles, err := readCacheFiles()
	if err != nil {
		return nil, err
	}
	extraFiles, err := readCacheFiles()
	if err != nil {
		return nil, err
	}
	outputs, err := readOutputs()
	if err != nil {
		return nil, err
	}
	return &incrementalManifest{
		Version:       fields[0],
		WD:            fields[1],
		Tags:          fields[2],
		Prefix:        fields[3],
		HeaderHash:    fields[4],
		EnvHash:       fields[5],
		Patterns:      patterns,
		LocalPackages: localPackages,
		ExternalPkgs:  externalPkgs,
		ExternalFiles: externalFiles,
		ExtraFiles:    extraFiles,
		Outputs:       outputs,
	}, nil
}

func incrementalContentKey(content []byte) string {
	sum := sha256.Sum256(content)
	return fmt.Sprintf("%x", sum[:])
}
