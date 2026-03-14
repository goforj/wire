package wire

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"testing"
	"time"
)

const (
	largeBenchmarkTestPackageCount = 24
	largeBenchmarkHelperCount  = 12
)

var largeBenchmarkSizes = []int{10, 100, 1000}

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
			packageCount:      packageCount,
			coldNormal:        coldNormal,
			coldIncremental:   coldIncremental,
			normal:            normal,
			incremental:       incremental,
			knownToggle:       knownToggle,
		})
	}

	var out strings.Builder
	tw := tabwriter.NewWriter(&out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "size\tcold normal\tcold incr\tcold delta\tcold x\tshape normal\tshape incr\tshape delta\tshape x\tknown toggle")
	for _, row := range rows {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%.2fx\t%s\t%s\t%s\t%.2fx\t%s\n",
			row.packageCount,
			formatBenchmarkDuration(row.coldNormal),
			formatBenchmarkDuration(row.coldIncremental),
			formatPercentImprovement(row.coldNormal, row.coldIncremental),
			speedupRatio(row.coldNormal, row.coldIncremental),
			formatBenchmarkDuration(row.normal),
			formatBenchmarkDuration(row.incremental),
			formatPercentImprovement(row.normal, row.incremental),
			speedupRatio(row.normal, row.incremental),
			formatBenchmarkDuration(row.knownToggle),
		)
	}
	if err := tw.Flush(); err != nil {
		t.Fatalf("flush benchmark table: %v", err)
	}
	fmt.Print(out.String())
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
	var out strings.Builder
	tw := tabwriter.NewWriter(&out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "size\tnormal total\tbase load\tlazy load\tincr total\tfast load\tfast generate\tspeedup")
	for _, packageCount := range largeBenchmarkSizes {
		normal := measureLargeRepoShapeChangeTraceOnce(t, repoRoot, packageCount, false)
		incremental := measureLargeRepoShapeChangeTraceOnce(t, repoRoot, packageCount, true)
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%.2fx\n",
			packageCount,
			formatBenchmarkDuration(normal.total),
			formatBenchmarkDuration(normal.label("load.packages.base.load")),
			formatBenchmarkDuration(normal.label("load.packages.lazy.load")),
			formatBenchmarkDuration(incremental.total),
			formatBenchmarkDuration(incremental.label("incremental.local_fastpath.load")),
			formatBenchmarkDuration(incremental.label("incremental.local_fastpath.generate")),
			speedupRatio(normal.total, incremental.total),
		)
	}
	if err := tw.Flush(); err != nil {
		t.Fatalf("flush breakdown table: %v", err)
	}
	fmt.Print(out.String())
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
		return fmt.Sprintf("%.2fµs", float64(d)/float64(time.Microsecond))
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
