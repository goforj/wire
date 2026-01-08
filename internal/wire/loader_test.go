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
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAndGenerateModule(t *testing.T) {
	repoRoot := mustRepoRoot(t)
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require github.com/goforj/wire v0.0.0",
		"replace github.com/goforj/wire => " + repoRoot,
		"",
	}, "\n"))

	writeFile(t, filepath.Join(root, "app", "app.go"), strings.Join([]string{
		"package app",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "app", "wire.go"), strings.Join([]string{
		"//go:build wireinject",
		"// +build wireinject",
		"",
		"package app",
		"",
		"import (",
		"\t\"example.com/app/dep\"",
		"\t\"github.com/goforj/wire\"",
		")",
		"",
		"func Init() *dep.Foo {",
		"\twire.Build(dep.New)",
		"\treturn nil",
		"}",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"type Foo struct{}",
		"",
		"func New() *Foo {",
		"\treturn &Foo{}",
		"}",
		"",
	}, "\n"))

	writeFile(t, filepath.Join(root, "noop", "noop.go"), strings.Join([]string{
		"package noop",
		"",
		"type Thing struct{}",
		"",
	}, "\n"))

	env := append(os.Environ(), "GOWORK=off")
	ctx := context.Background()

	info, errs := Load(ctx, root, env, "", []string{"./app"})
	if len(errs) > 0 {
		t.Fatalf("Load returned errors: %v", errs)
	}
	if info == nil {
		t.Fatal("Load returned nil info")
	}
	if len(info.Injectors) != 1 || info.Injectors[0].FuncName != "Init" {
		t.Fatalf("Load returned unexpected injectors: %+v", info.Injectors)
	}

	gens, errs := Generate(ctx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("Generate returned errors: %v", errs)
	}
	if len(gens) != 1 {
		t.Fatalf("Generate returned %d results, want 1", len(gens))
	}
	if len(gens[0].Errs) > 0 {
		t.Fatalf("Generate result had errors: %v", gens[0].Errs)
	}
	if len(gens[0].Content) == 0 {
		t.Fatal("Generate returned empty output for wire package")
	}
	if gens[0].OutputPath == "" {
		t.Fatal("Generate returned empty output path")
	}

	noops, errs := Generate(ctx, root, env, []string{"./noop"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("Generate noop returned errors: %v", errs)
	}
	if len(noops) != 1 {
		t.Fatalf("Generate noop returned %d results, want 1", len(noops))
	}
	if len(noops[0].Errs) > 0 {
		t.Fatalf("Generate noop result had errors: %v", noops[0].Errs)
	}
	if noops[0].OutputPath == "" {
		t.Fatal("Generate noop returned empty output path")
	}
	if len(noops[0].Content) != 0 {
		t.Fatal("Generate noop returned unexpected output")
	}
}

func mustRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	repoRoot := filepath.Clean(filepath.Join(wd, "..", ".."))
	if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err != nil {
		t.Fatalf("repo root not found at %s: %v", repoRoot, err)
	}
	return repoRoot
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}
