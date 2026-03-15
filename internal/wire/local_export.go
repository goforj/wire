// Copyright 2026 The Wire Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
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
	"go/token"
	"go/types"
	"path/filepath"

	"golang.org/x/tools/go/gcexportdata"
	"golang.org/x/tools/go/packages"
)

const localExportVersion = "wire-local-export-v1"

func localExportKey(wd string, tags string, pkgPath string, shapeHash string) string {
	sum := sha256.Sum256([]byte(localExportVersion + "\x00" + packageCacheScope(wd) + "\x00" + tags + "\x00" + pkgPath + "\x00" + shapeHash))
	return fmt.Sprintf("%x", sum[:])
}

func localExportPath(key string) string {
	return filepath.Join(cacheDir(), key+".iexp")
}

func localExportPathForFingerprint(wd string, tags string, fp *packageFingerprint) string {
	if fp == nil || fp.PkgPath == "" || fp.ShapeHash == "" {
		return ""
	}
	return localExportPath(localExportKey(wd, tags, fp.PkgPath, fp.ShapeHash))
}

func localExportExists(wd string, tags string, fp *packageFingerprint) bool {
	path := localExportPathForFingerprint(wd, tags, fp)
	if path == "" {
		return false
	}
	_, err := osStat(path)
	return err == nil
}

func writeLocalPackageExports(wd string, tags string, pkgs []*packages.Package, fps map[string]*packageFingerprint) {
	if len(pkgs) == 0 || len(fps) == 0 {
		return
	}
	moduleRoot := findModuleRoot(wd)
	for _, pkg := range pkgs {
		if pkg == nil || pkg.Types == nil || pkg.PkgPath == "" {
			continue
		}
		if classifyPackageLocation(moduleRoot, pkg) != "local" {
			continue
		}
		fp := fps[pkg.PkgPath]
		path := localExportPathForFingerprint(wd, tags, fp)
		if path == "" {
			continue
		}
		writeLocalPackageExportFile(path, pkg.Fset, pkg.Types)
	}
}

func writeLocalPackageExportFile(path string, fset *token.FileSet, pkg *types.Package) {
	if path == "" || fset == nil || pkg == nil {
		return
	}
	dir := cacheDir()
	if err := osMkdirAll(dir, 0755); err != nil {
		return
	}
	tmp, err := osCreateTemp(dir, filepath.Base(path)+".tmp-")
	if err != nil {
		return
	}
	writeErr := gcexportdata.Write(tmp, fset, pkg)
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		osRemove(tmp.Name())
		return
	}
	if err := osRename(tmp.Name(), path); err != nil {
		osRemove(tmp.Name())
	}
}
