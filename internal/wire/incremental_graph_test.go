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
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestIncrementalGraphRoundTrip(t *testing.T) {
	graph := &incrementalGraph{
		Version: incrementalGraphVersion,
		WD:      "/tmp/app",
		Tags:    "dev",
		Roots:   []string{"example.com/app", "example.com/other"},
		LocalReverse: map[string][]string{
			"example.com/dep": {"example.com/app"},
			"example.com/sub": {"example.com/dep", "example.com/other"},
		},
	}
	data, err := encodeIncrementalGraph(graph)
	if err != nil {
		t.Fatalf("encodeIncrementalGraph failed: %v", err)
	}
	got, err := decodeIncrementalGraph(data)
	if err != nil {
		t.Fatalf("decodeIncrementalGraph failed: %v", err)
	}
	if !reflect.DeepEqual(got, graph) {
		t.Fatalf("graph round-trip mismatch:\n got=%+v\nwant=%+v", got, graph)
	}
}

func TestAffectedRoots(t *testing.T) {
	graph := &incrementalGraph{
		Roots: []string{"example.com/app", "example.com/other"},
		LocalReverse: map[string][]string{
			"example.com/dep": {"example.com/app"},
			"example.com/sub": {"example.com/dep", "example.com/other"},
		},
	}
	got := affectedRoots(graph, []string{"example.com/sub"})
	want := []string{"example.com/app", "example.com/other"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("affectedRoots=%v want %v", got, want)
	}
}

func TestBuildIncrementalGraph(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/test\n\ngo 1.21\n")

	appFile := filepath.Join(root, "app", "app.go")
	depFile := filepath.Join(root, "dep", "dep.go")
	writeFile(t, appFile, "package app\n")
	writeFile(t, depFile, "package dep\n")

	dep := &packages.Package{
		PkgPath:         "example.com/test/dep",
		CompiledGoFiles: []string{depFile},
		GoFiles:         []string{depFile},
		Imports:         map[string]*packages.Package{},
	}
	app := &packages.Package{
		PkgPath:         "example.com/test/app",
		CompiledGoFiles: []string{appFile},
		GoFiles:         []string{appFile},
		Imports: map[string]*packages.Package{
			"example.com/test/dep": dep,
		},
	}

	graph := buildIncrementalGraph(root, "", []*packages.Package{app})
	if len(graph.Roots) != 1 || graph.Roots[0] != app.PkgPath {
		t.Fatalf("unexpected roots: %v", graph.Roots)
	}
	got := graph.LocalReverse[dep.PkgPath]
	want := []string{app.PkgPath}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected reverse edges: got=%v want=%v", got, want)
	}
}
