# Wire v1.3.0 performance

For the two projects with 342 external packages, v1.3.0 was 84-109x faster than
Google Wire on unchanged runs and 18-19x faster after a function body edit. With
1,016 local packages and few external dependencies, an unchanged run was 9x
faster. These are generation times for test projects, not application runtime
benchmarks or a promise for every codebase.

Compared directly with v1.2.0, no single-module case took more than 5% longer
in v1.3.0 across Go 1.25.0, 1.26.3, and 1.27.2. Some cases were faster.
Workspaces have one extra cost:
Wire now checks sibling source files that the old cache could miss. The largest
workspace measured here added 2.9 ms in default mode and 10.5 ms with opt-in
content hashing. That extra work fixes stale constructor calls.

## Test setup

Measured on an Apple M4 Max with 48 GiB RAM and macOS 26.5.2. Generator processes
used `GOMAXPROCS=4`, and Go builds used `-p=4`. Runs were serial, and version
order was rotated or alternated to reduce ordering effects. This was an active workstation;
small differences can come from other activity. Treat small percentage changes
as approximate with five samples per case.

The compared sources were:

- Google Wire: [`9c25c90`](https://github.com/google/wire/commit/9c25c9016f6825302537c4efdd5e897976f9c826).
- GoForj Wire v1.2.0: [`a64b614`](https://github.com/goforj/wire/commit/a64b6147462d563640d769c527176ed3edf14d05).
- GoForj Wire v1.3.0: [`2367f18`](https://github.com/goforj/wire/commit/2367f18afe9990c379d49864e1efc8be85ed5c65).

The project sources come from the factories in
[`internal/wire/import_bench_test.go`](../internal/wire/import_bench_test.go).
Each has ten application features and ordinary constructors. The external
profiles import a mix of database, HTTP, Redis, and other library packages.

| Project | Local packages | Standard library packages | External packages |
| --- | ---: | ---: | ---: |
| local | 41 | 185 | 1 |
| local-high | 1,016 | 185 | 1 |
| external-low | 42 | 240 | 342 |
| external-high | 117 | 240 | 342 |

Counts above use Go 1.25.0 and cover packages reachable from the injector
with the `wireinject` tag. The external count includes Wire's marker package.
Both GoForj versions used the same project files and marker-package checkout.
Google Wire used the same factory inputs with its own import path. Local
replacements selected the marker packages; the generator binaries were built
from the exact source commits listed above. This isolates the generator release
comparison from changes to the application.

The main comparison used five samples for each case, with both `mtime` (default)
and `content` cache modes. The newer-Go checks repeated unchanged, body-edit, and
constructor-edit cases on the two larger projects. In total, the comparisons
below contain **932 timed generator invocations**. Untimed setup and warmup runs
are additional.

Building the generator and downloading modules happened before timing. For the
main comparison, `go list -export -deps -tags=wireinject` also populated Go's build
cache before timing. A fresh Wire cache therefore measures Wire startup and
cache creation without charging it for compiling all dependency export data.
Cold Go caches are measured separately below.

Before measuring an unchanged fork run, the audit verified a generated-output
cache hit. Edit cases warmed the base state, changed one file, and then timed
regeneration. The first full pass compiled the generated projects for every
version, mode, project, and edit case. Later timing samples repeated the edits
without repeating those compilation checks.

## Google Wire versus GoForj Wire

Go 1.25.0, default cache mode, five-run medians. All times are milliseconds.
A fresh Wire cache starts with Go's build cache warm; subsequent rows start from
a warmed Wire cache.

| Project | Change | Google Wire | GoForj v1.3.0 | Speedup |
| --- | --- | ---: | ---: | ---: |
| external-high | Fresh Wire cache | 1,600.3 | 1,571.7 | 1.02x |
| external-high | Unchanged | 1,582.0 | 18.9 | 83.71x |
| external-high | Function body edit | 1,580.0 | 87.3 | 18.11x |
| external-high | Constructor argument added | 1,604.8 | 262.6 | 6.11x |
| external-high | Import added | 1,567.2 | 267.8 | 5.85x |
| external-high | Return to previous imports | 1,573.3 | 275.0 | 5.72x |
| external-low | Fresh Wire cache | 1,626.6 | 1,540.9 | 1.06x |
| external-low | Unchanged | 1,581.0 | 14.5 | 109.33x |
| external-low | Function body edit | 1,579.9 | 81.4 | 19.41x |
| external-low | Constructor argument added | 1,580.2 | 242.3 | 6.52x |
| external-low | Import added | 1,565.3 | 243.1 | 6.44x |
| external-low | Return to previous imports | 1,564.6 | 244.2 | 6.41x |
| local-high | Fresh Wire cache | 558.0 | 713.8 | 0.78x |
| local-high | Unchanged | 544.1 | 60.2 | 9.03x |
| local-high | Function body edit | 543.3 | 132.7 | 4.09x |
| local-high | Constructor argument added | 535.0 | 407.3 | 1.31x |
| local-high | Import added | 521.9 | 408.3 | 1.28x |
| local-high | Return to previous imports | 537.9 | 413.5 | 1.30x |
| local | Fresh Wire cache | 401.7 | 378.1 | 1.06x |
| local | Unchanged | 394.7 | 9.3 | 42.47x |
| local | Function body edit | 391.7 | 26.8 | 14.59x |
| local | Constructor argument added | 402.6 | 100.9 | 3.99x |
| local | Import added | 398.1 | 99.9 | 3.98x |
| local | Return to previous imports | 396.0 | 102.0 | 3.88x |

The 1,016-local-package project is a useful limit on the claim. Its first run
with a fresh Wire cache was slower than Google Wire. Reruns were faster, but
constructor and import edits had much smaller gains than unchanged runs.

## v1.3.0 versus v1.2.0

Go 1.25.0, five-run medians. Positive changes mean v1.3.0 took longer. Both
versions used identical project inputs and started each case with the same kind
of cache state. Small differences should be read alongside their absolute cost.

### Default cache mode

| Project | Change | v1.2.0 (ms) | v1.3.0 (ms) | Change |
| --- | --- | ---: | ---: | ---: |
| external-high | Fresh Wire cache | 1,550.6 | 1,571.7 | +1.4% |
| external-high | Unchanged | 18.4 | 18.9 | +2.8% |
| external-high | Function body edit | 87.5 | 87.3 | -0.2% |
| external-high | Constructor argument added | 258.3 | 262.6 | +1.6% |
| external-high | Import added | 269.8 | 267.8 | -0.8% |
| external-high | Return to previous imports | 270.4 | 275.0 | +1.7% |
| external-low | Fresh Wire cache | 1,523.7 | 1,540.9 | +1.1% |
| external-low | Unchanged | 14.7 | 14.5 | -1.3% |
| external-low | Function body edit | 80.2 | 81.4 | +1.6% |
| external-low | Constructor argument added | 241.3 | 242.3 | +0.4% |
| external-low | Import added | 247.2 | 243.1 | -1.6% |
| external-low | Return to previous imports | 244.5 | 244.2 | -0.1% |
| local-high | Fresh Wire cache | 737.1 | 713.8 | -3.2% |
| local-high | Unchanged | 63.1 | 60.2 | -4.5% |
| local-high | Function body edit | 135.8 | 132.7 | -2.3% |
| local-high | Constructor argument added | 420.1 | 407.3 | -3.1% |
| local-high | Import added | 408.6 | 408.3 | -0.1% |
| local-high | Return to previous imports | 414.6 | 413.5 | -0.3% |
| local | Fresh Wire cache | 379.7 | 378.1 | -0.4% |
| local | Unchanged | 9.1 | 9.3 | +2.2% |
| local | Function body edit | 27.4 | 26.8 | -1.9% |
| local | Constructor argument added | 99.9 | 100.9 | +1.0% |
| local | Import added | 103.0 | 99.9 | -3.0% |
| local | Return to previous imports | 101.3 | 102.0 | +0.7% |

### Content-hash cache mode

| Project | Change | v1.2.0 (ms) | v1.3.0 (ms) | Change |
| --- | --- | ---: | ---: | ---: |
| external-high | Fresh Wire cache | 1,884.4 | 1,853.2 | -1.7% |
| external-high | Unchanged | 20.0 | 20.0 | -0.0% |
| external-high | Function body edit | 148.3 | 142.1 | -4.2% |
| external-high | Constructor argument added | 314.6 | 313.3 | -0.4% |
| external-high | Import added | 326.2 | 320.1 | -1.9% |
| external-high | Return to previous imports | 197.8 | 201.1 | +1.7% |
| external-low | Fresh Wire cache | 1,812.7 | 1,838.4 | +1.4% |
| external-low | Unchanged | 14.9 | 15.2 | +1.6% |
| external-low | Function body edit | 136.3 | 135.3 | -0.8% |
| external-low | Constructor argument added | 299.2 | 299.3 | +0.1% |
| external-low | Import added | 301.4 | 300.5 | -0.3% |
| external-low | Return to previous imports | 181.1 | 178.0 | -1.7% |
| local-high | Fresh Wire cache | 793.0 | 802.4 | +1.2% |
| local-high | Unchanged | 70.8 | 71.3 | +0.8% |
| local-high | Function body edit | 167.7 | 171.9 | +2.5% |
| local-high | Constructor argument added | 430.8 | 439.9 | +2.1% |
| local-high | Import added | 432.1 | 439.4 | +1.7% |
| local-high | Return to previous imports | 329.7 | 338.8 | +2.8% |
| local | Fresh Wire cache | 448.7 | 445.4 | -0.7% |
| local | Unchanged | 9.7 | 10.1 | +4.6% |
| local | Function body edit | 42.8 | 41.9 | -2.2% |
| local | Constructor argument added | 115.1 | 116.1 | +0.8% |
| local | Import added | 116.1 | 118.4 | +2.0% |
| local | Return to previous imports | 83.2 | 85.6 | +2.8% |

The main matrix has 600 timings and 100 successful generation/compilation checks.
Its raw samples are in [release-comparison-go1.25.csv](benchmarks/release-comparison-go1.25.csv).

## Go 1.26 and Go 1.27

The two larger projects were also checked with both releases built using Go
1.26.3 and Go 1.27.2. Each row is the median of five samples. These runs covered
both cache modes and added 240 timings and 48 compilation checks.

| Go | Project | Cache mode | Change | v1.2.0 (ms) | v1.3.0 (ms) | Change |
| --- | --- | --- | --- | ---: | ---: | ---: |
| 1.26.3 | local-high | mtime | Unchanged | 62.6 | 65.0 | +3.8% |
| 1.26.3 | local-high | mtime | Function body edit | 140.0 | 140.1 | +0.1% |
| 1.26.3 | local-high | mtime | Constructor argument added | 423.1 | 419.3 | -0.9% |
| 1.26.3 | local-high | content | Unchanged | 73.3 | 74.4 | +1.6% |
| 1.26.3 | local-high | content | Function body edit | 173.4 | 173.7 | +0.2% |
| 1.26.3 | local-high | content | Constructor argument added | 443.1 | 448.7 | +1.3% |
| 1.26.3 | external-high | mtime | Unchanged | 19.6 | 19.2 | -2.0% |
| 1.26.3 | external-high | mtime | Function body edit | 89.7 | 87.4 | -2.6% |
| 1.26.3 | external-high | mtime | Constructor argument added | 268.7 | 274.6 | +2.2% |
| 1.26.3 | external-high | content | Unchanged | 20.6 | 20.5 | -0.5% |
| 1.26.3 | external-high | content | Function body edit | 151.4 | 150.0 | -0.9% |
| 1.26.3 | external-high | content | Constructor argument added | 327.0 | 327.7 | +0.2% |
| 1.27.2 | local-high | mtime | Unchanged | 61.4 | 63.0 | +2.7% |
| 1.27.2 | local-high | mtime | Function body edit | 141.1 | 139.3 | -1.2% |
| 1.27.2 | local-high | mtime | Constructor argument added | 413.2 | 421.2 | +1.9% |
| 1.27.2 | local-high | content | Unchanged | 71.4 | 71.5 | +0.1% |
| 1.27.2 | local-high | content | Function body edit | 186.5 | 178.9 | -4.1% |
| 1.27.2 | local-high | content | Constructor argument added | 449.6 | 439.7 | -2.2% |
| 1.27.2 | external-high | mtime | Unchanged | 20.4 | 21.4 | +5.0% |
| 1.27.2 | external-high | mtime | Function body edit | 95.2 | 93.0 | -2.3% |
| 1.27.2 | external-high | mtime | Constructor argument added | 308.4 | 305.1 | -1.1% |
| 1.27.2 | external-high | content | Unchanged | 22.7 | 21.0 | -7.5% |
| 1.27.2 | external-high | content | Function body edit | 150.9 | 153.8 | +1.9% |
| 1.27.2 | external-high | content | Constructor argument added | 352.2 | 358.9 | +1.9% |

Raw samples: [newer-go-comparison.csv](benchmarks/newer-go-comparison.csv).

## Fully cold Go caches

These are first generation runs with a fresh Go build cache and a fresh Wire
cache. Module downloads and the generator build are still excluded. Each value
is the median of three samples, with version order alternated. Go 1.25.0,
default cache mode.

| Project | v1.2.0 | v1.3.0 | Change |
| --- | ---: | ---: | ---: |
| local-high | 7.28 s | 7.44 s | +2.1% |
| external-high | 10.16 s | 10.22 s | +0.6% |

Cold starts are a real cost. The repository's separate three-sample scenario
runner measured Google Wire at 0.75 s versus GoForj at 7.27 s for `local-high`,
and 2.36 s versus 10.28 s for `external-high`, with fresh Go/Wire caches. The
release comparison above shows that the cold-start cost was already present
in v1.2.0. The large speedups apply after caches are populated.

Raw samples: [cold-go-cache.csv](benchmarks/cold-go-cache.csv).

## Generation from a separate tools module

The same two projects were measured from their own module and from a separate
tools module in a `go.work` workspace. Before timing, the audit warmed the cache
and checked for an output-cache hit. Each row has five samples, with compilation
checked on the first pass. Go 1.25.0. These checks added 80 timings and 16
compilation checks.

| Project | Cache mode | Run from | v1.2.0 (ms) | v1.3.0 (ms) | Change |
| --- | --- | --- | ---: | ---: | ---: |
| local-high | mtime | module | 60.9 | 60.9 | +0.0% |
| local-high | mtime | workspace | 59.9 | 62.9 | +4.9% |
| local-high | content | module | 72.7 | 70.8 | -2.5% |
| local-high | content | workspace | 62.1 | 72.6 | +16.9% |
| external-high | mtime | module | 19.3 | 18.7 | -3.3% |
| external-high | mtime | workspace | 18.4 | 19.1 | +3.5% |
| external-high | content | module | 19.6 | 19.9 | +1.6% |
| external-high | content | workspace | 18.7 | 21.1 | +12.9% |

For `local-high`, default workspace reruns increased by 2.9 ms. With content
hashing they increased by 10.5 ms, about 17%. The smaller set of local files in
`external-high` added 0.6 ms and 2.4 ms respectively.

The extra work matters for correctness. v1.2.0 could reuse old output after a
constructor changed in another workspace module, leaving code that no longer
compiled. v1.3.0 checks those files before reusing its output. Content hashing
reads their contents on each run, so it pays more than the default file-metadata
check. These are measured costs, not a claim of zero overhead.

Raw samples: [workspace-comparison.csv](benchmarks/workspace-comparison.csv).

## Correctness checks

- All 164 generation/compilation checks in the comparisons passed.
- The full repository tests passed on Go 1.25.0 with the race detector, and on Go 1.26.3 and 1.27.2.
- `go vet ./...` passed on Go 1.25.0.
- The published v1.3.0 tool was installed through the Go module proxy without a local Wire replacement. Both cache modes regenerated and compiled two workspace targets after removing a constructor argument and its options type.
- The workspace regression test also checks that a newly missing dependency produces an error after a successful cache warmup.

See [`workspace_cache_test.go`](../internal/wire/workspace_cache_test.go) for the
workspace/local-replacement correctness checks. The ordinary module benchmarks
used `GOWORK=off`; only the workspace-specific checks enabled a workspace.

## Running the repository benchmarks

For the application-shaped inputs:

```sh
bash scripts/import-benchmarks.sh scenarios
```

For the small import-only inputs:

```sh
bash scripts/import-benchmarks.sh table
```

The import-only test uses small constructors with few standard library or
external imports. Its 2-6x speedups measure a different kind of input from the
application tests above. Package count alone does not tell you how much work a
cache hit saves.

The checked-in scenario runner reports three-sample medians and includes fresh
Go build caches for its cold-run row. The five-sample release comparison in this
report adds an explicit export-data warmup, rotates release order, checks both
cache modes, and compiles edited outputs. Its `Return to previous imports` row
removes the import added by the preceding step; the repository runner's `known
import toggle` row instead returns from a constructor/import shape change. Those
rows should not be treated as identical tests.

To repeat a comparison between releases, use the same generated project files,
Go version, thread limit, and cache settings for both binaries. Measure fresh
Wire caches separately from fresh Go build caches. Warm past the creation of
`wire_gen.go` before timing unchanged runs, and compile the output after changing
constructors. A fast result that leaves stale code is not a successful run.
