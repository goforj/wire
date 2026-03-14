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
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

const incrementalGraphVersion = "wire-incremental-graph-v1"

type incrementalGraph struct {
	Version      string
	WD           string
	Tags         string
	Roots        []string
	LocalReverse map[string][]string
}

func analyzeIncrementalGraph(ctx context.Context, wd string, env []string, tags string, pkgs []*packages.Package, snapshot *incrementalFingerprintSnapshot) {
	if !IncrementalEnabled(ctx, env) || snapshot == nil {
		return
	}
	graph := buildIncrementalGraph(wd, tags, pkgs)
	writeIncrementalGraph(incrementalGraphKey(wd, tags, graph.Roots), graph)
	if len(snapshot.changed) == 0 {
		return
	}
	affected := affectedRoots(graph, snapshot.changed)
	if len(affected) > 0 {
		debugf(ctx, "incremental.graph changed=%s affected_roots=%s", stringsJoin(snapshot.changed), stringsJoin(affected))
	} else {
		debugf(ctx, "incremental.graph changed=%s affected_roots=", stringsJoin(snapshot.changed))
	}
}

func buildIncrementalGraph(wd string, tags string, pkgs []*packages.Package) *incrementalGraph {
	moduleRoot := findModuleRoot(wd)
	graph := &incrementalGraph{
		Version:      incrementalGraphVersion,
		WD:           filepath.Clean(wd),
		Tags:         tags,
		Roots:        make([]string, 0, len(pkgs)),
		LocalReverse: make(map[string][]string),
	}
	for _, pkg := range pkgs {
		if pkg == nil {
			continue
		}
		graph.Roots = append(graph.Roots, pkg.PkgPath)
	}
	sort.Strings(graph.Roots)
	for _, pkg := range collectAllPackages(pkgs) {
		if classifyPackageLocation(moduleRoot, pkg) != "local" {
			continue
		}
		for _, imp := range pkg.Imports {
			if classifyPackageLocation(moduleRoot, imp) != "local" {
				continue
			}
			graph.LocalReverse[imp.PkgPath] = append(graph.LocalReverse[imp.PkgPath], pkg.PkgPath)
		}
	}
	for path := range graph.LocalReverse {
		sort.Strings(graph.LocalReverse[path])
	}
	return graph
}

func affectedRoots(graph *incrementalGraph, changed []string) []string {
	if graph == nil || len(changed) == 0 {
		return nil
	}
	rootSet := make(map[string]struct{}, len(graph.Roots))
	for _, root := range graph.Roots {
		rootSet[root] = struct{}{}
	}
	seen := make(map[string]struct{})
	queue := append([]string(nil), changed...)
	affected := make(map[string]struct{})
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if _, ok := seen[cur]; ok {
			continue
		}
		seen[cur] = struct{}{}
		if _, ok := rootSet[cur]; ok {
			affected[cur] = struct{}{}
		}
		for _, next := range graph.LocalReverse[cur] {
			if _, ok := seen[next]; !ok {
				queue = append(queue, next)
			}
		}
	}
	out := make([]string, 0, len(affected))
	for root := range affected {
		out = append(out, root)
	}
	sort.Strings(out)
	return out
}

func incrementalGraphKey(wd string, tags string, roots []string) string {
	h := sha256.New()
	h.Write([]byte(incrementalGraphVersion))
	h.Write([]byte{0})
	h.Write([]byte(filepath.Clean(wd)))
	h.Write([]byte{0})
	h.Write([]byte(tags))
	h.Write([]byte{0})
	for _, root := range roots {
		h.Write([]byte(root))
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func incrementalGraphPath(key string) string {
	return filepath.Join(cacheDir(), key+".igr")
}

func writeIncrementalGraph(key string, graph *incrementalGraph) {
	data, err := encodeIncrementalGraph(graph)
	if err != nil {
		return
	}
	dir := cacheDir()
	if err := osMkdirAll(dir, 0755); err != nil {
		return
	}
	tmp, err := osCreateTemp(dir, key+".igr-")
	if err != nil {
		return
	}
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		osRemove(tmp.Name())
		return
	}
	if err := osRename(tmp.Name(), incrementalGraphPath(key)); err != nil {
		osRemove(tmp.Name())
	}
}

func readIncrementalGraph(key string) (*incrementalGraph, bool) {
	data, err := osReadFile(incrementalGraphPath(key))
	if err != nil {
		return nil, false
	}
	graph, err := decodeIncrementalGraph(data)
	if err != nil {
		return nil, false
	}
	return graph, true
}

func encodeIncrementalGraph(graph *incrementalGraph) ([]byte, error) {
	if graph == nil {
		return nil, fmt.Errorf("nil incremental graph")
	}
	var buf bytes.Buffer
	writeString := func(s string) error {
		if err := binary.Write(&buf, binary.LittleEndian, uint32(len(s))); err != nil {
			return err
		}
		_, err := buf.WriteString(s)
		return err
	}
	for _, s := range []string{graph.Version, graph.WD, graph.Tags} {
		if err := writeString(s); err != nil {
			return nil, err
		}
	}
	if err := binary.Write(&buf, binary.LittleEndian, uint32(len(graph.Roots))); err != nil {
		return nil, err
	}
	for _, root := range graph.Roots {
		if err := writeString(root); err != nil {
			return nil, err
		}
	}
	keys := make([]string, 0, len(graph.LocalReverse))
	for k := range graph.LocalReverse {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if err := binary.Write(&buf, binary.LittleEndian, uint32(len(keys))); err != nil {
		return nil, err
	}
	for _, k := range keys {
		if err := writeString(k); err != nil {
			return nil, err
		}
		children := append([]string(nil), graph.LocalReverse[k]...)
		sort.Strings(children)
		if err := binary.Write(&buf, binary.LittleEndian, uint32(len(children))); err != nil {
			return nil, err
		}
		for _, child := range children {
			if err := writeString(child); err != nil {
				return nil, err
			}
		}
	}
	return buf.Bytes(), nil
}

func decodeIncrementalGraph(data []byte) (*incrementalGraph, error) {
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
	var rootCount uint32
	if err := binary.Read(r, binary.LittleEndian, &rootCount); err != nil {
		return nil, err
	}
	roots := make([]string, 0, rootCount)
	for i := uint32(0); i < rootCount; i++ {
		root, err := readString()
		if err != nil {
			return nil, err
		}
		roots = append(roots, root)
	}
	var edgeCount uint32
	if err := binary.Read(r, binary.LittleEndian, &edgeCount); err != nil {
		return nil, err
	}
	reverse := make(map[string][]string, edgeCount)
	for i := uint32(0); i < edgeCount; i++ {
		k, err := readString()
		if err != nil {
			return nil, err
		}
		var n uint32
		if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
			return nil, err
		}
		children := make([]string, 0, n)
		for j := uint32(0); j < n; j++ {
			child, err := readString()
			if err != nil {
				return nil, err
			}
			children = append(children, child)
		}
		reverse[k] = children
	}
	return &incrementalGraph{
		Version:      version,
		WD:           wd,
		Tags:         tags,
		Roots:        roots,
		LocalReverse: reverse,
	}, nil
}

func stringsJoin(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return strings.Join(items, ",")
}
