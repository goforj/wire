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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

type goListRequest struct {
	WD       string
	Env      []string
	Tags     string
	Patterns []string
	NeedDeps bool
}

func runGoList(ctx context.Context, req goListRequest) (map[string]*packageMeta, error) {
	args := []string{"list", "-json", "-e", "-compiled", "-export"}
	if req.NeedDeps {
		args = append(args, "-deps")
	}
	if req.Tags != "" {
		args = append(args, "-tags=wireinject "+req.Tags)
	} else {
		args = append(args, "-tags=wireinject")
	}
	args = append(args, "--")
	args = append(args, req.Patterns...)

	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = req.WD
	if len(req.Env) > 0 {
		cmd.Env = req.Env
	} else {
		cmd.Env = os.Environ()
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go list: %w: %s", err, stderr.String())
	}
	dec := json.NewDecoder(&stdout)
	out := make(map[string]*packageMeta)
	for {
		var meta packageMeta
		if err := dec.Decode(&meta); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if meta.ImportPath == "" {
			continue
		}
		meta.Dir = canonicalLoaderPath(meta.Dir)
		for i, name := range meta.GoFiles {
			if !filepath.IsAbs(name) {
				meta.GoFiles[i] = filepath.Join(meta.Dir, name)
			}
		}
		for i, name := range meta.CompiledGoFiles {
			if !filepath.IsAbs(name) {
				meta.CompiledGoFiles[i] = filepath.Join(meta.Dir, name)
			}
		}
		if meta.Export != "" && !filepath.IsAbs(meta.Export) {
			meta.Export = filepath.Join(meta.Dir, meta.Export)
		}
		meta.Imports = normalizeImports(meta.Imports, meta.ImportMap)
		copyMeta := meta
		out[meta.ImportPath] = &copyMeta
	}
	return out, nil
}
