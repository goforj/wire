<p align="center">
  <img src="docs/assets/logo.png" width="400" alt="goforj/wire logo">
</p>

<p align="center">
    Compile-time dependency injection for Go - fast, explicit, and reflection-free.
</p>

<p align="center">
    <a href="https://pkg.go.dev/github.com/goforj/wire"><img src="https://pkg.go.dev/badge/github.com/goforj/wire.svg" alt="Go Reference"></a>
    <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache2-blue.svg" alt="License"></a>
    <a href="https://github.com/goforj/wire/actions"><img src="https://github.com/goforj/wire/actions/workflows/test.yml/badge.svg" alt="Go Test"></a>
    <a href="https://golang.org"><img src="https://img.shields.io/badge/go-1.19+-blue?logo=go" alt="Go version"></a>
    <img src="https://img.shields.io/github/v/tag/goforj/wire?label=version&sort=semver" alt="Latest tag">
    <a href="https://codecov.io/gh/goforj/wire" ><img src="https://codecov.io/github/goforj/wire/graph/badge.svg?token=3KFTK96U8C"/></a>
    <a href="https://goreportcard.com/report/github.com/goforj/wire"><img src="https://goreportcard.com/badge/github.com/goforj/wire" alt="Go Report Card"></a>
</p>

<p align="center">
  <code>wire</code> generates plain Go code to wire your application together.
  No runtime container, no reflection, no hidden magic - just fast, explicit initialization.
</p>

> [!NOTE]
> This is a maintained fork of `google/wire`.
> Google no longer maintains the original project, and this fork focuses on
> compile-time performance, cacheability, and developer ergonomics while
> preserving wire's API and behavior for existing codebases. On real projects,
> compile time is typically 8–10x faster (often more) with the default cache.

Wire is a code generation tool that automates connecting components using
[dependency injection][]. Dependencies between components are represented in
Wire as function parameters, encouraging explicit initialization instead of
global variables. Because Wire operates without runtime state or reflection,
code written to be used with Wire is useful even for hand-written
initialization.

For an overview, see the [introductory blog post][].

[dependency injection]: https://en.wikipedia.org/wiki/Dependency_injection
[introductory blog post]: https://blog.golang.org/wire
[godoc]: https://godoc.org/github.com/goforj/wire
[travis]: https://travis-ci.com/google/wire

## Installing

Install Wire by running:

```shell
go install github.com/goforj/wire/cmd/wire@latest
```

and ensuring that `$GOPATH/bin` is added to your `$PATH`.

## Documentation

- [Tutorial][]
- [User Guide][]
- [Best Practices][]
- [FAQ][]

[Tutorial]: ./_tutorial/README.md
[Best Practices]: ./docs/best-practices.md
[FAQ]: ./docs/faq.md
[User Guide]: ./docs/guide.md

## Project status

This fork tracks the original Wire feature set and prioritizes performance and
workflow improvements. We aim to remain compatible with existing Wire
codebases while landing safe, measurable compile-time optimizations.

## Community

For questions, please use [GitHub Discussions](https://github.com/goforj/wire/discussions).

This project is covered by the Go [Code of Conduct][].

[Code of Conduct]: ./CODE_OF_CONDUCT.md
[go-cloud mailing list]: https://groups.google.com/forum/#!forum/go-cloud
