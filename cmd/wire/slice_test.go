package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/goforj/wire/internal/wire"
	"github.com/google/subcommands"
)

// TestShowSliceInputs checks that show reports collection dependencies without exposing element bindings.
func TestShowSliceInputs(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/sliceshow\n\ngo 1.25.0\nrequire github.com/goforj/wire v1.2.0\nreplace github.com/goforj/wire => " + filepath.ToSlash(repo) + "\n",
		"providers.go": `package sliceshow
import "github.com/goforj/wire"
// first and second intentionally return the same type.
func first(string) int { return 1 }
// second has a different input to expose the union of collection dependencies.
func second(bool) int { return 2 }
// Set exposes only the assembled slice.
var Set = wire.NewSet(wire.Slice(new([]int), first, second))
// Empty exposes an empty collection without inputs.
var Empty = wire.NewSet(wire.Slice(new([]string)))
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	env := append(os.Environ(), "GOWORK=off", "WIRE_CACHE_DIR="+filepath.Join(root, "cache"))
	info, errs := wire.Load(context.Background(), root, env, "", []string{"."})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	for _, test := range []struct{ set, input, output string }{
		{"Set", "bool, string", "[]int"},
		{"Empty", "no inputs", "[]string"},
	} {
		groups, _ := gather(info, wire.ProviderSetID{ImportPath: "example.com/sliceshow", VarName: test.set})
		if len(groups) != 1 || groups[0].name != test.input || groups[0].outputs.Len() != 1 {
			t.Fatalf("unexpected groups for %s: %+v", test.set, groups)
		}
		for _, out := range info.Sets[wire.ProviderSetID{ImportPath: "example.com/sliceshow", VarName: test.set}].Outputs() {
			if out.String() != test.output {
				t.Fatalf("output = %s, want %s", out, test.output)
			}
		}
	}
	// Execute also exercises formatting the collection's source position.
	t.Chdir(root)
	t.Setenv("GOWORK", "off")
	t.Setenv("WIRE_CACHE_DIR", filepath.Join(root, "cache"))
	cmd := new(showCmd)
	flags := flag.NewFlagSet("show", flag.ContinueOnError)
	cmd.SetFlags(flags)
	if got := cmd.Execute(context.Background(), flags); got != subcommands.ExitSuccess {
		t.Fatalf("show exit = %v", got)
	}
}
