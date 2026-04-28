package wire

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/goforj/wire/internal/cachepolicy"
	"golang.org/x/tools/go/packages"
)

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
