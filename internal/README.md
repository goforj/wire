# Internal Package Guide

This directory holds the implementation behind the `wire` CLI and library.
If you are new to the codebase, the two packages that matter most are:

- `internal/wire`: parses injector/provider-set source and generates `wire_gen.go`
- `internal/loader`: loads package graphs, supports the custom loader, and manages loader-side caches

Everything else is either small support code or repo maintenance helpers.

## High-Level Flow

For `wire gen`, the rough flow is:

1. `cmd/wire` parses flags and calls `wire.Generate(...)`
2. `internal/wire` tries the output cache fast path
3. `internal/loader` loads the root graph and typed package graph
4. `internal/wire/parse.go` walks source/type information and builds Wire's internal model
5. `internal/wire/wire.go` generates formatted Go output
6. `cmd/wire` writes `wire_gen.go`

If you are debugging behavior, start in:

- [`wire/wire.go`](./wire/wire.go)
- [`wire/parse.go`](./wire/parse.go)
- [`loader/loader.go`](./loader/loader.go)
- [`loader/custom.go`](./loader/custom.go)

## Package Overview

### `internal/wire`

This is the core implementation of Wire's analysis and code generation.

Important responsibilities:

- loading packages through the loader abstraction
- parsing `wire.NewSet`, `wire.Bind`, `wire.Struct`, `wire.FieldsOf`, and injector functions
- building the internal provider graph model
- validating dependency graphs
- rendering generated Go source
- managing the output cache

Important files:

- [`wire/wire.go`](./wire/wire.go)
  Main generation entry point. Defines `Generate`, `GenerateResult`, and the final generation loop.
- [`wire/parse.go`](./wire/parse.go)
  The biggest conceptual file in the repo. Defines the internal model (`ProviderSet`, `Provider`, `Value`, `Field`, etc.) and parses source/type information into that model.
- [`wire/analyze.go`](./wire/analyze.go)
  Performs graph validation and dependency analysis once provider sets are parsed.
- [`wire/output_cache.go`](./wire/output_cache.go)
  Output cache read/write and key construction.
- [`wire/load_debug.go`](./wire/load_debug.go)
  Debug/timing summaries for loaded package graphs.
- [`wire/timing.go`](./wire/timing.go)
  Timing/debug plumbing for the `wire` package.
- [`wire/loader_timing_bridge.go`](./wire/loader_timing_bridge.go)
  Connects loader timing output into wire timing output.
- [`wire/errors.go`](./wire/errors.go)
  Error collection and formatting helpers.
- [`wire/copyast.go`](./wire/copyast.go)
  AST copying helpers used during generation.
- [`wire/loader_validation.go`](./wire/loader_validation.go)
  Loader-mode-related validation helpers.

Useful mental model:

- `parse.go` turns package syntax/types into a Wire-specific graph model
- `analyze.go` validates that graph
- `wire.go` emits Go code from that graph

### `internal/loader`

This package abstracts package loading and is the main performance-sensitive layer.

Important responsibilities:

- choosing between the custom loader and `go/packages` fallback
- root graph loading vs typed package loading
- discovery via `go list`
- discovery cache
- loader artifact cache
- touched-package validation
- loader timings and fallback reasons

Important files:

- [`loader/loader.go`](./loader/loader.go)
  Public loader API and shared request/result structs. This is the best entry point for understanding loader responsibilities.
- [`loader/custom.go`](./loader/custom.go)
  The custom loader implementation. This is the most performance-critical file in the repo.
- [`loader/fallback.go`](./loader/fallback.go)
  `go/packages` fallback implementation and fallback reason handling.
- [`loader/discovery.go`](./loader/discovery.go)
  Runs `go list`, decodes package metadata, and populates the discovery cache.
- [`loader/discovery_cache.go`](./loader/discovery_cache.go)
  Discovery cache storage and invalidation.
- [`loader/artifact_cache.go`](./loader/artifact_cache.go)
  Loader artifact cache keying and read/write helpers.
- [`loader/mode.go`](./loader/mode.go)
  Loader mode selection helpers.
- [`loader/timing.go`](./loader/timing.go)
  Timing/debug plumbing for the loader package.

Useful mental model:

- `discovery.go` answers "what packages/files/imports exist?"
- `custom.go` answers "how do we build the package graph and type info efficiently?"
- `artifact_cache.go` is the typed-package reuse layer
- `fallback.go` is the correctness backstop

### `internal/cachepaths`

Small shared helper package for Wire-managed cache directories.

Responsibilities:

- resolve the shared cache root
- resolve specific cache directories
- support shared and specific env var overrides

Important file:

- [`cachepaths/cachepaths.go`](./cachepaths/cachepaths.go)

This package exists to keep cache path policy centralized instead of duplicated across loader, output cache, and the `wire cache` command.

## Internal Data Structures

These are the main structs worth learning first.

### In `internal/wire`

- `ProviderSet`
  The central Wire model. Represents a set built from `wire.NewSet(...)` or `wire.Build(...)`.
- `Provider`
  A constructor source, either a function or a named struct provider.
- `IfaceBinding`
  A `wire.Bind(...)` relationship.
- `Value`
  A `wire.Value(...)` source.
- `Field`
  A `wire.FieldsOf(...)` source.
- `InjectorArgs` / `InjectorArg`
  Injector function argument modeling.

If you understand `ProviderSet`, most of `parse.go` becomes easier to follow.

### In `internal/loader`

- `RootLoadRequest` / `RootLoadResult`
  Used for lightweight root graph loading, primarily for cache lookup and root discovery.
- `PackageLoadRequest` / `PackageLoadResult`
  Used for full typed package loading.
- `LazyLoadRequest` / `LazyLoadResult`
  Used for package-targeted typed loading.
- `TouchedValidationRequest` / `TouchedValidationResult`
  Used to validate whether a touched local package can stay on the custom path.
- `DiscoverySnapshot`
  Captures `go list` metadata so later phases can reuse discovery without rerunning it.
- `packageMeta`
  Internal metadata from `go list`. This is the custom loader's raw package description.
- `customTypedGraphLoader`
  Main stateful custom loader for building typed package graphs.

## Empty or Transitional Directories

- `internal/cachestore`
- `internal/semanticcache`

These directories currently exist but do not contain active implementation files.
Treat them as inactive unless they are populated later.

## Repo Maintenance Files

There are also a few internal scripts/data files that support repo maintenance:

- [`alldeps`](./alldeps)
  Dependency allowlist/check input.
- [`runtests.sh`](./runtests.sh)
  Repo test runner used in CI/dev workflows.
- [`listdeps.sh`](./listdeps.sh)
  Dependency listing helper.
- [`check_api_change.sh`](./check_api_change.sh)
  API change check helper.

## Suggested Reading Order

If you are trying to understand the codebase quickly:

1. [`wire/wire.go`](./wire/wire.go)
2. [`loader/loader.go`](./loader/loader.go)
3. [`loader/custom.go`](./loader/custom.go)
4. [`wire/parse.go`](./wire/parse.go)
5. [`wire/analyze.go`](./wire/analyze.go)
6. [`wire/output_cache.go`](./wire/output_cache.go)
7. [`loader/discovery.go`](./loader/discovery.go)
8. [`loader/artifact_cache.go`](./loader/artifact_cache.go)

## Practical Notes For New Readers

- If a behavior issue involves parsing provider sets, start in `internal/wire/parse.go`.
- If a behavior issue involves performance, cache hits, or package loading, start in `internal/loader/custom.go`.
- If a behavior issue involves "why did we skip generation?" or "why did generation return instantly?", check `internal/wire/output_cache.go`.
- If a behavior issue only appears in one environment or workspace, inspect cache path resolution in `internal/cachepaths/cachepaths.go` and discovery behavior in `internal/loader/discovery.go`.

This repo has two real centers of complexity:

- the Wire semantic model in `internal/wire`
- the package loading/caching machinery in `internal/loader`

Most other internal code exists to support those two areas.
