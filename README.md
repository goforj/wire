# Wire: Automated Initialization in Go

[![Build Status](https://github.com/goforj/wire/actions/workflows/tests.yml/badge.svg?branch=main)](https://github.com/goforj/wire/actions)
[![godoc](https://godoc.org/github.com/goforj/wire?status.svg)][godoc]

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
