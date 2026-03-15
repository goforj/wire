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
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

const incrementalManifestVersion = "wire-incremental-manifest-v3"

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
	touched      []string
	reason       string
}

type incrementalPreloadValidation struct {
	valid        bool
	currentLocal []packageFingerprint
	touched      []string
	reason       string
}

const touchedValidationVersion = "wire-touched-validation-v1"

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
		validateStart := timeNow()
		if len(state.touched) > 0 {
			debugf(ctx, "incremental.preload_manifest touched=%s", strings.Join(state.touched, ","))
		}
		if err := validateIncrementalPreloadTouchedPackages(ctx, wd, env, opts, state.currentLocal, state.touched); err != nil {
			logTiming(ctx, "incremental.preload_manifest.validate_touched", validateStart)
			if shouldBypassIncrementalManifestAfterFastPathError(err) {
				invalidateIncrementalPreloadState(state)
			}
			debugf(ctx, "incremental.preload_manifest miss reason=touched_validation")
			return nil, false
		}
		logTiming(ctx, "incremental.preload_manifest.validate_touched", validateStart)
		outputsStart := timeNow()
		results, ok := incrementalManifestOutputs(state.manifest)
		logTiming(ctx, "incremental.preload_manifest.outputs", outputsStart)
		if !ok {
			debugf(ctx, "incremental.preload_manifest miss reason=outputs")
			return nil, false
		}
		if manifestNeedsLocalRefresh(state.manifest.LocalPackages, state.currentLocal) {
			refreshed := *state.manifest
			refreshed.LocalPackages = append([]packageFingerprint(nil), state.currentLocal...)
			writeIncrementalManifestFile(state.selectorKey, &refreshed)
			writeIncrementalManifestFile(incrementalManifestStateKey(state.selectorKey, refreshed.LocalPackages), &refreshed)
		}
		debugf(ctx, "incremental.preload_manifest hit outputs=%d", len(results))
		return results, true
	} else if archived := readStateIncrementalManifest(state.selectorKey, state.currentLocal); archived != nil {
		if validation := incrementalManifestPreloadValid(ctx, archived, wd, env, patterns, opts); validation.valid {
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
	validation := incrementalManifestPreloadValid(ctx, manifest, wd, env, patterns, opts)
	return &incrementalPreloadState{
		selectorKey:  selectorKey,
		manifest:     manifest,
		valid:        validation.valid,
		currentLocal: validation.currentLocal,
		touched:      validation.touched,
		reason:       validation.reason,
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
	scope := runCacheScope(wd, patterns)
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
		WD:            scope,
		Tags:          opts.Tags,
		Prefix:        opts.PrefixOutputFile,
		HeaderHash:    headerHash(opts.Header),
		EnvHash:       envHash(env),
		Patterns:      normalizePatternsForScope(wd, packageCacheScope(wd), patterns),
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
	h.Write([]byte(runCacheScope(wd, patterns)))
	h.Write([]byte{0})
	h.Write([]byte(envHash(env)))
	h.Write([]byte{0})
	h.Write([]byte(opts.Tags))
	h.Write([]byte{0})
	h.Write([]byte(opts.PrefixOutputFile))
	h.Write([]byte{0})
	h.Write([]byte(headerHash(opts.Header)))
	h.Write([]byte{0})
	for _, p := range normalizePatternsForScope(wd, packageCacheScope(wd), patterns) {
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
	if manifest.WD != runCacheScope(wd, patterns) || manifest.Tags != opts.Tags || manifest.Prefix != opts.PrefixOutputFile {
		return false
	}
	if manifest.HeaderHash != headerHash(opts.Header) || manifest.EnvHash != envHash(env) {
		return false
	}
	normalizedPatterns := normalizePatternsForScope(wd, packageCacheScope(wd), patterns)
	if len(manifest.Patterns) != len(normalizedPatterns) {
		return false
	}
	for i, p := range normalizedPatterns {
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

func incrementalManifestPreloadValid(ctx context.Context, manifest *incrementalManifest, wd string, env []string, patterns []string, opts *GenerateOptions) incrementalPreloadValidation {
	if manifest == nil || manifest.Version != incrementalManifestVersion {
		return incrementalPreloadValidation{reason: "version"}
	}
	if manifest.WD != runCacheScope(wd, patterns) || manifest.Tags != opts.Tags || manifest.Prefix != opts.PrefixOutputFile {
		return incrementalPreloadValidation{reason: "config"}
	}
	if manifest.HeaderHash != headerHash(opts.Header) || manifest.EnvHash != envHash(env) {
		return incrementalPreloadValidation{reason: "env"}
	}
	normalizedPatterns := normalizePatternsForScope(wd, packageCacheScope(wd), patterns)
	if len(manifest.Patterns) != len(normalizedPatterns) {
		return incrementalPreloadValidation{reason: "patterns.length"}
	}
	for i, p := range normalizedPatterns {
		if manifest.Patterns[i] != p {
			return incrementalPreloadValidation{reason: "patterns.value"}
		}
	}
	if len(manifest.ExtraFiles) > 0 {
		extraStart := timeNow()
		current, err := buildCacheFilesFromMeta(manifest.ExtraFiles)
		logTiming(ctx, "incremental.preload_manifest.validate_extra_files", extraStart)
		if err != nil || len(current) != len(manifest.ExtraFiles) {
			return incrementalPreloadValidation{reason: "extra_files"}
		}
		for i := range current {
			if current[i] != manifest.ExtraFiles[i] {
				return incrementalPreloadValidation{reason: "extra_files.diff"}
			}
		}
	}
	localStart := timeNow()
	packagesState := incrementalManifestCurrentLocalPackages(ctx, manifest.LocalPackages)
	logTiming(ctx, "incremental.preload_manifest.validate_local_packages", localStart)
	if !packagesState.valid {
		return incrementalPreloadValidation{
			currentLocal: packagesState.currentLocal,
			touched:      packagesState.touched,
			reason:       "local_packages." + packagesState.reason,
		}
	}
	if len(manifest.ExternalFiles) > 0 {
		externalStart := timeNow()
		current, err := buildCacheFilesFromMeta(manifest.ExternalFiles)
		logTiming(ctx, "incremental.preload_manifest.validate_external_files", externalStart)
		if err != nil || len(current) != len(manifest.ExternalFiles) {
			return incrementalPreloadValidation{
				currentLocal: packagesState.currentLocal,
				touched:      packagesState.touched,
				reason:       "external_files",
			}
		}
		for i := range current {
			if current[i] != manifest.ExternalFiles[i] {
				return incrementalPreloadValidation{
					currentLocal: packagesState.currentLocal,
					touched:      packagesState.touched,
					reason:       "external_files.diff",
				}
			}
		}
	}
	if len(manifest.Outputs) == 0 {
		return incrementalPreloadValidation{
			currentLocal: packagesState.currentLocal,
			touched:      packagesState.touched,
			reason:       "outputs",
		}
	}
	return incrementalPreloadValidation{
		valid:        true,
		currentLocal: packagesState.currentLocal,
		touched:      packagesState.touched,
	}
}

type incrementalLocalPackagesState struct {
	valid        bool
	currentLocal []packageFingerprint
	touched      []string
	reason       string
}

func incrementalManifestCurrentLocalPackages(ctx context.Context, local []packageFingerprint) incrementalLocalPackagesState {
	currentState := make([]packageFingerprint, 0, len(local))
	touched := make([]string, 0, len(local))
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
			if diffs := describeCacheFileDiffs(fp.Files, currentMeta); len(diffs) > 0 {
				debugf(ctx, "incremental.preload_manifest local_pkg=%s meta_diff=%s", fp.PkgPath, strings.Join(diffs, "; "))
			}
			contentHash, err := hashFiles(storedFiles)
			if err != nil {
				debugf(ctx, "incremental.preload_manifest local_pkg=%s content_error=%v", fp.PkgPath, err)
				if firstReason == "" {
					firstReason = fp.PkgPath + ".content_error"
				}
				continue
			}
			currentFP.ContentHash = contentHash
			if contentHash != fp.ContentHash {
				debugf(ctx, "incremental.preload_manifest local_pkg=%s stored_content=%s current_content=%s hash_files=%s", fp.PkgPath, fp.ContentHash, contentHash, strings.Join(storedFiles, ","))
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
				} else {
					debugf(ctx, "incremental.preload_manifest local_pkg=%s content_changed_shape_unchanged", fp.PkgPath)
					touched = append(touched, fp.PkgPath)
				}
			}
		}
		currentDirs, dirsChanged, err := packageDirectoryMetaChanged(fp, storedFiles)
		if err != nil {
			debugf(ctx, "incremental.preload_manifest local_pkg=%s dir_meta_error=%v", fp.PkgPath, err)
			if firstReason == "" {
				firstReason = fp.PkgPath + ".dir_meta_error"
			}
			continue
		}
		currentFP.Dirs = currentDirs
		if dirsChanged {
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
		}
		currentState = append(currentState, currentFP)
	}
	if firstReason != "" {
		return incrementalLocalPackagesState{
			currentLocal: currentState,
			touched:      touched,
			reason:       firstReason,
		}
	}
	sort.Strings(touched)
	return incrementalLocalPackagesState{
		valid:        true,
		currentLocal: currentState,
		touched:      touched,
	}
}

func validateIncrementalPreloadTouchedPackages(ctx context.Context, wd string, env []string, opts *GenerateOptions, local []packageFingerprint, touched []string) error {
	if len(touched) == 0 {
		return nil
	}
	cacheKey := touchedValidationKey(wd, env, opts, local, touched)
	if cacheKey != "" {
		cacheHitStart := timeNow()
		if _, ok := readCache(cacheKey); ok {
			logTiming(ctx, "incremental.preload_manifest.validate_touched_cache_hit", cacheHitStart)
			return nil
		}
	}
	cfg := &packages.Config{
		Context:    ctx,
		Mode:       packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps | packages.NeedExportsFile | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesSizes,
		Dir:        wd,
		Env:        env,
		BuildFlags: []string{"-tags=wireinject"},
		Fset:       token.NewFileSet(),
	}
	if len(opts.Tags) > 0 {
		cfg.BuildFlags[0] += " " + opts.Tags
	}
	loadStart := timeNow()
	pkgs, err := packages.Load(cfg, touched...)
	logTiming(ctx, "incremental.preload_manifest.validate_touched_load", loadStart)
	if err != nil {
		return err
	}
	errorsStart := timeNow()
	byPath := make(map[string]*packages.Package, len(pkgs))
	for _, pkg := range pkgs {
		if pkg != nil {
			byPath[pkg.PkgPath] = pkg
		}
	}
	for _, path := range touched {
		if pkg := byPath[path]; pkg != nil && len(pkg.Errors) > 0 {
			logTiming(ctx, "incremental.preload_manifest.validate_touched_errors", errorsStart)
			return formatLocalTypeCheckError(wd, pkg.PkgPath, pkg.Errors)
		}
	}
	logTiming(ctx, "incremental.preload_manifest.validate_touched_errors", errorsStart)
	if cacheKey != "" {
		cacheWriteStart := timeNow()
		writeCache(cacheKey, []byte("ok"))
		logTiming(ctx, "incremental.preload_manifest.validate_touched_cache_write", cacheWriteStart)
	}
	return nil
}

func touchedValidationKey(wd string, env []string, opts *GenerateOptions, local []packageFingerprint, touched []string) string {
	if len(touched) == 0 {
		return ""
	}
	byPath := fingerprintsFromSlice(local)
	h := sha256.New()
	h.Write([]byte(touchedValidationVersion))
	h.Write([]byte{0})
	h.Write([]byte(packageCacheScope(wd)))
	h.Write([]byte{0})
	h.Write([]byte(envHash(env)))
	h.Write([]byte{0})
	if opts != nil {
		h.Write([]byte(opts.Tags))
	}
	h.Write([]byte{0})
	for _, pkgPath := range touched {
		fp := byPath[pkgPath]
		if fp == nil || fp.ContentHash == "" {
			return ""
		}
		h.Write([]byte(pkgPath))
		h.Write([]byte{0})
		h.Write([]byte(fp.ContentHash))
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%x", h.Sum(nil))
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

func describeCacheFileDiffs(stored []cacheFile, current []cacheFile) []string {
	if len(stored) == 0 && len(current) == 0 {
		return nil
	}
	storedByPath := make(map[string]cacheFile, len(stored))
	currentByPath := make(map[string]cacheFile, len(current))
	for _, file := range stored {
		storedByPath[filepath.Clean(file.Path)] = file
	}
	for _, file := range current {
		currentByPath[filepath.Clean(file.Path)] = file
	}
	paths := make([]string, 0, len(storedByPath)+len(currentByPath))
	seen := make(map[string]struct{}, len(storedByPath)+len(currentByPath))
	for path := range storedByPath {
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	for path := range currentByPath {
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	diffs := make([]string, 0, len(paths))
	for _, path := range paths {
		storedFile, storedOK := storedByPath[path]
		currentFile, currentOK := currentByPath[path]
		switch {
		case !storedOK:
			diffs = append(diffs, fmt.Sprintf("%s added size=%d mtime=%d", path, currentFile.Size, currentFile.ModTime))
		case !currentOK:
			diffs = append(diffs, fmt.Sprintf("%s removed size=%d mtime=%d", path, storedFile.Size, storedFile.ModTime))
		case storedFile != currentFile:
			diffs = append(diffs, fmt.Sprintf("%s size:%d->%d mtime:%d->%d", path, storedFile.Size, currentFile.Size, storedFile.ModTime, currentFile.ModTime))
		}
	}
	return diffs
}

func manifestNeedsLocalRefresh(stored []packageFingerprint, current []packageFingerprint) bool {
	if len(stored) != len(current) {
		return false
	}
	for i := range stored {
		if stored[i].PkgPath != current[i].PkgPath {
			return false
		}
		if stored[i].ContentHash == "" && current[i].ContentHash != "" {
			return true
		}
		if len(stored[i].Dirs) == 0 && len(current[i].Dirs) > 0 {
			return true
		}
	}
	return false
}

func packageDirectoryMetaChanged(fp packageFingerprint, storedFiles []string) ([]cacheFile, bool, error) {
	dirs := packageFingerprintDirs(storedFiles)
	if len(dirs) == 0 {
		return nil, false, nil
	}
	current, err := buildCacheFiles(dirs)
	if err != nil {
		return nil, false, err
	}
	if len(fp.Dirs) != len(current) {
		return current, true, nil
	}
	for i := range current {
		if current[i] != fp.Dirs[i] {
			return current, true, nil
		}
	}
	return current, false, nil
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

func removeIncrementalManifestFile(key string) {
	if key == "" {
		return
	}
	_ = osRemove(incrementalManifestPath(key))
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
