package wire

import (
	"context"
	"fmt"
	"go/ast"
	"go/build"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/types/typeutil"
)

// TestSliceValidation checks malformed directives and collection graph failures through generation.
func TestSliceValidation(t *testing.T) {
	repo, err := importBenchRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, build, out, want string
	}{
		{"nil type", "wire.Slice(nil, provideInt)", "[]int", "pointer to a slice type"},
		{"non pointer", "wire.Slice([]int{}, provideInt)", "[]int", "pointer to a slice type"},
		{"non slice", "wire.Slice(new(int), provideInt)", "[]int", "pointer to a slice type"},
		{"wrong element", "wire.Slice(new([]int), provideString)", "[]int", "not assignable to int"},
		{"non provider expression", "wire.Slice(new([]int), 42)", "[]int", "unknown pattern"},
		{"value element", "wire.Slice(new([]int), wire.Value(1))", "[]int", "elements must be provider functions"},
		{"struct element", `wire.Slice(new([]item), wire.Struct(new(item), "*"))`, "[]item", "elements must be provider functions"},
		{"set element", "wire.Slice(new([]int), wire.NewSet(provideInt))", "[]int", "elements must be provider functions"},
		{"nested collection element", "wire.Slice(new([]int), wire.Slice(new([]int), provideInt))", "[]int", "elements must be provider functions"},
		{"invalid signature", "wire.Slice(new([]int), invalid)", "[]int", "wrong signature for provider invalid"},
		{"missing dependency", "wire.Slice(new([]int), needsString)", "[]int", "no provider found for string"},
		{"collection cycle", "wire.Slice(new([]int), cyclic)", "[]int", "cycle for []int"},
		{"indirect cycle", "wire.Slice(new([]int), needsConfig), newConfig", "[]int", "cycle for"},
		{"extended imported binding cycle", "wire.NewSet(wire.Slice(new(ints), provideInt), wire.Bind(new(sized), new(ints))), wire.Slice(new(ints), needsSized)", "ints", "cycle for"},
		{"ordinary collection conflict", "wire.Slice(new([]int), provideInt), provideSlice", "[]int", "multiple bindings for []int"},
		{"imported collection conflict", "wire.NewSet(wire.Slice(new([]int), provideInt)), wire.NewSet(provideSlice)", "[]int", "multiple bindings for []int"},
		{"reverse collection conflict", "wire.NewSet(provideSlice), wire.NewSet(wire.Slice(new([]int), provideInt))", "[]int", "multiple bindings for []int"},
		{"ordinary duplicates", "provideInt, anotherInt, wire.Slice(new([]int))", "[]int", "multiple bindings for int"},
		{"unused collection", "wire.Slice(new([]int), provideInt), provideString", "string", "unused wire.Slice for []int"},
		{"unused imported collection", "wire.NewSet(wire.Slice(new([]int), provideInt)), provideString", "string", "unused provider set"},
		{"element not globally bound", "wire.Slice(new([]int), provideInt), consumeBoth", "string", "no provider found for int"},
		{"cleanup signature", "wire.Slice(new([]int), cleanupInt)", "[]int", "returns cleanup but injection does not return cleanup"},
		{"error signature", "wire.Slice(new([]int), errorInt)", "[]int", "returns error but injection not allowed to fail"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			mod := "module example.com/slicetest\n\ngo 1.25.0\nrequire github.com/goforj/wire v1.2.0\nreplace github.com/goforj/wire => " + filepath.ToSlash(repo) + "\n"
			if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(mod), 0o644); err != nil {
				t.Fatal(err)
			}
			source := `//go:build wireinject

package slicetest
import "github.com/goforj/wire"
// item is a valid struct rejected as a collection constructor.
type item struct{}
// config forms an indirect collection cycle.
type config struct{}
// ints is a named collection that supports an interface binding.
type ints []int
// Size permits collection injection through an ordinary interface binding.
func (v ints) Size() int { return len(v) }
// sized forms a cycle when an added element consumes its own bound collection.
type sized interface { Size() int }
// needsSized closes a cycle introduced after the binding was imported.
func needsSized(sized) int { return 1 }
// provideInt is a valid element constructor.
func provideInt() int { return 1 }
// anotherInt preserves ordinary duplicate binding checks.
func anotherInt() int { return 2 }
// provideString supplies an incompatible element type.
func provideString() string { return "value" }
// invalid has an unsupported provider signature.
func invalid() (int, int) { return 1, 2 }
// needsString requires a deliberately missing dependency.
func needsString(string) int { return 1 }
// cyclic needs the collection to construct one of its own elements.
func cyclic([]int) int { return 1 }
// needsConfig forms an indirect cycle through an ordinary provider.
func needsConfig(config) int { return 1 }
// newConfig closes the indirect cycle.
func newConfig([]int) config { return config{} }
// provideSlice conflicts with an explicit collection directive.
func provideSlice() []int { return nil }
// consumeBoth proves collection results do not become ordinary element bindings.
func consumeBoth([]int, int) string { return "value" }
// cleanupInt requires an injector cleanup result.
func cleanupInt() (int, func()) { return 1, func(){} }
// errorInt requires an injector error result.
func errorInt() (int, error) { return 1, nil }
// initialize supplies the collection under test.
func initialize() ` + tt.out + ` { wire.Build(` + tt.build + `); return *new(` + tt.out + `) }
`
			if err := os.WriteFile(filepath.Join(root, "wire.go"), []byte(source), 0o644); err != nil {
				t.Fatal(err)
			}
			env := append(os.Environ(), "GOWORK=off", "WIRE_CACHE_DIR="+filepath.Join(root, "cache"))
			gens, errs := Generate(context.Background(), root, env, []string{"."}, &GenerateOptions{})
			messages := comparableGenerateErrors(errs)
			for _, gen := range gens {
				messages = append(messages, comparableGenerateErrors(gen.Errs)...)
			}
			if got := strings.Join(messages, "\n"); !strings.Contains(got, tt.want) {
				t.Fatalf("Generate error = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestProcessSliceMissingType checks an incomplete AST before type information is needed.
func TestProcessSliceMissingType(t *testing.T) {
	oc := &objectCache{fset: token.NewFileSet()}
	_, errs := oc.processSlice(new(types.Info), "", &ast.CallExpr{Fun: ast.NewIdent("Slice")})
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "requires a pointer to a slice type") {
		t.Fatalf("processSlice errors = %v", errs)
	}
}

// TestSliceProgram compiles and runs collection lifecycles with both supported loaders on every test run.
func TestSliceProgram(t *testing.T) {
	marker, err := os.ReadFile(filepath.Join("..", "..", "wire.go"))
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := loadTestCase(filepath.Join("testdata", "Slice"), marker)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOWORK", "off")
	for _, mode := range []string{"custom", "fallback"} {
		t.Run(mode, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := fixture.materialize(root); err != nil {
				t.Fatal(err)
			}
			env := append(os.Environ(), "GOPATH="+root, "WIRE_LOADER_MODE="+mode, "WIRE_CACHE_DIR="+filepath.Join(root, "cache"))
			gens, errs := Generate(context.Background(), filepath.Join(root, "src", "example.com"), env, []string{fixture.pkg}, &GenerateOptions{})
			if len(errs) > 0 || len(gens) != 1 {
				t.Fatalf("Generate returned %d outputs, errors: %v", len(gens), errs)
			}
			if len(gens[0].Errs) > 0 {
				t.Fatal(gens[0].Errs)
			}
			if err := gens[0].Commit(); err != nil {
				t.Fatal(err)
			}
			if err := goBuildCheck(filepath.Join(build.Default.GOROOT, "bin", "go"), root, fixture); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// BenchmarkSolveSlice measures same-type constructor resolution as collection size grows.
func BenchmarkSolveSlice(b *testing.B) {
	for _, size := range []int{10, 100, 1000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			pkg := types.NewPackage("example.com/slicebench", "slicebench")
			collection := &SliceProvider{Out: types.NewSlice(types.Typ[types.Int])}
			for i := 0; i < size; i++ {
				collection.Providers = append(collection.Providers, &Provider{
					Pkg:  pkg,
					Name: fmt.Sprintf("newValue%04d", i),
					Args: []ProviderInput{{Type: types.Typ[types.String]}},
					Out:  []types.Type{types.Typ[types.Int]},
				})
			}
			set := &ProviderSet{Slices: []*SliceProvider{collection}, sliceInputs: []*providerSetSrc{{Slice: collection}}}
			fset := token.NewFileSet()
			var errs []error
			set.providerMap, set.srcMap, errs = buildProviderMap(fset, typeutil.MakeHasher(), set)
			if len(errs) > 0 {
				b.Fatal(errs)
			}
			given := types.NewTuple(types.NewVar(token.NoPos, nil, "shared", types.Typ[types.String]))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				calls, errs := solve(fset, collection.Out, given, set)
				if len(errs) > 0 || len(calls) != size+1 {
					b.Fatalf("solve returned %d calls and errors %v", len(calls), errs)
				}
			}
		})
	}
}
