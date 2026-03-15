package wire

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

const (
	largeBenchmarkTestPackageCount = 24
	largeBenchmarkHelperCount      = 12
)

var largeBenchmarkSizes = []int{10, 100, 1000}

type incrementalScenarioBenchmarkCase struct {
	name    string
	mutate  func(tb testing.TB, root string)
	measure func(tb testing.TB, root string, env []string, ctx context.Context) incrementalScenarioTrace
	wantErr bool
}

type incrementalScenarioTrace struct {
	total  time.Duration
	labels map[string]time.Duration
}

type incrementalScenarioBudget struct {
	total            time.Duration
	validateLocal    time.Duration
	validateExt      time.Duration
	validateTouch    time.Duration
	validateTouchHit time.Duration
	outputs          time.Duration
	generateLoad     time.Duration
	localFastpath    time.Duration
}

type largeRepoPerformanceBudget struct {
	shapeTotal  time.Duration
	localLoad   time.Duration
	parse       time.Duration
	typecheck   time.Duration
	generate    time.Duration
	knownToggle time.Duration
}

func BenchmarkGenerateIncrementalFirstSeenShapeChange(b *testing.B) {
	cacheHooksMu.Lock()
	state := saveCacheHooks()
	b.Cleanup(func() {
		restoreCacheHooks(state)
		cacheHooksMu.Unlock()
	})

	repoRoot := benchmarkRepoRoot(b)

	for i := 0; i < b.N; i++ {
		cacheRoot := b.TempDir()
		osTempDir = func() string { return cacheRoot }

		root := b.TempDir()
		writeIncrementalBenchmarkModule(b, repoRoot, root)

		env := append(os.Environ(), "GOWORK=off")
		ctx := WithIncremental(context.Background(), true)

		if _, errs := Generate(ctx, root, env, []string{"./app"}, &GenerateOptions{}); len(errs) > 0 {
			b.Fatalf("baseline Generate returned errors: %v", errs)
		}

		writeBenchmarkFile(b, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
			"package dep",
			"",
			"type Foo struct { Message string; Count int }",
			"",
			"func NewMessage() string { return \"ok\" }",
			"",
			"func NewCount() int { return 7 }",
			"",
			"func New(msg string, count int) *Foo {",
			"\treturn &Foo{Message: msg, Count: count}",
			"}",
			"",
		}, "\n"))
		writeBenchmarkFile(b, filepath.Join(root, "dep", "wire.go"), strings.Join([]string{
			"package dep",
			"",
			"import \"github.com/goforj/wire\"",
			"",
			"var NewSet = wire.NewSet(NewMessage, NewCount, New)",
			"",
		}, "\n"))

		b.StartTimer()
		gens, errs := Generate(ctx, root, env, []string{"./app"}, &GenerateOptions{})
		b.StopTimer()
		if len(errs) > 0 {
			b.Fatalf("incremental shape-change Generate returned errors: %v", errs)
		}
		if len(gens) != 1 || len(gens[0].Errs) > 0 {
			b.Fatalf("unexpected Generate results: %+v", gens)
		}
	}
}

func BenchmarkGenerateIncrementalScenarioMatrix(b *testing.B) {
	cacheHooksMu.Lock()
	state := saveCacheHooks()
	b.Cleanup(func() {
		restoreCacheHooks(state)
		cacheHooksMu.Unlock()
	})

	repoRoot := benchmarkRepoRoot(b)
	for _, scenario := range incrementalScenarioBenchmarks() {
		scenario := scenario
		b.Run(scenario.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StartTimer()
				_ = measureIncrementalScenarioOnce(b, repoRoot, scenario)
				b.StopTimer()
			}
		})
	}
}

func TestPrintIncrementalScenarioBenchmarkTable(t *testing.T) {
	if os.Getenv("WIRE_BENCH_SCENARIOS") == "" {
		t.Skip("set WIRE_BENCH_SCENARIOS=1 to print the incremental scenario benchmark table")
	}

	cacheHooksMu.Lock()
	state := saveCacheHooks()
	t.Cleanup(func() {
		restoreCacheHooks(state)
		cacheHooksMu.Unlock()
	})

	repoRoot := benchmarkRepoRoot(t)
	rows := [][]string{{
		"scenario",
		"total",
		"local pkgs",
		"external",
		"touched",
		"touch hit",
		"outputs",
		"gen load",
		"local fastpath",
	}}
	for _, scenario := range incrementalScenarioBenchmarks() {
		trace := measureIncrementalScenarioOnce(t, repoRoot, scenario)
		rows = append(rows, []string{
			scenario.name,
			formatBenchmarkDuration(trace.total),
			formatBenchmarkDuration(trace.label("incremental.preload_manifest.validate_local_packages")),
			formatBenchmarkDuration(trace.label("incremental.preload_manifest.validate_external_files")),
			formatBenchmarkDuration(trace.label("incremental.preload_manifest.validate_touched")),
			formatBenchmarkDuration(trace.label("incremental.preload_manifest.validate_touched_cache_hit")),
			formatBenchmarkDuration(trace.label("incremental.preload_manifest.outputs")),
			formatBenchmarkDuration(trace.label("generate.load")),
			formatBenchmarkDuration(trace.label("incremental.local_fastpath.load")),
		})
	}
	fmt.Print(renderASCIITable(rows))
}

func TestIncrementalScenarioPerformanceBudgets(t *testing.T) {
	if os.Getenv("WIRE_PERF_BUDGETS") == "" {
		t.Skip("set WIRE_PERF_BUDGETS=1 to enforce incremental scenario performance budgets")
	}

	cacheHooksMu.Lock()
	state := saveCacheHooks()
	t.Cleanup(func() {
		restoreCacheHooks(state)
		cacheHooksMu.Unlock()
	})

	repoRoot := benchmarkRepoRoot(t)
	budgets := incrementalScenarioPerformanceBudgets()
	for _, scenario := range incrementalScenarioBenchmarks() {
		scenario := scenario
		budget, ok := budgets[scenario.name]
		if !ok {
			t.Fatalf("missing performance budget for scenario %q", scenario.name)
		}
		t.Run(scenario.name, func(t *testing.T) {
			trace := measureIncrementalScenarioMedian(t, repoRoot, scenario, 5)
			assertScenarioBudget(t, trace, budget)
		})
	}
}

func TestLargeRepoPerformanceBudgets(t *testing.T) {
	if os.Getenv("WIRE_PERF_BUDGETS") == "" {
		t.Skip("set WIRE_PERF_BUDGETS=1 to enforce incremental scenario performance budgets")
	}

	cacheHooksMu.Lock()
	state := saveCacheHooks()
	t.Cleanup(func() {
		restoreCacheHooks(state)
		cacheHooksMu.Unlock()
	})

	repoRoot := benchmarkRepoRoot(t)
	budgets := largeRepoPerformanceBudgets()
	for _, packageCount := range largeBenchmarkSizes {
		packageCount := packageCount
		budget, ok := budgets[packageCount]
		if !ok {
			t.Fatalf("missing large-repo performance budget for size %d", packageCount)
		}
		t.Run(strconv.Itoa(packageCount), func(t *testing.T) {
			trace := measureLargeRepoShapeChangeTraceMedian(t, repoRoot, packageCount, true, 3)
			checkBudgetDuration(t, "shape_total", trace.total, budget.shapeTotal)
			checkBudgetDuration(t, "local_fastpath_load", trace.label("incremental.local_fastpath.load"), budget.localLoad)
			checkBudgetDuration(t, "parse", trace.label("incremental.local_fastpath.parse"), budget.parse)
			checkBudgetDuration(t, "typecheck", trace.label("incremental.local_fastpath.typecheck"), budget.typecheck)
			checkBudgetDuration(t, "generate", trace.label("incremental.local_fastpath.generate"), budget.generate)

			knownToggle := measureLargeRepoKnownToggleMedian(t, repoRoot, packageCount, 3)
			checkBudgetDuration(t, "known_toggle", knownToggle, budget.knownToggle)
		})
	}
}

func BenchmarkGenerateLargeRepoNormalShapeChange(b *testing.B) {
	runLargeRepoShapeChangeBenchmarks(b, false)
}

func BenchmarkGenerateLargeRepoIncrementalShapeChange(b *testing.B) {
	runLargeRepoShapeChangeBenchmarks(b, true)
}

func TestPrintLargeRepoBenchmarkComparisonTable(t *testing.T) {
	if os.Getenv("WIRE_BENCH_TABLE") == "" {
		t.Skip("set WIRE_BENCH_TABLE=1 to print the large-repo benchmark comparison table")
	}

	cacheHooksMu.Lock()
	state := saveCacheHooks()
	t.Cleanup(func() {
		restoreCacheHooks(state)
		cacheHooksMu.Unlock()
	})

	repoRoot := benchmarkRepoRoot(t)
	rows := make([]largeRepoBenchmarkRow, 0, len(largeBenchmarkSizes))
	for _, packageCount := range largeBenchmarkSizes {
		coldNormal := measureLargeRepoColdOnce(t, repoRoot, packageCount, false)
		coldIncremental := measureLargeRepoColdOnce(t, repoRoot, packageCount, true)
		normal := measureLargeRepoShapeChangeOnce(t, repoRoot, packageCount, false)
		incremental := measureLargeRepoShapeChangeOnce(t, repoRoot, packageCount, true)
		knownToggle := measureLargeRepoKnownToggleOnce(t, repoRoot, packageCount)
		rows = append(rows, largeRepoBenchmarkRow{
			packageCount:    packageCount,
			coldNormal:      coldNormal,
			coldIncremental: coldIncremental,
			normal:          normal,
			incremental:     incremental,
			knownToggle:     knownToggle,
		})
	}

	table := [][]string{{
		"repo size",
		"cold old",
		"cold new",
		"cold delta",
		"shape old",
		"shape new",
		"shape delta",
		"known toggle",
		"cold speedup",
		"shape speedup",
	}}
	for _, row := range rows {
		table = append(table, []string{
			strconv.Itoa(row.packageCount),
			formatBenchmarkDuration(row.coldNormal),
			formatBenchmarkDuration(row.coldIncremental),
			formatPercentImprovement(row.coldNormal, row.coldIncremental),
			formatBenchmarkDuration(row.normal),
			formatBenchmarkDuration(row.incremental),
			formatPercentImprovement(row.normal, row.incremental),
			formatBenchmarkDuration(row.knownToggle),
			fmt.Sprintf("%.2fx", speedupRatio(row.coldNormal, row.coldIncremental)),
			fmt.Sprintf("%.2fx", speedupRatio(row.normal, row.incremental)),
		})
	}
	fmt.Print(renderASCIITable(table))
}

func TestPrintLargeRepoShapeChangeBreakdownTable(t *testing.T) {
	if os.Getenv("WIRE_BENCH_BREAKDOWN") == "" {
		t.Skip("set WIRE_BENCH_BREAKDOWN=1 to print the large-repo shape-change breakdown table")
	}

	cacheHooksMu.Lock()
	state := saveCacheHooks()
	t.Cleanup(func() {
		restoreCacheHooks(state)
		cacheHooksMu.Unlock()
	})

	repoRoot := benchmarkRepoRoot(t)
	rows := [][]string{{
		"repo size",
		"old total",
		"old base load",
		"old typed load",
		"new total",
		"new local load",
		"new parse",
		"new typecheck",
		"new injector solve",
		"new format",
		"new generate",
		"speedup",
	}}
	for _, packageCount := range largeBenchmarkSizes {
		normal := measureLargeRepoShapeChangeTraceOnce(t, repoRoot, packageCount, false)
		incremental := measureLargeRepoShapeChangeTraceOnce(t, repoRoot, packageCount, true)
		rows = append(rows, []string{
			strconv.Itoa(packageCount),
			formatBenchmarkDuration(normal.total),
			formatBenchmarkDuration(normal.label("load.packages.base.load")),
			formatBenchmarkDuration(normal.label("load.packages.lazy.load")),
			formatBenchmarkDuration(incremental.total),
			formatBenchmarkDuration(incremental.label("incremental.local_fastpath.load")),
			formatBenchmarkDuration(incremental.label("incremental.local_fastpath.parse")),
			formatBenchmarkDuration(incremental.label("incremental.local_fastpath.typecheck")),
			formatBenchmarkDuration(incremental.label("generate.package.example.com/app/app.injectors")),
			formatBenchmarkDuration(incremental.label("generate.package.example.com/app/app.format")),
			formatBenchmarkDuration(incremental.label("incremental.local_fastpath.generate")),
			fmt.Sprintf("%.2fx", speedupRatio(normal.total, incremental.total)),
		})
	}
	fmt.Print(renderASCIITable(rows))
}

func writeIncrementalBenchmarkModule(tb testing.TB, repoRoot string, root string) {
	tb.Helper()

	writeBenchmarkFile(tb, filepath.Join(root, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require github.com/goforj/wire v0.0.0",
		"replace github.com/goforj/wire => " + repoRoot,
		"",
	}, "\n"))

	writeBenchmarkFile(tb, filepath.Join(root, "app", "wire.go"), strings.Join([]string{
		"//go:build wireinject",
		"// +build wireinject",
		"",
		"package app",
		"",
		"import (",
		"\t\"example.com/app/dep\"",
		"\t\"github.com/goforj/wire\"",
		")",
		"",
		"func Init() *dep.Foo {",
		"\twire.Build(dep.NewSet)",
		"\treturn nil",
		"}",
		"",
	}, "\n"))

	writeBenchmarkFile(tb, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"type Foo struct { Message string }",
		"",
		"func NewMessage() string { return \"ok\" }",
		"",
		"func New(msg string) *Foo {",
		"\treturn &Foo{Message: msg}",
		"}",
		"",
	}, "\n"))

	writeBenchmarkFile(tb, filepath.Join(root, "dep", "wire.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"github.com/goforj/wire\"",
		"",
		"var NewSet = wire.NewSet(NewMessage, New)",
		"",
	}, "\n"))
}

func TestGenerateIncrementalLargeRepoShapeChangeMatchesNormalGenerate(t *testing.T) {
	lockCacheHooks(t)
	state := saveCacheHooks()
	t.Cleanup(func() { restoreCacheHooks(state) })

	cacheRoot := t.TempDir()
	osTempDir = func() string { return cacheRoot }

	repoRoot := benchmarkRepoRoot(t)
	root := t.TempDir()
	writeLargeBenchmarkModule(t, repoRoot, root, largeBenchmarkTestPackageCount)

	env := append(os.Environ(), "GOWORK=off")
	incrementalCtx := WithIncremental(context.Background(), true)

	if _, errs := Generate(incrementalCtx, root, env, []string{"./app"}, &GenerateOptions{}); len(errs) > 0 {
		t.Fatalf("baseline incremental Generate returned errors: %v", errs)
	}

	mutateLargeBenchmarkModule(t, root, largeBenchmarkTestPackageCount/2)

	var incrementalLabels []string
	incrementalTimedCtx := WithTiming(incrementalCtx, func(label string, _ time.Duration) {
		incrementalLabels = append(incrementalLabels, label)
	})
	incrementalGens, errs := Generate(incrementalTimedCtx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("incremental large-repo Generate returned errors: %v", errs)
	}
	if len(incrementalGens) != 1 || len(incrementalGens[0].Errs) > 0 {
		t.Fatalf("unexpected incremental results: %+v", incrementalGens)
	}
	if !containsLabel(incrementalLabels, "incremental.local_fastpath.load") {
		t.Fatalf("expected large-repo shape change to use local fast path, labels=%v", incrementalLabels)
	}

	normalGens, errs := Generate(context.Background(), root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		t.Fatalf("normal large-repo Generate returned errors: %v", errs)
	}
	if len(normalGens) != 1 || len(normalGens[0].Errs) > 0 {
		t.Fatalf("unexpected normal results: %+v", normalGens)
	}
	if incrementalGens[0].OutputPath != normalGens[0].OutputPath {
		t.Fatalf("output paths differ: incremental=%q normal=%q", incrementalGens[0].OutputPath, normalGens[0].OutputPath)
	}
	if string(incrementalGens[0].Content) != string(normalGens[0].Content) {
		t.Fatal("large-repo shape-changing incremental output differs from normal Generate output")
	}
}

func runLargeRepoShapeChangeBenchmarks(b *testing.B, incremental bool) {
	cacheHooksMu.Lock()
	state := saveCacheHooks()
	b.Cleanup(func() {
		restoreCacheHooks(state)
		cacheHooksMu.Unlock()
	})

	repoRoot := benchmarkRepoRoot(b)
	for _, packageCount := range largeBenchmarkSizes {
		packageCount := packageCount
		b.Run(fmt.Sprintf("size=%d", packageCount), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StartTimer()
				_ = measureLargeRepoShapeChangeOnce(b, repoRoot, packageCount, incremental)
				b.StopTimer()
			}
		})
	}
}

func incrementalScenarioBenchmarks() []incrementalScenarioBenchmarkCase {
	return []incrementalScenarioBenchmarkCase{
		{
			name:   "preload_unchanged",
			mutate: func(testing.TB, string) {},
		},
		{
			name: "preload_whitespace_only_change",
			mutate: func(tb testing.TB, root string) {
				writeBenchmarkFile(tb, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
					"package dep",
					"",
					"const SQLText = \"green\"",
					"",
					"var defaultCount = 1",
					"",
					"type Foo struct { Message string }",
					"",
					"func NewMessage() string { return SQLText }",
					"",
					"func helper(msg string) string { return msg }",
					"",
					"",
					"func New(msg string) *Foo {",
					"",
					"\treturn &Foo{Message: helper(msg)}",
					"",
					"}",
					"",
				}, "\n"))
			},
		},
		{
			name: "preload_body_only_change",
			mutate: func(tb testing.TB, root string) {
				writeBenchmarkFile(tb, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
					"package dep",
					"",
					"const SQLText = \"green\"",
					"",
					"var defaultCount = 1",
					"",
					"type Foo struct { Message string }",
					"",
					"func NewMessage() string {",
					"\treturn helper(SQLText)",
					"}",
					"",
					"func helper(msg string) string { return msg }",
					"",
					"func New(msg string) *Foo {",
					"\treturn &Foo{Message: helper(msg)}",
					"}",
					"",
				}, "\n"))
			},
		},
		{
			name: "preload_body_only_repeat_change",
			measure: func(tb testing.TB, root string, env []string, ctx context.Context) incrementalScenarioTrace {
				writeBodyOnlyScenarioVariant(tb, root, "b")
				if _, errs := Generate(ctx, root, env, []string{"./app"}, &GenerateOptions{}); len(errs) > 0 {
					tb.Fatalf("warm changed variant Generate returned errors: %v", errs)
				}
				writeBodyOnlyScenarioVariant(tb, root, "a")
				if _, errs := Generate(ctx, root, env, []string{"./app"}, &GenerateOptions{}); len(errs) > 0 {
					tb.Fatalf("reset variant Generate returned errors: %v", errs)
				}
				writeBodyOnlyScenarioVariant(tb, root, "b")
				trace := incrementalScenarioTrace{labels: make(map[string]time.Duration)}
				timedCtx := WithTiming(ctx, func(label string, dur time.Duration) {
					trace.labels[label] += dur
				})
				start := time.Now()
				gens, errs := Generate(timedCtx, root, env, []string{"./app"}, &GenerateOptions{})
				trace.total = time.Since(start)
				if len(errs) > 0 {
					tb.Fatalf("%s: Generate returned errors: %v", "preload_body_only_repeat_change", errs)
				}
				if len(gens) != 1 || len(gens[0].Errs) > 0 {
					tb.Fatalf("%s: unexpected Generate results: %+v", "preload_body_only_repeat_change", gens)
				}
				return trace
			},
		},
		{
			name: "local_fastpath_method_body_change",
			mutate: func(tb testing.TB, root string) {
				writeBenchmarkFile(tb, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
					"package dep",
					"",
					"const SQLText = \"green\"",
					"",
					"var defaultCount = 1",
					"",
					"type Foo struct { Message string }",
					"",
					"func (f Foo) Summary() string {",
					"\treturn helper(f.Message)",
					"}",
					"",
					"func NewMessage() string { return SQLText }",
					"",
					"func helper(msg string) string { return msg }",
					"",
					"func New(msg string) *Foo {",
					"\treturn &Foo{Message: msg}",
					"}",
					"",
				}, "\n"))
			},
		},
		{
			name: "preload_const_value_change",
			mutate: func(tb testing.TB, root string) {
				writeBenchmarkFile(tb, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
					"package dep",
					"",
					"const SQLText = \"blue\"",
					"",
					"var defaultCount = 1",
					"",
					"type Foo struct { Message string }",
					"",
					"func NewMessage() string { return SQLText }",
					"",
					"func helper(msg string) string { return msg }",
					"",
					"func New(msg string) *Foo {",
					"\treturn &Foo{Message: helper(msg)}",
					"}",
					"",
				}, "\n"))
			},
		},
		{
			name: "preload_var_initializer_change",
			mutate: func(tb testing.TB, root string) {
				writeBenchmarkFile(tb, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
					"package dep",
					"",
					"const SQLText = \"green\"",
					"",
					"var defaultCount = 2",
					"",
					"type Foo struct { Message string }",
					"",
					"func NewMessage() string { return SQLText }",
					"",
					"func helper(msg string) string { return msg }",
					"",
					"func New(msg string) *Foo {",
					"\treturn &Foo{Message: helper(msg)}",
					"}",
					"",
				}, "\n"))
			},
		},
		{
			name: "local_fastpath_add_top_level_helper",
			mutate: func(tb testing.TB, root string) {
				writeBenchmarkFile(tb, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
					"package dep",
					"",
					"const SQLText = \"green\"",
					"",
					"var defaultCount = 1",
					"",
					"type Foo struct { Message string }",
					"",
					"func NewMessage() string { return SQLText }",
					"",
					"func helper(msg string) string { return msg }",
					"",
					"func NewTag() string { return \"tag\" }",
					"",
					"func New(msg string) *Foo {",
					"\treturn &Foo{Message: helper(msg)}",
					"}",
					"",
				}, "\n"))
			},
		},
		{
			name: "preload_import_only_implementation_change",
			mutate: func(tb testing.TB, root string) {
				writeBenchmarkFile(tb, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
					"package dep",
					"",
					"import \"fmt\"",
					"",
					"const SQLText = \"green\"",
					"",
					"var defaultCount = 1",
					"",
					"type Foo struct { Message string }",
					"",
					"func NewMessage() string { return SQLText }",
					"",
					"func helper(msg string) string { return fmt.Sprint(msg) }",
					"",
					"func New(msg string) *Foo {",
					"\treturn &Foo{Message: helper(msg)}",
					"}",
					"",
				}, "\n"))
			},
		},
		{
			name: "local_fastpath_signature_change",
			mutate: func(tb testing.TB, root string) {
				writeBenchmarkFile(tb, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
					"package dep",
					"",
					"const SQLText = \"green\"",
					"",
					"var defaultCount = 7",
					"",
					"type Foo struct {",
					"\tMessage string",
					"\tCount   int",
					"}",
					"",
					"func NewMessage() string { return SQLText }",
					"",
					"func NewCount() int { return defaultCount }",
					"",
					"func helper(msg string) string { return msg }",
					"",
					"func New(msg string, count int) *Foo {",
					"\treturn &Foo{Message: helper(msg), Count: count}",
					"}",
					"",
				}, "\n"))
				writeBenchmarkFile(tb, filepath.Join(root, "dep", "wire.go"), strings.Join([]string{
					"package dep",
					"",
					"import \"github.com/goforj/wire\"",
					"",
					"var NewSet = wire.NewSet(NewMessage, NewCount, New)",
					"",
				}, "\n"))
			},
		},
		{
			name: "local_fastpath_struct_field_addition",
			mutate: func(tb testing.TB, root string) {
				writeBenchmarkFile(tb, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
					"package dep",
					"",
					"const SQLText = \"green\"",
					"",
					"var defaultCount = 1",
					"",
					"type Foo struct {",
					"\tMessage string",
					"\tCount   int",
					"}",
					"",
					"func NewMessage() string { return SQLText }",
					"",
					"func helper(msg string) string { return msg }",
					"",
					"func New(msg string) *Foo {",
					"\treturn &Foo{Message: helper(msg), Count: defaultCount}",
					"}",
					"",
				}, "\n"))
			},
		},
		{
			name: "local_fastpath_interface_method_addition",
			mutate: func(tb testing.TB, root string) {
				writeBenchmarkFile(tb, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
					"package dep",
					"",
					"const SQLText = \"green\"",
					"",
					"var defaultCount = 1",
					"",
					"type Fooer interface {",
					"\tMessage() string",
					"\tCount() int",
					"}",
					"",
					"type Foo struct { Message string }",
					"",
					"func NewMessage() string { return SQLText }",
					"",
					"func helper(msg string) string { return msg }",
					"",
					"func New(msg string) *Foo {",
					"\treturn &Foo{Message: helper(msg)}",
					"}",
					"",
				}, "\n"))
			},
		},
		{
			name: "fallback_invalid_body_change",
			mutate: func(tb testing.TB, root string) {
				writeBenchmarkFile(tb, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
					"package dep",
					"",
					"const SQLText = \"green\"",
					"",
					"var defaultCount = 1",
					"",
					"type Foo struct { Message string }",
					"",
					"func NewMessage() string { return missing }",
					"",
					"func helper(msg string) string { return msg }",
					"",
					"func New(msg string) *Foo {",
					"\treturn &Foo{Message: helper(msg)}",
					"}",
					"",
				}, "\n"))
			},
			wantErr: true,
		},
	}
}

func incrementalScenarioPerformanceBudgets() map[string]incrementalScenarioBudget {
	return map[string]incrementalScenarioBudget{
		"preload_unchanged": {
			total:         300 * time.Millisecond,
			validateLocal: 25 * time.Millisecond,
			validateExt:   25 * time.Millisecond,
			validateTouch: 5 * time.Millisecond,
			outputs:       5 * time.Millisecond,
		},
		"preload_whitespace_only_change": {
			total:         300 * time.Millisecond,
			validateLocal: 25 * time.Millisecond,
			validateExt:   25 * time.Millisecond,
			validateTouch: 250 * time.Millisecond,
			outputs:       5 * time.Millisecond,
		},
		"preload_body_only_change": {
			total:         400 * time.Millisecond,
			validateLocal: 40 * time.Millisecond,
			validateExt:   40 * time.Millisecond,
			validateTouch: 250 * time.Millisecond,
			outputs:       5 * time.Millisecond,
		},
		"preload_body_only_repeat_change": {
			total:            150 * time.Millisecond,
			validateLocal:    40 * time.Millisecond,
			validateExt:      40 * time.Millisecond,
			validateTouch:    5 * time.Millisecond,
			validateTouchHit: 5 * time.Millisecond,
			outputs:          5 * time.Millisecond,
		},
		"local_fastpath_method_body_change": {
			total:         500 * time.Millisecond,
			validateLocal: 60 * time.Millisecond,
			validateExt:   60 * time.Millisecond,
			localFastpath: 300 * time.Millisecond,
		},
		"preload_import_only_implementation_change": {
			total:         150 * time.Millisecond,
			validateLocal: 40 * time.Millisecond,
			validateExt:   40 * time.Millisecond,
			validateTouch: 50 * time.Millisecond,
			outputs:       5 * time.Millisecond,
		},
		"preload_const_value_change": {
			total:         400 * time.Millisecond,
			validateLocal: 40 * time.Millisecond,
			validateExt:   40 * time.Millisecond,
			validateTouch: 250 * time.Millisecond,
			outputs:       5 * time.Millisecond,
		},
		"preload_var_initializer_change": {
			total:         400 * time.Millisecond,
			validateLocal: 40 * time.Millisecond,
			validateExt:   40 * time.Millisecond,
			validateTouch: 250 * time.Millisecond,
			outputs:       5 * time.Millisecond,
		},
		"local_fastpath_add_top_level_helper": {
			total:         500 * time.Millisecond,
			validateLocal: 60 * time.Millisecond,
			validateExt:   60 * time.Millisecond,
			localFastpath: 300 * time.Millisecond,
		},
		"local_fastpath_signature_change": {
			total:         500 * time.Millisecond,
			validateLocal: 60 * time.Millisecond,
			validateExt:   60 * time.Millisecond,
			localFastpath: 300 * time.Millisecond,
		},
		"local_fastpath_struct_field_addition": {
			total:         500 * time.Millisecond,
			validateLocal: 60 * time.Millisecond,
			validateExt:   60 * time.Millisecond,
			localFastpath: 300 * time.Millisecond,
		},
		"local_fastpath_interface_method_addition": {
			total:         500 * time.Millisecond,
			validateLocal: 60 * time.Millisecond,
			validateExt:   60 * time.Millisecond,
			localFastpath: 300 * time.Millisecond,
		},
		"fallback_invalid_body_change": {
			total:        800 * time.Millisecond,
			generateLoad: 500 * time.Millisecond,
		},
	}
}

func measureIncrementalScenarioOnce(tb testing.TB, repoRoot string, scenario incrementalScenarioBenchmarkCase) incrementalScenarioTrace {
	tb.Helper()

	cacheRoot := tb.TempDir()
	osTempDir = func() string { return cacheRoot }

	root := tb.TempDir()
	writeIncrementalScenarioBenchmarkModule(tb, repoRoot, root)

	env := append(os.Environ(), "GOWORK=off")
	ctx := WithIncremental(context.Background(), true)

	if _, errs := Generate(ctx, root, env, []string{"./app"}, &GenerateOptions{}); len(errs) > 0 {
		tb.Fatalf("baseline Generate returned errors: %v", errs)
	}

	if scenario.measure != nil {
		return scenario.measure(tb, root, env, ctx)
	}

	scenario.mutate(tb, root)

	trace := incrementalScenarioTrace{labels: make(map[string]time.Duration)}
	timedCtx := WithTiming(ctx, func(label string, dur time.Duration) {
		trace.labels[label] += dur
	})
	start := time.Now()
	gens, errs := Generate(timedCtx, root, env, []string{"./app"}, &GenerateOptions{})
	trace.total = time.Since(start)

	if scenario.wantErr {
		if len(errs) == 0 {
			tb.Fatalf("%s: expected Generate errors", scenario.name)
		}
		if len(gens) != 0 {
			tb.Fatalf("%s: expected no generated results on error, got %+v", scenario.name, gens)
		}
		return trace
	}

	if len(errs) > 0 {
		tb.Fatalf("%s: Generate returned errors: %v", scenario.name, errs)
	}
	if len(gens) != 1 || len(gens[0].Errs) > 0 {
		tb.Fatalf("%s: unexpected Generate results: %+v", scenario.name, gens)
	}
	return trace
}

func writeIncrementalScenarioBenchmarkModule(tb testing.TB, repoRoot string, root string) {
	tb.Helper()

	writeBenchmarkFile(tb, filepath.Join(root, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require github.com/goforj/wire v0.0.0",
		"replace github.com/goforj/wire => " + repoRoot,
		"",
	}, "\n"))

	writeBenchmarkFile(tb, filepath.Join(root, "app", "wire.go"), strings.Join([]string{
		"//go:build wireinject",
		"// +build wireinject",
		"",
		"package app",
		"",
		"import (",
		"\t\"example.com/app/dep\"",
		"\t\"github.com/goforj/wire\"",
		")",
		"",
		"func Init() *dep.Foo {",
		"\twire.Build(dep.NewSet)",
		"\treturn nil",
		"}",
		"",
	}, "\n"))

	writeBodyOnlyScenarioVariant(tb, root, "green")
}

func writeBodyOnlyScenarioVariant(tb testing.TB, root string, value string) {
	tb.Helper()
	writeBenchmarkFile(tb, filepath.Join(root, "dep", "dep.go"), strings.Join([]string{
		"package dep",
		"",
		"const SQLText = \"" + value + "\"",
		"",
		"var defaultCount = 1",
		"",
		"type Foo struct { Message string }",
		"",
		"func NewMessage() string { return SQLText }",
		"",
		"func helper(msg string) string { return msg }",
		"",
		"func New(msg string) *Foo {",
		"\treturn &Foo{Message: helper(msg)}",
		"}",
		"",
	}, "\n"))

	writeBenchmarkFile(tb, filepath.Join(root, "dep", "wire.go"), strings.Join([]string{
		"package dep",
		"",
		"import \"github.com/goforj/wire\"",
		"",
		"var NewSet = wire.NewSet(NewMessage, New)",
		"",
	}, "\n"))
}

func measureIncrementalScenarioMedian(tb testing.TB, repoRoot string, scenario incrementalScenarioBenchmarkCase, samples int) incrementalScenarioTrace {
	tb.Helper()
	if samples <= 0 {
		samples = 1
	}
	traces := make([]incrementalScenarioTrace, 0, samples)
	for i := 0; i < samples; i++ {
		traces = append(traces, measureIncrementalScenarioOnce(tb, repoRoot, scenario))
	}
	sort.Slice(traces, func(i, j int) bool { return traces[i].total < traces[j].total })
	return traces[len(traces)/2]
}

func assertScenarioBudget(t *testing.T, trace incrementalScenarioTrace, budget incrementalScenarioBudget) {
	t.Helper()
	checkBudgetDuration(t, "total", trace.total, budget.total)
	checkBudgetDuration(t, "validate_local_packages", trace.label("incremental.preload_manifest.validate_local_packages"), budget.validateLocal)
	checkBudgetDuration(t, "validate_external_files", trace.label("incremental.preload_manifest.validate_external_files"), budget.validateExt)
	checkBudgetDuration(t, "validate_touched", trace.label("incremental.preload_manifest.validate_touched"), budget.validateTouch)
	checkBudgetDuration(t, "validate_touched_cache_hit", trace.label("incremental.preload_manifest.validate_touched_cache_hit"), budget.validateTouchHit)
	checkBudgetDuration(t, "outputs", trace.label("incremental.preload_manifest.outputs"), budget.outputs)
	checkBudgetDuration(t, "generate_load", trace.label("generate.load"), budget.generateLoad)
	checkBudgetDuration(t, "local_fastpath_load", trace.label("incremental.local_fastpath.load"), budget.localFastpath)
}

func checkBudgetDuration(t *testing.T, name string, got time.Duration, max time.Duration) {
	t.Helper()
	if max <= 0 {
		return
	}
	if got > max {
		t.Fatalf("%s exceeded budget: got=%s max=%s", name, got, max)
	}
}

func (s incrementalScenarioTrace) label(name string) time.Duration {
	if s.labels == nil {
		return 0
	}
	return s.labels[name]
}

type largeRepoBenchmarkRow struct {
	packageCount    int
	coldNormal      time.Duration
	coldIncremental time.Duration
	normal          time.Duration
	incremental     time.Duration
	knownToggle     time.Duration
}

type shapeChangeTrace struct {
	total  time.Duration
	labels map[string]time.Duration
}

func largeRepoPerformanceBudgets() map[int]largeRepoPerformanceBudget {
	return map[int]largeRepoPerformanceBudget{
		10: {
			shapeTotal:  45 * time.Millisecond,
			localLoad:   3 * time.Millisecond,
			parse:       500 * time.Microsecond,
			typecheck:   4 * time.Millisecond,
			generate:    3 * time.Millisecond,
			knownToggle: 3 * time.Millisecond,
		},
		100: {
			shapeTotal:  35 * time.Millisecond,
			localLoad:   20 * time.Millisecond,
			parse:       1500 * time.Microsecond,
			typecheck:   12 * time.Millisecond,
			generate:    20 * time.Millisecond,
			knownToggle: 15 * time.Millisecond,
		},
		1000: {
			shapeTotal:  260 * time.Millisecond,
			localLoad:   110 * time.Millisecond,
			parse:       4 * time.Millisecond,
			typecheck:   70 * time.Millisecond,
			generate:    180 * time.Millisecond,
			knownToggle: 90 * time.Millisecond,
		},
	}
}

func measureLargeRepoShapeChangeOnce(tb testing.TB, repoRoot string, packageCount int, incremental bool) time.Duration {
	tb.Helper()

	cacheRoot := tb.TempDir()
	osTempDir = func() string { return cacheRoot }

	root := tb.TempDir()
	writeLargeBenchmarkModule(tb, repoRoot, root, packageCount)
	env := append(os.Environ(), "GOWORK=off")
	ctx := context.Background()
	if incremental {
		ctx = WithIncremental(ctx, true)
	}
	if _, errs := Generate(ctx, root, env, []string{"./app"}, &GenerateOptions{}); len(errs) > 0 {
		tb.Fatalf("baseline Generate returned errors: %v", errs)
	}

	mutateLargeBenchmarkModule(tb, root, packageCount/2)

	start := time.Now()
	gens, errs := Generate(ctx, root, env, []string{"./app"}, &GenerateOptions{})
	dur := time.Since(start)
	if len(errs) > 0 {
		tb.Fatalf("shape-change Generate returned errors: %v", errs)
	}
	if len(gens) != 1 || len(gens[0].Errs) > 0 {
		tb.Fatalf("unexpected Generate results: %+v", gens)
	}
	return dur
}

func measureLargeRepoShapeChangeTraceOnce(tb testing.TB, repoRoot string, packageCount int, incremental bool) shapeChangeTrace {
	tb.Helper()

	cacheRoot := tb.TempDir()
	osTempDir = func() string { return cacheRoot }

	root := tb.TempDir()
	writeLargeBenchmarkModule(tb, repoRoot, root, packageCount)
	env := append(os.Environ(), "GOWORK=off")
	ctx := context.Background()
	if incremental {
		ctx = WithIncremental(ctx, true)
	}
	if _, errs := Generate(ctx, root, env, []string{"./app"}, &GenerateOptions{}); len(errs) > 0 {
		tb.Fatalf("baseline Generate returned errors: %v", errs)
	}

	mutateLargeBenchmarkModule(tb, root, packageCount/2)

	trace := shapeChangeTrace{labels: make(map[string]time.Duration)}
	ctx = WithTiming(ctx, func(label string, dur time.Duration) {
		trace.labels[label] += dur
	})
	start := time.Now()
	gens, errs := Generate(ctx, root, env, []string{"./app"}, &GenerateOptions{})
	trace.total = time.Since(start)
	if len(errs) > 0 {
		tb.Fatalf("shape-change Generate returned errors: %v", errs)
	}
	if len(gens) != 1 || len(gens[0].Errs) > 0 {
		tb.Fatalf("unexpected Generate results: %+v", gens)
	}
	return trace
}

func measureLargeRepoShapeChangeTraceMedian(tb testing.TB, repoRoot string, packageCount int, incremental bool, samples int) shapeChangeTrace {
	tb.Helper()
	if samples <= 0 {
		samples = 1
	}
	traces := make([]shapeChangeTrace, 0, samples)
	for i := 0; i < samples; i++ {
		traces = append(traces, measureLargeRepoShapeChangeTraceOnce(tb, repoRoot, packageCount, incremental))
	}
	sort.Slice(traces, func(i, j int) bool { return traces[i].total < traces[j].total })
	return traces[len(traces)/2]
}

func (s shapeChangeTrace) label(name string) time.Duration {
	if s.labels == nil {
		return 0
	}
	return s.labels[name]
}

func measureLargeRepoColdOnce(tb testing.TB, repoRoot string, packageCount int, incremental bool) time.Duration {
	tb.Helper()

	cacheRoot := tb.TempDir()
	osTempDir = func() string { return cacheRoot }

	root := tb.TempDir()
	writeLargeBenchmarkModule(tb, repoRoot, root, packageCount)
	env := append(os.Environ(), "GOWORK=off")
	ctx := context.Background()
	if incremental {
		ctx = WithIncremental(ctx, true)
	}

	start := time.Now()
	gens, errs := Generate(ctx, root, env, []string{"./app"}, &GenerateOptions{})
	dur := time.Since(start)
	if len(errs) > 0 {
		tb.Fatalf("cold Generate returned errors: %v", errs)
	}
	if len(gens) != 1 || len(gens[0].Errs) > 0 {
		tb.Fatalf("unexpected cold Generate results: %+v", gens)
	}
	return dur
}

func measureLargeRepoKnownToggleOnce(tb testing.TB, repoRoot string, packageCount int) time.Duration {
	tb.Helper()

	cacheRoot := tb.TempDir()
	osTempDir = func() string { return cacheRoot }

	root := tb.TempDir()
	writeLargeBenchmarkModule(tb, repoRoot, root, packageCount)
	env := append(os.Environ(), "GOWORK=off")
	ctx := WithIncremental(context.Background(), true)
	mutatedIndex := packageCount / 2

	if _, errs := Generate(ctx, root, env, []string{"./app"}, &GenerateOptions{}); len(errs) > 0 {
		tb.Fatalf("baseline Generate returned errors: %v", errs)
	}

	mutateLargeBenchmarkModule(tb, root, mutatedIndex)
	gens, errs := Generate(ctx, root, env, []string{"./app"}, &GenerateOptions{})
	if len(errs) > 0 {
		tb.Fatalf("mutated Generate returned errors: %v", errs)
	}
	if len(gens) != 1 || len(gens[0].Errs) > 0 {
		tb.Fatalf("unexpected mutated Generate results: %+v", gens)
	}

	writeLargeBenchmarkPackage(tb, root, mutatedIndex, false)

	start := time.Now()
	gens, errs = Generate(ctx, root, env, []string{"./app"}, &GenerateOptions{})
	dur := time.Since(start)
	if len(errs) > 0 {
		tb.Fatalf("toggle Generate returned errors: %v", errs)
	}
	if len(gens) != 1 || len(gens[0].Errs) > 0 {
		tb.Fatalf("unexpected toggle Generate results: %+v", gens)
	}
	return dur
}

func measureLargeRepoKnownToggleMedian(tb testing.TB, repoRoot string, packageCount int, samples int) time.Duration {
	tb.Helper()
	if samples <= 0 {
		samples = 1
	}
	values := make([]time.Duration, 0, samples)
	for i := 0; i < samples; i++ {
		values = append(values, measureLargeRepoKnownToggleOnce(tb, repoRoot, packageCount))
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return values[len(values)/2]
}

func formatPercentImprovement(normal time.Duration, incremental time.Duration) string {
	if normal <= 0 {
		return "0.0%"
	}
	improvement := 100 * (float64(normal-incremental) / float64(normal))
	return fmt.Sprintf("%.1f%%", improvement)
}

func speedupRatio(normal time.Duration, incremental time.Duration) float64 {
	if incremental <= 0 {
		return 0
	}
	return float64(normal) / float64(incremental)
}

func formatBenchmarkDuration(d time.Duration) string {
	switch {
	case d >= time.Second:
		return fmt.Sprintf("%.2fs", d.Seconds())
	case d >= time.Millisecond:
		return fmt.Sprintf("%.2fms", float64(d)/float64(time.Millisecond))
	case d >= time.Microsecond:
		return fmt.Sprintf("%.2fus", float64(d)/float64(time.Microsecond))
	default:
		return d.String()
	}
}

func writeLargeBenchmarkModule(tb testing.TB, repoRoot string, root string, packageCount int) {
	tb.Helper()

	writeBenchmarkFile(tb, filepath.Join(root, "go.mod"), strings.Join([]string{
		"module example.com/app",
		"",
		"go 1.19",
		"",
		"require github.com/goforj/wire v0.0.0",
		"replace github.com/goforj/wire => " + repoRoot,
		"",
	}, "\n"))

	wireImports := []string{
		"//go:build wireinject",
		"// +build wireinject",
		"",
		"package app",
		"",
		"import (",
		"\t\"github.com/goforj/wire\"",
	}
	appImports := []string{
		"package app",
		"",
		"import (",
	}
	buildArgs := []string{"\twire.Build("}
	argNames := make([]string, 0, packageCount)
	for i := 0; i < packageCount; i++ {
		pkgName := fmt.Sprintf("layer%02d", i)
		wireImports = append(wireImports, fmt.Sprintf("\t%s %q", pkgName, "example.com/app/"+pkgName))
		appImports = append(appImports, fmt.Sprintf("\t%s %q", pkgName, "example.com/app/"+pkgName))
		buildArgs = append(buildArgs, fmt.Sprintf("\t\t%s.NewSet,", pkgName))
		argNames = append(argNames, fmt.Sprintf("dep%02d *%s.Token", i, pkgName))
	}
	wireImports = append(wireImports, ")", "")
	appImports = append(appImports, ")", "")
	wireFile := append([]string{}, wireImports...)
	wireFile = append(wireFile, "func Init() *App {")
	wireFile = append(wireFile, buildArgs...)
	wireFile = append(wireFile, "\t\tNewApp,", "\t)", "\treturn nil", "}", "")
	writeBenchmarkFile(tb, filepath.Join(root, "app", "wire.go"), strings.Join(wireFile, "\n"))

	appGo := append(appImports[:len(appImports)-2], // reuse imports without trailing blank line
		")",
		"",
		"type App struct {",
		"\tCount int",
		"}",
		"",
		fmt.Sprintf("func NewApp(%s) *App {", strings.Join(argNames, ", ")),
		fmt.Sprintf("\treturn &App{Count: %d}", packageCount),
		"}",
		"",
	)
	writeBenchmarkFile(tb, filepath.Join(root, "app", "app.go"), strings.Join(appGo, "\n"))

	for i := 0; i < packageCount; i++ {
		writeLargeBenchmarkPackage(tb, root, i, false)
	}
}

func mutateLargeBenchmarkModule(tb testing.TB, root string, mutatedIndex int) {
	tb.Helper()
	writeLargeBenchmarkPackage(tb, root, mutatedIndex, true)
}

func writeLargeBenchmarkPackage(tb testing.TB, root string, index int, mutated bool) {
	tb.Helper()

	pkgName := fmt.Sprintf("layer%02d", index)
	pkgDir := filepath.Join(root, pkgName)

	writeBenchmarkFile(tb, filepath.Join(pkgDir, "helpers.go"), renderLargeBenchmarkHelpers(pkgName, index, mutated))
	writeBenchmarkFile(tb, filepath.Join(pkgDir, "wire.go"), renderLargeBenchmarkWire(pkgName, mutated))
}

func renderLargeBenchmarkHelpers(pkgName string, index int, mutated bool) string {
	lines := []string{
		"package " + pkgName,
		"",
		"import (",
		"\t\"fmt\"",
		"\t\"strconv\"",
		"\t\"strings\"",
		")",
		"",
		"type Config struct {",
		"\tLabel string",
		"}",
		"",
		"type Weight int",
		"",
		"type Token struct {",
		"\tConfig Config",
		"\tWeight Weight",
		"}",
		"",
		fmt.Sprintf("func NewConfig() Config { return Config{Label: %q} }", pkgName),
		"",
	}
	if mutated {
		lines = append(lines,
			fmt.Sprintf("func NewWeight() Weight { return Weight(%d) }", index+100),
			"",
			"func New(cfg Config, weight Weight) *Token {",
			fmt.Sprintf("\t_ = helper%02d()", largeBenchmarkHelperCount-1),
			"\treturn &Token{Config: cfg, Weight: weight}",
			"}",
			"",
		)
	} else {
		lines = append(lines,
			"func New(cfg Config) *Token {",
			fmt.Sprintf("\t_ = helper%02d()", largeBenchmarkHelperCount-1),
			"\treturn &Token{Config: cfg}",
			"}",
			"",
		)
	}
	for i := 0; i < largeBenchmarkHelperCount; i++ {
		lines = append(lines, fmt.Sprintf("func helper%02d() string {", i))
		lines = append(lines, fmt.Sprintf("\treturn strings.ToUpper(fmt.Sprintf(\"%%s-%%d\", %q, %d)) + strconv.Itoa(%d)", pkgName, i, index+i))
		lines = append(lines, "}", "")
	}
	return strings.Join(lines, "\n")
}

func renderLargeBenchmarkWire(pkgName string, mutated bool) string {
	lines := []string{
		"package " + pkgName,
		"",
		"import (",
		"\t\"github.com/goforj/wire\"",
		")",
		"",
	}
	if mutated {
		lines = append(lines, "var NewSet = wire.NewSet(NewConfig, NewWeight, New)", "")
	} else {
		lines = append(lines, "var NewSet = wire.NewSet(NewConfig, New)", "")
	}
	return strings.Join(lines, "\n")
}

func strconvQuote(s string) string {
	return fmt.Sprintf("%q", s)
}

func benchmarkRepoRoot(tb testing.TB) string {
	tb.Helper()
	wd, err := os.Getwd()
	if err != nil {
		tb.Fatalf("Getwd failed: %v", err)
	}
	repoRoot := filepath.Clean(filepath.Join(wd, "..", ".."))
	if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err != nil {
		tb.Fatalf("repo root not found at %s: %v", repoRoot, err)
	}
	return repoRoot
}

func writeBenchmarkFile(tb testing.TB, path string, content string) {
	tb.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		tb.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		tb.Fatalf("WriteFile failed: %v", err)
	}
}

func renderASCIITable(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, cell := range row {
			if width := utf8.RuneCountInString(cell); width > widths[i] {
				widths[i] = width
			}
		}
	}
	var b strings.Builder
	border := func() {
		b.WriteByte('+')
		for _, width := range widths {
			b.WriteString(strings.Repeat("-", width+2))
			b.WriteByte('+')
		}
		b.WriteByte('\n')
	}
	writeRow := func(row []string) {
		b.WriteByte('|')
		for i, cell := range row {
			b.WriteByte(' ')
			b.WriteString(cell)
			b.WriteString(strings.Repeat(" ", widths[i]-utf8.RuneCountInString(cell)+1))
			b.WriteByte('|')
		}
		b.WriteByte('\n')
	}
	border()
	writeRow(rows[0])
	border()
	for _, row := range rows[1:] {
		writeRow(row)
	}
	border()
	return b.String()
}
