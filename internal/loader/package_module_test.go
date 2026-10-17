package loader

import (
	"go/token"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/tools/go/packages"
)

// TestPackageStubModule preserves module identities through cached package reconstruction.
func TestPackageStubModule(t *testing.T) {
	for _, mod := range []*goListModule{
		nil,
		{Path: "example.com/app", Main: true, Dir: "/workspace/app", GoMod: "/workspace/app/go.mod"},
		{Path: "example.com/lib", Version: "v1.0.0", Replace: &goListModule{Path: "../lib", Dir: "/workspace/lib", GoMod: "/workspace/lib/go.mod"}},
	} {
		pkg := packageStub(token.NewFileSet(), &packageMeta{Module: mod})
		var want *packages.Module
		if mod != nil {
			want = &packages.Module{Path: mod.Path, Version: mod.Version, Main: mod.Main, Dir: mod.Dir, GoMod: mod.GoMod}
			if mod.Replace != nil {
				want.Replace = &packages.Module{Path: mod.Replace.Path, Dir: mod.Replace.Dir, GoMod: mod.Replace.GoMod}
			}
		}
		if diff := cmp.Diff(want, pkg.Module); diff != "" {
			t.Fatalf("module metadata mismatch (-want +got):\n%s", diff)
		}
	}
}
