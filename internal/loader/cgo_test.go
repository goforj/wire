package loader

import (
	"context"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// TestLocalCgoDiscoveryFallsBackWithoutLosingConstructors reproduces reuse of metadata that omitted native constructor bodies.
func TestLocalCgoDiscoveryFallsBackWithoutLosingConstructors(t *testing.T) {
	setting, err := exec.Command("go", "env", "CGO_ENABLED").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(setting)) != "1" {
		t.Skip("native cgo compiler is disabled")
	}
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/native\n\ngo 1.25\n")
	writeTestFile(t, filepath.Join(root, "native", "types.go"), "package native\n\ntype Value struct { Number int }\n")
	bridge := filepath.Join(root, "native", "bridge.go")
	writeTestFile(t, bridge, "package native\n\n/* static int value(void) { return 7; } */\nimport \"C\"\n\nfunc New() *Value { return &Value{Number: int(C.value())} }\n")
	writeTestFile(t, filepath.Join(root, "app", "app.go"), "package app\n\nimport \"example.com/native/native\"\n\nfunc Init() *native.Value { return native.New() }\n")
	discovery, err := New().LoadRootGraph(context.Background(), RootLoadRequest{WD: root, Env: os.Environ(), Patterns: []string{"./app"}, NeedDeps: true})
	if err != nil {
		t.Fatal(err)
	}
	if discovery.Discovery == nil || len(discovery.Discovery.meta["example.com/native/native"].CgoFiles) != 1 {
		t.Fatal("discovery lost cgo metadata")
	}
	result, err := New().LoadPackages(context.Background(), PackageLoadRequest{WD: root, Env: os.Environ(), Patterns: []string{"./app"}, Discovery: discovery.Discovery, Mode: packages.LoadAllSyntax, Fset: token.NewFileSet()})
	if err != nil {
		t.Fatal(err)
	}
	if result.Backend != ModeFallback || result.FallbackReason != FallbackReasonCustomUnsupported {
		t.Fatalf("native graph backend = %#v", result)
	}
	for _, pkg := range collectGraph(result.Packages) {
		if len(pkg.Errors) != 0 {
			t.Fatalf("native package errors: %v", pkg.Errors)
		}
		if pkg.PkgPath == "example.com/native/native" && pkg.Types.Scope().Lookup("New") == nil {
			t.Fatal("native constructor was lost")
		}
	}
	entry, err := buildDiscoveryCacheEntry(goListRequest{WD: root, Env: os.Environ()}, discovery.Discovery.meta)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, bridge, "package native\n\nimport _ \"strings\"\n\nfunc New() *Value { return &Value{Number: 8} }\n")
	if validateDiscoveryCacheEntry(entry) {
		t.Fatal("native source edit retained stale discovery")
	}
}
