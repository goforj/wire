package wire

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/goforj/wire/internal/cachepolicy"
	"golang.org/x/tools/go/packages"
)

// TestIsLocalWirePackage distinguishes mutable local sources from versioned dependencies.
func TestIsLocalWirePackage(t *testing.T) {
	root := t.TempDir()
	tooling, sibling := filepath.Join(root, "tooling"), filepath.Join(root, "sibling")
	for _, dir := range []string{tooling, sibling} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name string
		pkg  *packages.Package
		want bool
	}{
		{"nil", nil, false},
		{"no files", &packages.Package{}, false},
		{"current module", &packages.Package{GoFiles: []string{filepath.Join(tooling, "p.go")}}, true},
		{"workspace sibling", &packages.Package{GoFiles: []string{filepath.Join(sibling, "p.go")}, Module: &packages.Module{Main: true, Dir: sibling}}, true},
		{"filesystem replacement", &packages.Package{GoFiles: []string{filepath.Join(sibling, "p.go")}, Module: &packages.Module{Replace: &packages.Module{Dir: sibling}}}, true},
		{"versioned replacement", &packages.Package{GoFiles: []string{filepath.Join(sibling, "p.go")}, Module: &packages.Module{Replace: &packages.Module{Dir: sibling, Version: "v1.2.3"}}}, false},
		{"versioned dependency", &packages.Package{GoFiles: []string{filepath.Join(sibling, "p.go")}, Module: &packages.Module{Dir: sibling, Version: "v1.2.3"}}, false},
		{"outside module directory", &packages.Package{GoFiles: []string{filepath.Join(root, "sibling-other", "p.go")}, Module: &packages.Module{Main: true, Dir: sibling}}, false},
		{"incomplete module metadata", &packages.Package{GoFiles: []string{filepath.Join(sibling, "p.go")}, Module: &packages.Module{Main: true}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isLocalWirePackage(tooling, tt.pkg); got != tt.want {
				t.Fatalf("isLocalWirePackage = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestOutputCacheEnabled(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		env  []string
		want bool
	}{
		{
			name: "enabled with artifacts",
			env:  []string{"WIRE_LOADER_ARTIFACTS=1"},
			want: true,
		},
		{
			name: "disabled without artifacts",
			env:  []string{"WIRE_LOADER_ARTIFACTS=0"},
			want: false,
		},
		{
			name: "disabled by dedicated env",
			env:  []string{"WIRE_LOADER_ARTIFACTS=1", "WIRE_OUTPUT_CACHE=0"},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := outputCacheEnabled(ctx, t.TempDir(), tt.env); got != tt.want {
				t.Fatalf("outputCacheEnabled(..., %v) = %v, want %v", tt.env, got, tt.want)
			}
		})
	}
}

func TestOutputCacheKeyDefaultModTimeIgnoresContentOnlyChangeWithSameMetadata(t *testing.T) {
	restore := cachepolicy.SetForTest(cachepolicy.ModeMTime)
	defer restore()

	wd := t.TempDir()
	if err := os.WriteFile(filepath.Join(wd, "go.mod"), []byte("module example.com/app\n\ngo 1.19\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	goFile := filepath.Join(wd, "wire.go")
	if err := os.WriteFile(goFile, []byte("package app\n\nfunc Init() string { return \"one\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(goFile)
	if err != nil {
		t.Fatal(err)
	}
	root := &packages.Package{
		PkgPath: "example.com/app",
		GoFiles: []string{goFile},
	}

	first, err := outputCacheKey(wd, &GenerateOptions{}, root)
	if err != nil {
		t.Fatalf("outputCacheKey(first) error = %v", err)
	}

	if err := os.WriteFile(goFile, []byte("package app\n\nfunc Init() string { return \"two\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(goFile, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}

	second, err := outputCacheKey(wd, &GenerateOptions{}, root)
	if err != nil {
		t.Fatalf("outputCacheKey(second) error = %v", err)
	}

	if first != second {
		t.Fatalf("default mod-time output cache key changed: %q != %q", first, second)
	}
}

func TestOutputCacheKeyContentModeDetectsContentOnlyChangeWithSameMetadata(t *testing.T) {
	wd := t.TempDir()
	if err := os.WriteFile(filepath.Join(wd, "go.mod"), []byte("module example.com/app\n\ngo 1.19\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	goFile := filepath.Join(wd, "wire.go")
	if err := os.WriteFile(goFile, []byte("package app\n\nfunc Init() string { return \"one\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(goFile)
	if err != nil {
		t.Fatal(err)
	}
	root := &packages.Package{
		PkgPath: "example.com/app",
		GoFiles: []string{goFile},
	}
	restore := cachepolicy.SetForTest(cachepolicy.ModeContent)
	defer restore()

	first, err := outputCacheKey(wd, &GenerateOptions{}, root)
	if err != nil {
		t.Fatalf("outputCacheKey(first) error = %v", err)
	}

	if err := os.WriteFile(goFile, []byte("package app\n\nfunc Init() string { return \"two\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(goFile, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}

	second, err := outputCacheKey(wd, &GenerateOptions{}, root)
	if err != nil {
		t.Fatalf("outputCacheKey(second) error = %v", err)
	}

	if first == second {
		t.Fatalf("content-based output cache key did not change")
	}
}
