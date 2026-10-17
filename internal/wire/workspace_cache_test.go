package wire

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goforj/wire/internal/cachepolicy"
)

// TestGenerateCachedSiblingProviderEdit covers provider refactors after discovery and output caches are warm.
func TestGenerateCachedSiblingProviderEdit(t *testing.T) {
	for _, mode := range []string{cachepolicy.ModeMTime, cachepolicy.ModeContent} {
		for _, workspace := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/workspace=%t", mode, workspace), func(t *testing.T) {
				restore := cachepolicy.SetForTest(mode)
				defer restore()
				root, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				repo, err := importBenchRepoRoot()
				if err != nil {
					t.Fatal(err)
				}
				write := func(name, content string) {
					t.Helper()
					name = filepath.Join(root, name)
					if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				module := "\n\ngo 1.25.0\n\nrequire github.com/goforj/wire v1.2.0\nreplace github.com/goforj/wire => " + filepath.ToSlash(repo) + "\n"
				write("tooling/go.mod", "module example.com/tooling"+module+"\nrequire example.com/baseapp v0.0.0\nreplace example.com/baseapp => ../baseapp\nrequire example.com/app v0.0.0\nreplace example.com/app => ../app\n")
				write("baseapp/go.mod", "module example.com/baseapp"+module)
				write("app/go.mod", "module example.com/app"+module+"\nrequire example.com/baseapp v0.0.0\nreplace example.com/baseapp => ../baseapp\n")
				work := "off"
				if workspace {
					write("go.work", "go 1.25.0\nuse (\n ./tooling\n ./baseapp\n ./app\n)\n")
					work = filepath.Join(root, "go.work")
				}
				write("baseapp/provider.go", `package baseapp
import "github.com/goforj/wire"
// Options is the configuration removed by the refactor.
type Options struct{}
// Application is shared by both injectors.
type Application struct{}
// NewApplication initially requires configuration.
func NewApplication(Options) *Application { return &Application{} }
// Set supplies the application and its original configuration.
var Set = wire.NewSet(NewApplication, wire.Struct(new(Options), "*"))
`)
				write("baseapp/wire.go", `//go:build wireinject

package baseapp
import "github.com/goforj/wire"
// Initialize declares the library injector.
func Initialize() *Application { wire.Build(Set); return nil }
`)
				write("app/wire.go", `//go:build wireinject

package app
import ("github.com/goforj/wire"; "example.com/baseapp")
// Initialize declares the consumer injector.
func Initialize() *baseapp.Application { wire.Build(baseapp.Set); return nil }
`)
				env := append(os.Environ(), "GOWORK="+work, "WIRE_CACHE_DIR="+filepath.Join(root, "cache"), "WIRE_LOADER_ARTIFACTS=1", "WIRE_OUTPUT_CACHE=1")
				wd := filepath.Join(root, "tooling")
				patterns := []string{"example.com/baseapp", "example.com/app"}
				generate := func() []GenerateResult {
					t.Helper()
					gens, errs := Generate(context.Background(), wd, env, patterns, &GenerateOptions{})
					if len(errs) != 0 {
						t.Fatalf("Generate: %v", errs)
					}
					if len(gens) != 2 {
						t.Fatalf("generated %d targets, want 2", len(gens))
					}
					for _, gen := range gens {
						if len(gen.Errs) != 0 {
							t.Fatalf("Generate %s: %v", gen.PkgPath, gen.Errs)
						}
						if err := gen.Commit(); err != nil {
							t.Fatal(err)
						}
					}
					return gens
				}
				// Generated files change discovery once, so warm beyond their creation.
				for i := 0; i < 3; i++ {
					generate()
				}
				_, _, _, hit := prepareGenerateOutputCache(context.Background(), wd, env, patterns, &GenerateOptions{})
				if !hit {
					t.Fatal("output cache was not warm before the provider edit")
				}
				write("baseapp/provider.go", `package baseapp
import "github.com/goforj/wire"
// Application is shared by both injectors.
type Application struct{}
// NewApplication no longer requires configuration.
func NewApplication() *Application { return &Application{} }
// Set supplies the application after removing configuration.
var Set = wire.NewSet(NewApplication)
`)
				for _, gen := range generate() {
					if strings.Contains(string(gen.Content), "Options") {
						t.Fatalf("%s retained the deleted Options type:\n%s", gen.PkgPath, gen.Content)
					}
				}
				cmd := exec.Command("go", "test", patterns[0], patterns[1])
				cmd.Dir, cmd.Env = wd, env
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("generated code does not compile: %v\n%s", err, output)
				}
				// A warm successful result must not hide a new unsatisfied dependency.
				for i := 0; i < 3; i++ {
					generate()
				}
				_, _, _, hit = prepareGenerateOutputCache(context.Background(), wd, env, patterns, &GenerateOptions{})
				if !hit {
					t.Fatal("output cache was not warm before introducing the missing dependency")
				}
				write("baseapp/provider.go", `package baseapp
import "github.com/goforj/wire"
// Application is shared by both injectors.
type Application struct{}
// NewApplication now requires an unprovided dependency.
func NewApplication(string) *Application { return &Application{} }
// Set deliberately omits the string provider.
var Set = wire.NewSet(NewApplication)
`)
				gens, errs := Generate(context.Background(), wd, env, patterns, &GenerateOptions{})
				messages := fmt.Sprint(errs)
				for _, gen := range gens {
					messages += fmt.Sprint(gen.Errs)
				}
				if !strings.Contains(messages, "no provider found for string") {
					t.Fatalf("expected the new missing dependency to be reported, got: %s", messages)
				}
			})
		}
	}
}
