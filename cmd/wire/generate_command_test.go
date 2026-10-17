package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestGenerateCommandFlags keeps the override available on every generating subcommand.
func TestGenerateCommandFlags(t *testing.T) {
	gen, diff, watch := &genCmd{}, &diffCmd{}, &watchCmd{}
	for _, test := range []struct {
		name string
		set  func(*flag.FlagSet)
		got  *string
	}{
		{"gen", gen.SetFlags, &gen.generateCommand},
		{"diff", diff.SetFlags, &diff.generateCommand},
		{"watch", watch.SetFlags, &watch.generateCommand},
	} {
		t.Run(test.name, func(t *testing.T) {
			flags := flag.NewFlagSet(test.name, flag.ContinueOnError)
			test.set(flags)
			if *test.got != "" {
				t.Fatalf("default command = %q", *test.got)
			}
			if err := flags.Parse([]string{"-generate_command", "go tool wire"}); err != nil {
				t.Fatal(err)
			}
			if *test.got != "go tool wire" {
				t.Fatalf("parsed command = %q", *test.got)
			}
		})
	}
}

// TestGenerateCommandRoundTrip exercises real go generate parsing, tool dependencies and output caching.
func TestGenerateCommandRoundTrip(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	repo, err = filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, command string
		workspace     bool
	}{
		{"module tool", "go tool wire", false},
		{"workspace tool", "go tool wire", true},
		{"workspace environment", "$WIRE_TEST_GO tool wire", true},
		{"workspace quoted path", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			app := filepath.Join(root, "app")
			if err := os.Mkdir(app, 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOWORK", "off")
			t.Setenv("WIRE_CACHE_DIR", filepath.Join(root, "wire-cache"))
			t.Setenv("WIRE_LOADER_ARTIFACTS", "1")
			t.Setenv("WIRE_OUTPUT_CACHE", "1")
			t.Setenv("WIRE_TEST_GO", goTool)
			files := map[string]string{
				"go.mod":       fmt.Sprintf("module example.com/generatecommand\n\ngo 1.25.0\n\ntool github.com/goforj/wire/cmd/wire\n\nrequire github.com/goforj/wire v0.0.0\n\nreplace github.com/goforj/wire => %s\n", strconv.Quote(filepath.ToSlash(repo))),
				"providers.go": "package fixture\n\n// Service is the fixture dependency.\ntype Service struct{}\n",
				"tagged.go":    "//go:build fixture\n\npackage fixture\n\n// newService requires the custom build tag to be preserved.\nfunc newService() *Service { return &Service{} }\n",
				"wire.go":      "//go:build wireinject\n\npackage fixture\n\nimport \"github.com/goforj/wire\"\n\n// InitializeService declares the fixture dependency graph.\nfunc InitializeService() *Service { wire.Build(newService); return nil }\n",
			}
			for name, content := range files {
				if err := os.WriteFile(filepath.Join(app, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			runGenerateFixture(t, app, "mod", "tidy")
			if test.workspace {
				work := filepath.Join(root, "go.work")
				if err := os.WriteFile(work, []byte("go 1.25.0\n\nuse ./app\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("GOWORK", work)
			}
			command := test.command
			if command == "" {
				wrapper := filepath.Join(root, "wire wrapper.go")
				content := "package main\n\nimport (\"os\"; \"os/exec\")\n\n// main forwards generator arguments to the project-owned Wire tool.\nfunc main() { cmd := exec.Command(os.Getenv(\"WIRE_TEST_GO\"), append([]string{\"tool\", \"wire\"}, os.Args[1:]...)...); cmd.Stdout = os.Stdout; cmd.Stderr = os.Stderr; if err := cmd.Run(); err != nil { os.Exit(1) } }\n"
				if err := os.WriteFile(wrapper, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
				command = "go run " + strconv.Quote(wrapper)
			}
			runGenerateFixture(t, app, "tool", "wire", "gen", "-tags", "fixture", "-generate_command", command)
			generated := filepath.Join(app, "wire_gen.go")
			before, err := os.ReadFile(generated)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(before), "//go:generate "+command+" gen -generate_command ") {
				t.Fatalf("custom directive missing:\n%s", before)
			}
			for range 2 {
				runGenerateFixture(t, app, "generate", "-tags", "fixture", ".")
				after, err := os.ReadFile(generated)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(before, after) {
					t.Fatalf("go generate changed output:\n%s", after)
				}
			}
			runGenerateFixture(t, app, "tool", "wire", "diff", "-tags", "fixture", "-generate_command", command)
			runGenerateFixture(t, app, "test", "-tags", "fixture", ".")
			runGenerateFixture(t, app, "tool", "wire", "gen", "-tags", "fixture")
			defaultOutput, err := os.ReadFile(generated)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(defaultOutput), "//go:generate go run -mod=mod github.com/goforj/wire/cmd/wire gen -tags \"fixture\"") {
				t.Fatalf("default directive missing:\n%s", defaultOutput)
			}
			runGenerateFixture(t, app, "tool", "wire", "gen", "-tags", "fixture", "-generate_command", command)
			after, err := os.ReadFile(generated)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("switching cached commands changed output: %v\n%s", err, after)
			}
		})
	}
}

// runGenerateFixture runs Go in a real fixture module so its directive parser is exercised.
func runGenerateFixture(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("go", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}
