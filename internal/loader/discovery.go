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
	"time"
)

type goListRequest struct {
	WD           string
	Env          []string
	Tags         string
	Patterns     []string
	NeedDeps     bool
	SkipCompiled bool
}

func runGoList(ctx context.Context, req goListRequest) (map[string]*packageMeta, error) {
	cacheReadStart := time.Now()
	if cached, ok := readDiscoveryCache(req); ok {
		logDuration(ctx, "loader.discovery.cache_read.wall", time.Since(cacheReadStart))
		logDuration(ctx, "loader.discovery.golist.wall", 0)
		logDuration(ctx, "loader.discovery.decode.wall", 0)
		logDuration(ctx, "loader.discovery.canonicalize.wall", 0)
		logDuration(ctx, "loader.discovery.cache_build.wall", 0)
		logDuration(ctx, "loader.discovery.cache_write.wall", 0)
		return cached, nil
	}
	logDuration(ctx, "loader.discovery.cache_read.wall", time.Since(cacheReadStart))
	args := []string{"list", "-json", "-e", "-export"}
	if !req.SkipCompiled {
		args = append(args, "-compiled")
	}
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
	goListStart := time.Now()
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go list: %w: %s", err, stderr.String())
	}
	goListDuration := time.Since(goListStart)
	dec := json.NewDecoder(&stdout)
	out := make(map[string]*packageMeta)
	var decodeDuration time.Duration
	var canonicalizeDuration time.Duration
	for {
		var meta packageMeta
		decodeStart := time.Now()
		if err := dec.Decode(&meta); err != nil {
			decodeDuration += time.Since(decodeStart)
			if err == io.EOF {
				break
			}
			return nil, err
		}
		decodeDuration += time.Since(decodeStart)
		if meta.ImportPath == "" {
			continue
		}
		canonicalizeStart := time.Now()
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
		canonicalizeDuration += time.Since(canonicalizeStart)
		copyMeta := meta
		out[meta.ImportPath] = &copyMeta
	}
	cacheBuildStart := time.Now()
	entry, err := buildDiscoveryCacheEntry(req, out)
	cacheBuildDuration := time.Since(cacheBuildStart)
	if err == nil && entry != nil {
		cacheWriteStart := time.Now()
		_ = saveDiscoveryCacheEntry(req, entry)
		logDuration(ctx, "loader.discovery.cache_write.wall", time.Since(cacheWriteStart))
	} else {
		logDuration(ctx, "loader.discovery.cache_write.wall", 0)
	}
	logDuration(ctx, "loader.discovery.golist.wall", goListDuration)
	logDuration(ctx, "loader.discovery.decode.wall", decodeDuration)
	logDuration(ctx, "loader.discovery.canonicalize.wall", canonicalizeDuration)
	logDuration(ctx, "loader.discovery.cache_build.wall", cacheBuildDuration)
	return out, nil
}
