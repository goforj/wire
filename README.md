<p align="center">
  <img src="https://raw.githubusercontent.com/goforj/wire/main/docs/assets/logo-v2.png" width="300" alt="goforj/wire logo">
</p>

<p align="center">
    Compile-time dependency injection for Go - fast, explicit, and reflection-free.
</p>

<p align="center">
    <a href="https://pkg.go.dev/github.com/goforj/wire"><img src="https://pkg.go.dev/badge/github.com/goforj/wire.svg" alt="Go Reference"></a>
    <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache2-blue.svg" alt="License"></a>
    <a href="https://github.com/goforj/wire/actions"><img src="https://github.com/goforj/wire/actions/workflows/tests.yml/badge.svg" alt="Go Test"></a>
    <a href="https://golang.org"><img src="https://img.shields.io/badge/go-1.25+-blue?logo=go" alt="Go version"></a>
    <img src="https://img.shields.io/github/v/tag/goforj/wire?label=version&sort=semver" alt="Latest tag">
    <a href="https://codecov.io/gh/goforj/wire" ><img src="https://codecov.io/github/goforj/wire/graph/badge.svg?token=T7REZRC0G0"/></a>
</p>

<p align="center">
  <code>wire</code> generates plain Go code to wire your application together.
  No runtime container, no reflection, no hidden magic - just fast, explicit initialization.
</p>

**GoForj Wire is a maintained fork of [Google Wire](https://github.com/google/wire),
which is archived and no longer maintained.** Keep explicit constructor injection
and ordinary generated Go code, with incremental generation, a native watcher,
and project-owned tooling for modern Go workspaces. No GoForj framework dependency
is required.

Wire is a code generation tool that automates connecting components using
[dependency injection][]. Dependencies between components are represented in
Wire as function parameters, encouraging explicit initialization instead of
global variables. Because Wire operates without runtime state or reflection,
code written to be used with Wire is useful even for hand-written
initialization.

For an overview, see the [introductory blog post](https://blog.golang.org/wire).

[dependency injection]: https://en.wikipedia.org/wiki/Dependency_injection
[godoc]: https://godoc.org/github.com/goforj/wire

## Why GoForj Wire?

The improvements are in the generator and development workflow. Providers,
`wire.Build`, provider sets, interface bindings, and cleanup functions retain
Wire's familiar model.

| Improvement over Google Wire | What it gives you |
| --- | --- |
| **Incremental generation** | Reuse package-loading and generated-output caches on repeated runs, reducing the wait as your dependency graph grows. See the [measured comparison](#generation-performance). |
| **Native watch mode** | Run `wire watch ./...` once to regenerate as Go files change, with filesystem notifications and a polling fallback. |
| **Project-owned generation commands** | Use `-generate_command="go tool wire"` to keep generated directives on the tool version declared by your project. This also avoids the default command's `-mod=mod` restriction in Go workspaces. |
| **Workspace cache correctness** | Provider signature changes in sibling `go.work` modules and local replacements invalidate cached output, including when generation runs from a separate tooling module. Fixed in [v1.3.0](https://github.com/goforj/wire/releases/tag/v1.3.0). |
| **Cache and profiling controls** | Inspect or clear Wire caches with `wire cache` and `wire cache clear`; use `-timings`, `-cpuprofile`, `-memprofile`, and `-trace` to investigate generation costs. |
| **Ongoing maintenance** | Updated Go tooling dependencies, regression tests, and releases, while keeping the provider API small. |

Generated injectors still call constructors directly. These improvements reduce
generation work; they do not add a runtime container or change how your
application resolves dependencies.

### Generation performance

The repository includes a reproducible comparison against Google Wire at
[`9c25c90`](https://github.com/google/wire/commit/9c25c9016f6825302537c4efdd5e897976f9c826).
One run of the synthetic import-scale benchmark on macOS ARM64 with Go 1.25.0,
comparing GoForj Wire v1.3.0, produced:

| Imported fixture packages | Google Wire | GoForj initial series | GoForj unchanged | Unchanged speedup vs. Google Wire |
| ---: | ---: | ---: | ---: | ---: |
| 10 | 21.2 ms | 16.9 ms | 3.7 ms | 5.69x |
| 100 | 34.1 ms | 34.4 ms | 9.1 ms | 3.75x |
| 1,000 | 180.8 ms | 193.8 ms | 71.9 ms | 2.52x |

Each value is the median of three generator invocations. The benchmark runs
`go list -deps` before each sample; the unchanged series also runs the generator
once before each timed invocation. The initial series is labeled `cold` by the
script, but reuses caches across its three trials, so it is not a fully cold-cache
comparison. Timings exclude building the generator binaries.

The main benefit in this run is repeated generation. Initial runs can be slower,
and results depend on graph shape, toolchain, hardware, and cache state. These
numbers measure code generation, not application compilation or runtime speed.

To reproduce from a repository checkout with Git history:

```sh
bash scripts/import-benchmarks.sh table
```

The script prints the comparison table above with timings from your machine.
For edits that change bodies, signatures, or imports, run
`bash scripts/import-benchmarks.sh scenarios` to compare those paths separately.

## Installing

**Go 1.25 or later is required for v1.3.0.** Update local and CI toolchains
before upgrading from an older release.

```sh
go install github.com/goforj/wire/cmd/wire@latest
```

Ensure `$GOPATH/bin` is in your `$PATH`.

### Project-owned tools and workspaces

To manage Wire as a project tool dependency, add it to the module and opt into
the tool-based generation command:

```sh
go get -tool github.com/goforj/wire/cmd/wire@v1.3.0
go tool wire gen -generate_command="go tool wire" ./...
```

Wire then emits a directive that keeps this choice on subsequent runs:

```go
//go:generate go tool wire gen -generate_command "go tool wire"
```

This works with `go generate` in a Go workspace without the default command's
`-mod=mod` flag. The project must declare the Wire tool dependency. The tool
directive was introduced in Go 1.24; Wire v1.3.0 requires Go 1.25 or later.

`-generate_command` is available on `gen`, `diff`, and `watch`. Its value is the
command prefix that invokes Wire; Wire appends `gen`, the override itself, and
any build tags. Custom wrappers must forward those arguments. Commands follow
Go generate's argument rules, including Go-style double-quoted arguments and
environment-variable expansion. They are not interpreted by a shell.

Omitting the flag preserves the existing `go run -mod=mod` directive and generated
output. To restore that default, run `wire gen` without the override.

## Moving from Google Wire

You can adopt this generator without rewriting provider sets or injector stubs.
It recognizes both `github.com/google/wire` and `github.com/goforj/wire` imports.
Keep your existing `github.com/google/wire` module requirement if your source
still imports it; no module replacement is needed to use the fork's generator.

Install the fork, then run it in your application's module:

```sh
go install github.com/goforj/wire/cmd/wire@v1.3.0
wire gen ./...
```

Review and commit the regenerated files. The default `//go:generate` directive
now invokes `github.com/goforj/wire/cmd/wire`; generated files will therefore
change even when the provider graph stays the same. For a pinned, project-owned
tool or a `go.work` workspace, use the [tool dependency setup](#project-owned-tools-and-workspaces)
instead.

New projects can import `github.com/goforj/wire` directly. The provider API stays
familiar, and generated application code has no runtime dependency on Wire.
The fork's Go 1.25 minimum applies to its module and generator; it does not
require adding a runtime dependency to generated applications.

## How Wire Works (in 60 seconds)

Wire is a **compile-time dependency injection** tool. Instead of building a runtime
container, Wire generates ordinary Go code that explicitly initializes your
application.

There are only two concepts to learn:

### Providers

A provider is a normal Go function that produces a value (and optionally an error).

```go
func NewDB(cfg Config) (*sql.DB, error) {
    // ...
}
```

Dependencies are expressed as function parameters - no globals, no hidden state.

### Injectors

An injector is a stub function whose body calls `wire.Build(...)`.
Wire replaces this stub with generated code that calls providers in dependency
order.

```go
func InitializeApp(cfg Config) (*App, error) {
    wire.Build(AppSet)
    return nil, nil
}
```

The generated output is plain Go - readable, debuggable, and fast.

## Minimal Example

```go
// providers.go
type Message string

func ProvideMessage() Message {
    return "hello wire"
}

type Greeter struct {
    Msg Message
}

func NewGreeter(msg Message) *Greeter {
    return &Greeter{Msg: msg}
}

var GreeterSet = wire.NewSet(
    ProvideMessage,
    NewGreeter,
)
```

```go
// wire.go
//go:build wireinject

func InitializeGreeter() *Greeter {
    wire.Build(GreeterSet)
    return nil
}
```

Generate the injector:

```sh
wire
```

Wire produces `wire_gen.go` containing explicit initialization code with no
runtime dependency on Wire.

## Running generation manually

Use `make generate` to run `wire gen` across the repository. The CLI remains useful for targeted generation:

```sh
wire
wire gen ./...
```

## Development

Use `make test` for the test suite and `make vet` for static checks.

## Watching for changes

Wire includes a native watcher that re-runs generation on Go file changes:

```sh
wire watch
wire watch ./...
```

Detects the package root automatically and uses native filesystem notifications when available (with a polling fallback).

## Design Guidance

Wire favors explicitness and long-term maintainability:

* Prefer **small provider sets**, especially in libraries
* Avoid injecting common types like `string`; define distinct types instead
* Treat provider sets as **public APIs** with compatibility guarantees
* Use struct providers and interface bindings to keep graphs readable

For detailed guidance, see **Best Practices**.

## Documentation

If you're new to Wire, we recommend reading in this order:

1. **Tutorial** - A guided introduction with a complete working example
2. **User Guide** - Full reference for providers, injectors, bindings, and advanced patterns
3. **Best Practices** - Design guidance for large codebases and libraries
4. **FAQ** - Design rationale and common questions

* [Tutorial](./_tutorial/README.md)
* [User Guide](./docs/guide.md)
* [Best Practices](./docs/best-practices.md)
* [FAQ](./docs/faq.md)

## When Not to Use Wire

Wire is designed for medium-to-large Go applications with non-trivial
initialization logic.

For small programs with only a handful of dependencies, hand-written
initialization is often simpler and clearer.

## Project Status

This fork maintains Google Wire's provider model while improving generation
performance, workspace support, and developer tooling. Compatibility with existing
provider definitions is a core goal. See the [release notes](https://github.com/goforj/wire/releases)
for shipped changes and toolchain requirements.

## Community

For questions and discussion, use
[GitHub Discussions](https://github.com/goforj/wire/discussions).

This project follows the Go
[Code of Conduct](./CODE_OF_CONDUCT.md).
