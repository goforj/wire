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

**GoForj Wire is a maintained fork of [Google Wire](https://github.com/google/wire).**
Google Wire is archived. This fork adds caching for faster repeat runs, watch
mode, and support for `go tool wire`. You can use it in any Go project.

Wire connects your components by generating the Go code that calls their
constructors. You write normal functions and declare which ones Wire can use.
Wire works out the call order and passes each dependency where it's needed.
The result is code you can read, debug, or write by hand.

For an overview, see the [introductory blog post](https://blog.golang.org/wire).

## Why GoForj Wire?

Your providers, `wire.Build` calls, provider sets, interface bindings, and
cleanup functions work the same way as in Google Wire. Here's what this fork adds:

| Feature | How it helps |
| --- | --- |
| **Faster repeat runs** | Wire caches package information and generated code so it can skip work on later runs. See the [benchmark results](#generation-performance). |
| **Watch mode** | Run `wire watch ./...` to regenerate when Go files change. It uses filesystem notifications, with polling as a fallback. |
| **Pin Wire in your project** | Use `-generate_command="go tool wire"` so `go generate` uses the Wire version in your `go.mod`. This also works in Go workspaces, where the default command's `-mod=mod` flag causes problems. |
| **Workspace fixes** | Change a constructor in another module, rerun Wire, and its generated calls update too. This works with `go.work` and local `replace` directives, even if you keep Wire in a separate tools module. Fixed in [v1.3.0](https://github.com/goforj/wire/releases/tag/v1.3.0). |
| **Cache and profiling tools** | Use `wire cache` to find the cache directory or `wire cache clear` to clear it. For slow runs, `-timings`, `-cpuprofile`, `-memprofile`, and `-trace` help you find where the time goes. |

The generated code still calls your constructors directly, with no runtime
container or reflection.

### Generation performance

We compared GoForj Wire v1.3.0 with Google Wire at
[`9c25c90`](https://github.com/google/wire/commit/9c25c9016f6825302537c4efdd5e897976f9c826)
using test projects with 10, 100, and 1,000 imported packages. These results are
from macOS ARM64 with Go 1.25.0:

| Imported packages | Google Wire | GoForj initial runs | GoForj warmed cache | Warmed cache speedup |
| ---: | ---: | ---: | ---: | ---: |
| 10 | 21.2 ms | 16.9 ms | 3.7 ms | 5.69x |
| 100 | 34.1 ms | 34.4 ms | 9.1 ms | 3.75x |
| 1,000 | 180.8 ms | 193.8 ms | 71.9 ms | 2.52x |

Each number is the median of three runs. The script runs `go list -deps` before
each measurement. For the warmed-cache results, it also runs Wire once before
timing it. The initial runs reuse caches across their three measurements, even
though the script labels them `cold`. They aren't a fully cold-cache test.
Building the generator binaries isn't included in the timings.

Repeat runs were faster in this test. The initial runs were slower at 1,000
packages. Your results will depend on your project, toolchain, hardware, and
cache state. These numbers time Wire, not `go build` or your running application.

Run the same comparison from a checkout with Git history:

```sh
bash scripts/import-benchmarks.sh table
```

The script prints a table with your timings. To compare what happens after
changing a function body, a constructor signature, or an import, run
`bash scripts/import-benchmarks.sh scenarios`.

## Installing

**Wire v1.3.0 requires Go 1.25 or later.** Check your local and CI Go versions
before upgrading.

```sh
go install github.com/goforj/wire/cmd/wire@latest
```

Ensure `$GOPATH/bin` is in your `$PATH`.

### Pinning Wire in your project

To pin Wire's version in your `go.mod`, add it as a Go tool:

```sh
go get -tool github.com/goforj/wire/cmd/wire@v1.3.0
go tool wire gen -generate_command="go tool wire" ./...
```

Wire adds this command to the generated file:

```go
//go:generate go tool wire gen -generate_command "go tool wire"
```

Now `go generate` uses `go tool wire` too. This works inside a `go.work` workspace,
where the default `go run -mod=mod` command would fail. Keep the tool dependency
in your `go.mod`. Wire v1.3.0 requires Go 1.25 or later.

You can use `-generate_command` with `gen`, `diff`, and `watch`. Wire adds `gen`,
the `-generate_command` flag, and any build tags to the command you supply. If
you use a wrapper script, make sure it forwards those arguments. Quoting and
environment variables follow `go generate` rules; shell operators aren't supported.

Without the flag, Wire generates its usual `go run -mod=mod` directive. Run
`wire gen` without the flag to switch back to that default.

## Moving from Google Wire

Keep your existing provider sets and injector stubs. The generator accepts both
`github.com/google/wire` and `github.com/goforj/wire` imports. If your code still
imports Google Wire, keep that dependency in your `go.mod`. You don't need a
`replace` directive to use this generator.

Install the fork, then run it in your application's module:

```sh
go install github.com/goforj/wire/cmd/wire@v1.3.0
wire gen ./...
```

Review and commit the regenerated files. Their `//go:generate` command now points
to `github.com/goforj/wire/cmd/wire`, so you'll see a diff even if your providers
haven't changed. To pin the generator version or use it in a `go.work` workspace,
follow [Pinning Wire in your project](#pinning-wire-in-your-project).

New projects can import `github.com/goforj/wire` directly. Its module and generator
require Go 1.25. The generated initialization code has no runtime dependency on
Wire.

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

Use `make generate` to run `wire gen` across this repository. In your own project, run:

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

Wire finds the package root automatically. It uses filesystem notifications when available and falls back to polling.

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

We maintain this fork so existing Wire projects can keep using it with current
Go tooling. See the [release notes](https://github.com/goforj/wire/releases) for
changes and required Go versions.

## Community

For questions and discussion, use
[GitHub Discussions](https://github.com/goforj/wire/discussions).

This project follows the Go
[Code of Conduct](./CODE_OF_CONDUCT.md).
