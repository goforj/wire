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

package loader

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"golang.org/x/tools/go/gcexportdata"

	"github.com/goforj/wire/internal/cachepaths"
)

const (
	loaderArtifactEnv    = "WIRE_LOADER_ARTIFACTS"
	loaderArtifactDirEnv = cachepaths.LoaderArtifactDirEnv
)

func loaderArtifactEnabled(env []string) bool {
	return envValue(env, loaderArtifactEnv) != "0"
}

func loaderArtifactDir(env []string) (string, error) {
	return cachepaths.Dir(env, loaderArtifactDirEnv, "loader-artifacts")
}

func loaderArtifactPath(env []string, meta *packageMeta, isLocal bool) (string, error) {
	dir, err := loaderArtifactDir(env)
	if err != nil {
		return "", err
	}
	key, err := loaderArtifactKey(meta, isLocal)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, key+".bin"), nil
}

func loaderArtifactKey(meta *packageMeta, isLocal bool) (string, error) {
	sum := sha256.New()
	sum.Write([]byte("wire-loader-artifact-v4\n"))
	sum.Write([]byte(runtime.Version()))
	sum.Write([]byte{'\n'})
	sum.Write([]byte(meta.ImportPath))
	sum.Write([]byte{'\n'})
	sum.Write([]byte(meta.Name))
	sum.Write([]byte{'\n'})
	if !isLocal {
		sum.Write([]byte(meta.Export))
		sum.Write([]byte{'\n'})
		if meta.Export != "" {
			h, err := hashFileContent(meta.Export)
			if err != nil {
				return "", err
			}
			sum.Write([]byte(h))
			sum.Write([]byte{'\n'})
		} else {
			if err := hashMetaFiles(sum, metaFiles(meta)); err != nil {
				return "", err
			}
		}
		if meta.Error != nil {
			sum.Write([]byte(meta.Error.Err))
			sum.Write([]byte{'\n'})
		}
		return hex.EncodeToString(sum.Sum(nil)), nil
	}
	if err := hashMetaFiles(sum, metaFiles(meta)); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// hashFileContent returns the hex-encoded SHA-256 of the file content.
func hashFileContent(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), nil
}

// hashMetaFiles writes content-based hashes for each file into sum.
func hashMetaFiles(sum io.Writer, names []string) error {
	for _, name := range names {
		sum.Write([]byte(name))
		sum.Write([]byte{'\n'})
		h, err := hashFileContent(name)
		if err != nil {
			return err
		}
		sum.Write([]byte(h))
		sum.Write([]byte{'\n'})
	}
	return nil
}

func readLoaderArtifact(path string, fset *token.FileSet, imports map[string]*types.Package, pkgPath string) (*types.Package, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return readLoaderArtifactData(data, fset, imports, pkgPath)
}

func readLoaderArtifactData(data []byte, fset *token.FileSet, imports map[string]*types.Package, pkgPath string) (*types.Package, error) {
	return gcexportdata.Read(bytes.NewReader(data), fset, imports, pkgPath)
}

func writeLoaderArtifact(path string, fset *token.FileSet, pkg *types.Package) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var out bytes.Buffer
	if err := gcexportdata.Write(&out, fset, pkg); err != nil {
		return err
	}
	return os.WriteFile(path, out.Bytes(), 0o644)
}

func artifactUpToDate(env []string, artifactPath string, meta *packageMeta, isLocal bool) bool {
	_, err := os.Stat(artifactPath)
	return err == nil
}

func isProviderSetTypeForLoader(t types.Type) bool {
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	switch obj.Pkg().Path() {
	case "github.com/goforj/wire", "github.com/google/wire":
		return obj.Name() == "ProviderSet"
	default:
		return false
	}
}
