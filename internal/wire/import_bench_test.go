package wire

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	importBenchEnv        = "WIRE_IMPORT_BENCH_TABLE"
	importBenchBreakdown  = "WIRE_IMPORT_BENCH_BREAKDOWN"
	importBenchScenarios  = "WIRE_IMPORT_BENCH_SCENARIOS"
	importBenchScenarioBD = "WIRE_IMPORT_BENCH_SCENARIO_BREAKDOWN"
	stockWireCommit       = "9c25c9016f6825302537c4efdd5e897976f9c826"
	stockWireModulePath   = "github.com/google/wire"
	currentWireModulePath = "github.com/goforj/wire"
)

type importBenchRow struct {
	imports     int
	stockCold   time.Duration
	currentCold time.Duration
	currentWarm time.Duration
}

type importBenchScenarioRow struct {
	profile       string
	localCount    int
	stdlibCount   int
	externalCount int
	name          string
	stock         time.Duration
	current       time.Duration
}

type benchCaches struct {
	home    string
	goCache string
}

type benchGraphCounts struct {
	local    int
	stdlib   int
	external int
}

const importBenchTrials = 3

func TestPrintImportScaleBenchmarkTable(t *testing.T) {
	if os.Getenv(importBenchEnv) != "1" {
		t.Skipf("%s not set", importBenchEnv)
	}
	repoRoot, err := importBenchRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	currentBin := buildWireBinary(t, repoRoot, "current-wire")
	stockDir := extractStockWire(t, repoRoot, stockWireCommit)
	stockBin := buildWireBinary(t, stockDir, "stock-wire")
	stockCaches := newBenchCaches(t)
	currentCaches := newBenchCaches(t)

	sizes := []int{10, 100, 1000}
	rows := make([]importBenchRow, 0, len(sizes))
	for _, n := range sizes {
		stockFixture := createImportBenchFixture(t, n, stockWireModulePath, stockDir)
		currentFixture := createImportBenchFixture(t, n, currentWireModulePath, repoRoot)
		rows = append(rows, importBenchRow{
			imports:     n,
			stockCold:   medianDuration(runColdTrials(t, stockBin, stockFixture, stockCaches, importBenchTrials)),
			currentCold: medianDuration(runColdTrials(t, currentBin, currentFixture, currentCaches, importBenchTrials)),
			currentWarm: medianDuration(runWarmTrials(t, currentBin, currentFixture, currentCaches, importBenchTrials)),
		})
	}
	printImportBenchTable(t, rows)
}

func TestPrintImportScaleBenchmarkBreakdown(t *testing.T) {
	if os.Getenv(importBenchBreakdown) != "1" {
		t.Skipf("%s not set", importBenchBreakdown)
	}
	repoRoot, err := importBenchRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	currentBin := buildWireBinary(t, repoRoot, "current-wire")
	stockDir := extractStockWire(t, repoRoot, stockWireCommit)
	stockBin := buildWireBinary(t, stockDir, "stock-wire")
	stockCaches := newBenchCaches(t)
	currentCaches := newBenchCaches(t)

	const imports = 1000
	stockFixture := createImportBenchFixture(t, imports, stockWireModulePath, stockDir)
	currentFixture := createImportBenchFixture(t, imports, currentWireModulePath, repoRoot)

	stockCold := medianDuration(runColdTrials(t, stockBin, stockFixture, stockCaches, importBenchTrials))
	currentCold := medianDuration(runColdTrials(t, currentBin, currentFixture, currentCaches, importBenchTrials))
	currentWarm := medianDuration(runWarmTrials(t, currentBin, currentFixture, currentCaches, importBenchTrials))

	fmt.Printf("repo size: %d\n", imports)
	fmt.Printf("stock cold: %s\n", formatMs(stockCold))
	fmt.Printf("current cold: %s\n", formatMs(currentCold))
	fmt.Printf("current unchanged: %s\n", formatMs(currentWarm))
	fmt.Printf("cold speedup: %s\n", formatSpeedup(stockCold, currentCold))
	fmt.Printf("unchanged speedup: %s\n", formatSpeedup(stockCold, currentWarm))
	fmt.Printf("cold gap: %s\n", formatMs(currentCold-stockCold))

	prewarmGoBenchCache(t, currentFixture, currentCaches)
	_, output := runWireBenchCommandOutput(t, currentBin, currentFixture, currentCaches, "-timings")
	fmt.Println("current cold timings:")
	printScenarioTimingLines(output)
}

func TestPrintImportScenarioBenchmarkTable(t *testing.T) {
	if os.Getenv(importBenchScenarios) != "1" {
		t.Skipf("%s not set", importBenchScenarios)
	}
	repoRoot, err := importBenchRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	currentBin := buildWireBinary(t, repoRoot, "current-wire")
	stockDir := extractStockWire(t, repoRoot, stockWireCommit)
	stockBin := buildWireBinary(t, stockDir, "stock-wire")

	type appBenchProfile struct {
		localPkgs int
		depPkgs   int
		external  bool
		label     string
	}
	profiles := []appBenchProfile{
		{localPkgs: 10, depPkgs: 25, label: "local"},
		{localPkgs: 10, depPkgs: 1000, label: "local-high"},
		{localPkgs: 10, depPkgs: 25, external: true, label: "external"},
		{localPkgs: 10, depPkgs: 100, external: true, label: "external"},
	}
	rows := make([]importBenchScenarioRow, 0, len(profiles)*6)
	for _, profile := range profiles {
		shapeFixture := createAppShapeBenchFixture(t, profile.localPkgs, profile.depPkgs, profile.external, currentWireModulePath, repoRoot)
		shapeCounts := goListGraphCounts(t, shapeFixture, "example.com/appbench", newBenchCaches(t))
		rows = append(rows,
			importBenchScenarioRow{
				profile:       profile.label,
				localCount:    shapeCounts.local,
				stdlibCount:   shapeCounts.stdlib,
				externalCount: shapeCounts.external,
				name:          "cold run",
				stock:         medianDuration(runAppColdTrials(t, stockBin, profile.localPkgs, profile.depPkgs, profile.external, stockWireModulePath, stockDir, importBenchTrials)),
				current:       medianDuration(runAppColdTrials(t, currentBin, profile.localPkgs, profile.depPkgs, profile.external, currentWireModulePath, repoRoot, importBenchTrials)),
			},
			importBenchScenarioRow{
				profile:       profile.label,
				localCount:    shapeCounts.local,
				stdlibCount:   shapeCounts.stdlib,
				externalCount: shapeCounts.external,
				name:          "unchanged rerun",
				stock:         medianDuration(runAppWarmTrials(t, stockBin, profile.localPkgs, profile.depPkgs, profile.external, stockWireModulePath, stockDir, importBenchTrials)),
				current:       medianDuration(runAppWarmTrials(t, currentBin, profile.localPkgs, profile.depPkgs, profile.external, currentWireModulePath, repoRoot, importBenchTrials)),
			},
			importBenchScenarioRow{
				profile:       profile.label,
				localCount:    shapeCounts.local,
				stdlibCount:   shapeCounts.stdlib,
				externalCount: shapeCounts.external,
				name:          "body-only local edit",
				stock:         medianDuration(runAppScenarioTrials(t, stockBin, profile.localPkgs, profile.depPkgs, profile.external, stockWireModulePath, stockDir, "body", importBenchTrials)),
				current:       medianDuration(runAppScenarioTrials(t, currentBin, profile.localPkgs, profile.depPkgs, profile.external, currentWireModulePath, repoRoot, "body", importBenchTrials)),
			},
			importBenchScenarioRow{
				profile:       profile.label,
				localCount:    shapeCounts.local,
				stdlibCount:   shapeCounts.stdlib,
				externalCount: shapeCounts.external,
				name:          "shape change",
				stock:         medianDuration(runAppScenarioTrials(t, stockBin, profile.localPkgs, profile.depPkgs, profile.external, stockWireModulePath, stockDir, "shape", importBenchTrials)),
				current:       medianDuration(runAppScenarioTrials(t, currentBin, profile.localPkgs, profile.depPkgs, profile.external, currentWireModulePath, repoRoot, "shape", importBenchTrials)),
			},
			importBenchScenarioRow{
				profile:       profile.label,
				localCount:    shapeCounts.local,
				stdlibCount:   shapeCounts.stdlib,
				externalCount: shapeCounts.external,
				name:          "import change",
				stock:         medianDuration(runAppScenarioTrials(t, stockBin, profile.localPkgs, profile.depPkgs, profile.external, stockWireModulePath, stockDir, "import", importBenchTrials)),
				current:       medianDuration(runAppScenarioTrials(t, currentBin, profile.localPkgs, profile.depPkgs, profile.external, currentWireModulePath, repoRoot, "import", importBenchTrials)),
			},
			importBenchScenarioRow{
				profile:       profile.label,
				localCount:    shapeCounts.local,
				stdlibCount:   shapeCounts.stdlib,
				externalCount: shapeCounts.external,
				name:          "known import toggle",
				stock:         medianDuration(runAppKnownToggleTrials(t, stockBin, profile.localPkgs, profile.depPkgs, profile.external, stockWireModulePath, stockDir, importBenchTrials)),
				current:       medianDuration(runAppKnownToggleTrials(t, currentBin, profile.localPkgs, profile.depPkgs, profile.external, currentWireModulePath, repoRoot, importBenchTrials)),
			},
		)
	}
	printImportScenarioBenchTable(t, rows)
}

func TestPrintImportScenarioBenchmarkBreakdown(t *testing.T) {
	if os.Getenv(importBenchScenarioBD) != "1" {
		t.Skipf("%s not set", importBenchScenarioBD)
	}
	repoRoot, err := importBenchRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	currentBin := buildWireBinary(t, repoRoot, "current-wire")
	stockDir := extractStockWire(t, repoRoot, stockWireCommit)
	stockBin := buildWireBinary(t, stockDir, "stock-wire")

	const (
		localPkgs = 10
		depPkgs   = 1000
	)

	stockPkgDir := createAppShapeBenchFixture(t, localPkgs, depPkgs, false, stockWireModulePath, stockDir)
	currentPkgDir := createAppShapeBenchFixture(t, localPkgs, depPkgs, false, currentWireModulePath, repoRoot)
	stockCaches := newBenchCaches(t)
	currentCaches := newBenchCaches(t)

	prewarmGoBenchCache(t, stockPkgDir, stockCaches)
	_ = runWireBenchCommand(t, stockBin, stockPkgDir, stockCaches)
	writeAppShapeControllerFile(t, filepath.Dir(stockPkgDir), 0, "shape")
	_ = runWireBenchCommand(t, stockBin, stockPkgDir, stockCaches)
	writeAppShapeControllerFile(t, filepath.Dir(stockPkgDir), 0, "base")
	stockDur := runWireBenchCommand(t, stockBin, stockPkgDir, stockCaches)

	prewarmGoBenchCache(t, currentPkgDir, currentCaches)
	_ = runWireBenchCommand(t, currentBin, currentPkgDir, currentCaches)
	writeAppShapeControllerFile(t, filepath.Dir(currentPkgDir), 0, "shape")
	_ = runWireBenchCommand(t, currentBin, currentPkgDir, currentCaches)
	writeAppShapeControllerFile(t, filepath.Dir(currentPkgDir), 0, "base")
	currentDur, currentOutput := runWireBenchCommandOutput(t, currentBin, currentPkgDir, currentCaches, "-timings")

	fmt.Printf("scenario: local=%d dep=%d known import toggle\n", localPkgs, depPkgs)
	fmt.Printf("stock: %s\n", formatMs(stockDur))
	fmt.Printf("current: %s\n", formatMs(currentDur))
	fmt.Printf("speedup: %s\n", formatSpeedup(stockDur, currentDur))
	fmt.Println("current timings:")
	printScenarioTimingLines(currentOutput)
}

func runAppColdTrials(t *testing.T, bin string, features, depPkgs int, external bool, wireModulePath, wireReplaceDir string, trials int) []time.Duration {
	t.Helper()
	durations := make([]time.Duration, 0, trials)
	pkgDir := createAppShapeBenchFixture(t, features, depPkgs, external, wireModulePath, wireReplaceDir)
	for i := 0; i < trials; i++ {
		caches := newBenchCaches(t)
		prewarmGoBenchCache(t, pkgDir, caches)
		durations = append(durations, runWireBenchCommand(t, bin, pkgDir, caches))
	}
	return durations
}

func runAppWarmTrials(t *testing.T, bin string, features, depPkgs int, external bool, wireModulePath, wireReplaceDir string, trials int) []time.Duration {
	t.Helper()
	durations := make([]time.Duration, 0, trials)
	pkgDir := createAppShapeBenchFixture(t, features, depPkgs, external, wireModulePath, wireReplaceDir)
	caches := newBenchCaches(t)
	for i := 0; i < trials; i++ {
		resetAppShapeBenchFixture(t, pkgDir, features)
		prewarmGoBenchCache(t, pkgDir, caches)
		_ = runWireBenchCommand(t, bin, pkgDir, caches)
		durations = append(durations, runWireBenchCommand(t, bin, pkgDir, caches))
	}
	return durations
}

func runAppScenarioTrials(t *testing.T, bin string, features, depPkgs int, external bool, wireModulePath, wireReplaceDir, variant string, trials int) []time.Duration {
	t.Helper()
	durations := make([]time.Duration, 0, trials)
	pkgDir := createAppShapeBenchFixture(t, features, depPkgs, external, wireModulePath, wireReplaceDir)
	caches := newBenchCaches(t)
	root := filepath.Dir(pkgDir)
	for i := 0; i < trials; i++ {
		resetAppShapeBenchFixture(t, pkgDir, features)
		prewarmGoBenchCache(t, pkgDir, caches)
		_ = runWireBenchCommand(t, bin, pkgDir, caches)
		writeAppShapeControllerFile(t, root, 0, variant)
		durations = append(durations, runWireBenchCommand(t, bin, pkgDir, caches))
	}
	return durations
}

func runAppKnownToggleTrials(t *testing.T, bin string, features, depPkgs int, external bool, wireModulePath, wireReplaceDir string, trials int) []time.Duration {
	t.Helper()
	durations := make([]time.Duration, 0, trials)
	pkgDir := createAppShapeBenchFixture(t, features, depPkgs, external, wireModulePath, wireReplaceDir)
	caches := newBenchCaches(t)
	root := filepath.Dir(pkgDir)
	for i := 0; i < trials; i++ {
		resetAppShapeBenchFixture(t, pkgDir, features)
		prewarmGoBenchCache(t, pkgDir, caches)
		_ = runWireBenchCommand(t, bin, pkgDir, caches)
		writeAppShapeControllerFile(t, root, 0, "shape")
		_ = runWireBenchCommand(t, bin, pkgDir, caches)
		writeAppShapeControllerFile(t, root, 0, "base")
		durations = append(durations, runWireBenchCommand(t, bin, pkgDir, caches))
	}
	return durations
}

func buildWireBinary(t *testing.T, dir, name string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", out, "./cmd/wire")
	cmd.Dir = dir
	cmd.Env = benchEnv(t.TempDir(), filepath.Join(t.TempDir(), "gocache"))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build wire binary in %s: %v\n%s", dir, err, output)
	}
	return out
}

func newBenchCaches(t *testing.T) benchCaches {
	t.Helper()
	return benchCaches{
		home:    t.TempDir(),
		goCache: filepath.Join(t.TempDir(), "gocache"),
	}
}

func extractStockWire(t *testing.T, repoRoot, commit string) string {
	t.Helper()
	tmp := t.TempDir()
	cmd := exec.Command("git", "archive", "--format=tar", commit)
	cmd.Dir = repoRoot
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("git archive start: %v", err)
	}
	tr := tar.NewReader(stdout)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read stock tar: %v", err)
		}
		target := filepath.Join(tmp, hdr.Name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil {
				t.Fatalf("mkdir %s: %v", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatalf("mkdir parent %s: %v", target, err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				t.Fatalf("create %s: %v", target, err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				t.Fatalf("write %s: %v", target, err)
			}
			if err := f.Close(); err != nil {
				t.Fatalf("close %s: %v", target, err)
			}
		}
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("git archive wait: %v", err)
	}
	return tmp
}

func createImportBenchFixture(t *testing.T, imports int, wireModulePath, wireReplaceDir string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(importBenchGoMod(wireModulePath, wireReplaceDir)), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < imports; i++ {
		dir := filepath.Join(root, fmt.Sprintf("dep%04d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "dep.go"), []byte(importBenchDepFile(i, "base")), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app", "wire.go"), []byte(importBenchWireFile(imports, wireModulePath)), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "app")
}

func createAppShapeBenchFixture(t *testing.T, features, depPkgs int, external bool, wireModulePath, wireReplaceDir string) string {
	t.Helper()
	root := t.TempDir()
	modulePath := "example.com/appbench"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(appShapeGoMod(modulePath, wireModulePath, wireReplaceDir, external)), 0o644); err != nil {
		t.Fatal(err)
	}
	if external {
		seedAppShapeExternalGoSum(t, root)
	}
	for i := 0; i < depPkgs; i++ {
		writeAppShapeFile(t, filepath.Join(root, "internal", fmt.Sprintf("dep%04d", i), "dep.go"), appShapeDepFile(i))
	}
	writeAppShapeFile(t, filepath.Join(root, "internal", "logger", "logger.go"), appShapeLoggerFile(modulePath))
	writeAppShapeFile(t, filepath.Join(root, "internal", "cache", "cache.go"), appShapeCacheFile(modulePath))
	writeAppShapeFile(t, filepath.Join(root, "internal", "db", "db.go"), appShapeDBFile(modulePath))
	writeAppShapeFile(t, filepath.Join(root, "internal", "config", "config.go"), appShapeConfigFile(modulePath))
	writeAppShapeFile(t, filepath.Join(root, "internal", "metrics", "metrics.go"), appShapeMetricsFile(modulePath))
	writeAppShapeFile(t, filepath.Join(root, "internal", "httpx", "httpx.go"), appShapeHTTPXFile(modulePath))
	if external {
		writeAppShapeFile(t, filepath.Join(root, "internal", "extsink", "extsink.go"), appShapeExtSinkFile(modulePath))
	}
	writeAppShapeFile(t, filepath.Join(root, "wire", "app.go"), appShapeAppFile(modulePath, features))
	writeAppShapeFile(t, filepath.Join(root, "wire", "wire.go"), appShapeWireFile(modulePath, wireModulePath, features, external))
	for i := 0; i < features; i++ {
		writeAppShapeFile(t, filepath.Join(root, "internal", fmt.Sprintf("feature%04d", i), "feature.go"), appShapeFeatureFile(modulePath, wireModulePath, i, depPkgs, external))
		writeAppShapeControllerFile(t, root, i, "base")
	}
	return filepath.Join(root, "wire")
}

func writeAppShapeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeAppShapeControllerFile(t *testing.T, root string, index int, variant string) {
	t.Helper()
	path := filepath.Join(root, "internal", fmt.Sprintf("feature%04d", index), "controller.go")
	if err := os.WriteFile(path, []byte(appShapeControllerFile("example.com/appbench", index, variant)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func seedAppShapeExternalGoSum(t *testing.T, root string) {
	t.Helper()
	const source = "/private/tmp/test/go.sum"
	data, err := os.ReadFile(source)
	if err != nil {
		return
	}
	if err := os.WriteFile(filepath.Join(root, "go.sum"), data, 0o644); err != nil {
		t.Fatalf("write seeded go.sum: %v", err)
	}
}

func resetAppShapeBenchFixture(t *testing.T, pkgDir string, features int) {
	t.Helper()
	root := filepath.Dir(pkgDir)
	for i := 0; i < features; i++ {
		writeAppShapeControllerFile(t, root, i, "base")
	}
}

func appShapeGoMod(modulePath, wireModulePath, wireReplaceDir string, external bool) string {
	extraRequires := ""
	if external {
		extraRequires = `
	github.com/alecthomas/kong v1.14.0
	github.com/charmbracelet/lipgloss v1.1.0
	github.com/fsnotify/fsnotify v1.7.0
	github.com/glebarez/sqlite v1.11.0
	github.com/goforj/cache v0.1.5
	github.com/goforj/crypt v1.1.0
	github.com/goforj/env/v2 v2.3.0
	github.com/goforj/httpx v1.1.0
	github.com/goforj/null/v6 v6.0.2
	github.com/goforj/queue v0.1.5
	github.com/goforj/queue/driver/redisqueue v0.1.5
	github.com/goforj/scheduler v1.4.0
	github.com/goforj/storage v0.2.5
	github.com/goforj/storage/driver/localstorage v0.2.5
	github.com/goforj/storage/driver/redisstorage v0.2.5
	github.com/goforj/str v1.3.0
	github.com/google/go-cmp v0.6.0
	github.com/google/subcommands v1.2.0
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.3
	github.com/hibiken/asynq v0.26.0
	github.com/imroc/req/v3 v3.57.0
	github.com/labstack/echo/v4 v4.15.1
	github.com/pmezard/go-difflib v1.0.0
	github.com/redis/go-redis/v9 v9.17.2
	github.com/rs/zerolog v1.34.0
	github.com/shirou/gopsutil/v4 v4.26.2
	golang.org/x/mod v0.33.0
	golang.org/x/net v0.50.0
	golang.org/x/sync v0.19.0
	golang.org/x/sys v0.41.0
	golang.org/x/term v0.40.0
	golang.org/x/tools v0.42.0
	gopkg.in/yaml.v3 v3.0.1
	gorm.io/driver/mysql v1.6.0
	gorm.io/driver/postgres v1.6.0
	gorm.io/gorm v1.31.1`
	}
	return fmt.Sprintf(`module %s

go 1.19

require (
	%s v0.0.0%s
)

replace %s => %s
`, modulePath, wireModulePath, extraRequires, wireModulePath, wireReplaceDir)
}

func appShapeLoggerFile(modulePath string) string {
	return `package logger

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"
)

type Logger struct {
	sink io.Writer
	mu   sync.Mutex
}

func NewLogger() *Logger { return &Logger{sink: os.Stdout} }

func (l *Logger) Log(ctx context.Context, msg string, attrs map[string]string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = json.Marshal(map[string]any{
		"ctx":   ctx != nil,
		"msg":   msg,
		"attrs": attrs,
		"time":  time.Now().UTC().Format(time.RFC3339Nano),
	})
}
`
}

func appShapeCacheFile(modulePath string) string {
	return `package cache

type Manager struct{}

func NewManager() *Manager { return &Manager{} }
`
}

func appShapeDBFile(modulePath string) string {
	return `package db

import (
	"context"
	"database/sql"
	"net/url"
	"path/filepath"
)

type DB struct {
	driver string
	dsn    string
}

func NewDB() *DB {
	_ = filepath.Join("var", "lib", "appbench")
	_ = sql.LevelDefault
	u := &url.URL{Scheme: "postgres", Host: "localhost", Path: "/appbench"}
	return &DB{driver: "postgres", dsn: u.String()}
}

func (db *DB) PingContext(context.Context) error { return nil }
`
}

func appShapeDepFile(index int) string {
	return fmt.Sprintf(`package dep%04d

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
)

type Value struct {
	Name string
}

func Provide() Value {
	sum := sha256.Sum256([]byte(fmt.Sprintf("dep-%%04d", %d)))
	return Value{
		Name: filepath.Join("deps", strings.ToLower(hex.EncodeToString(sum[:])))[:16],
	}
}
`, index, index)
}

func appShapeConfigFile(modulePath string) string {
	return `package config

import (
	"encoding/json"
	"os"
	"strconv"
)

type Config struct {
	Port    int
	Service string
}

func NewConfig() *Config {
	cfg := &Config{Port: 8080, Service: "appbench"}
	if v := os.Getenv("APPBENCH_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.Port = port
		}
	}
	_, _ = json.Marshal(cfg)
	return cfg
}
`
}

func appShapeMetricsFile(modulePath string) string {
	return `package metrics

import (
	"expvar"
	"fmt"
	"sync/atomic"
)

type Metrics struct {
	requests atomic.Int64
	name     string
}

func NewMetrics() *Metrics {
	expvar.NewString("appbench_name").Set("appbench")
	return &Metrics{name: fmt.Sprintf("appbench_%s", "requests")}
}
`
}

func appShapeHTTPXFile(modulePath string) string {
	return `package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
)

type Client struct {
	client *http.Client
}

func NewClient() *Client {
	req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	_ = req.WithContext(context.Background())
	return &Client{client: &http.Client{}}
}
`
}

func appShapeExtSinkFile(modulePath string) string {
	return `package extsink

import (
	"context"
	"fmt"
	"os"

	_ "github.com/alecthomas/kong"
	_ "github.com/charmbracelet/lipgloss"
	_ "github.com/charmbracelet/lipgloss/table"
	"github.com/fsnotify/fsnotify"
	_ "github.com/glebarez/sqlite"
	_ "github.com/goforj/cache"
	_ "github.com/goforj/crypt"
	_ "github.com/goforj/env/v2"
	_ "github.com/goforj/httpx"
	_ "github.com/goforj/null/v6"
	_ "github.com/goforj/queue"
	_ "github.com/goforj/queue/driver/redisqueue"
	_ "github.com/goforj/scheduler"
	_ "github.com/goforj/storage"
	_ "github.com/goforj/storage/driver/localstorage"
	_ "github.com/goforj/storage/driver/redisstorage"
	_ "github.com/goforj/str"
	"github.com/google/go-cmp/cmp"
	"github.com/google/subcommands"
	_ "github.com/google/uuid"
	_ "github.com/gorilla/websocket"
	_ "github.com/hibiken/asynq"
	_ "github.com/imroc/req/v3"
	_ "github.com/labstack/echo/v4"
	_ "github.com/labstack/echo/v4/middleware"
	"github.com/pmezard/go-difflib/difflib"
	_ "github.com/redis/go-redis/v9"
	_ "github.com/rs/zerolog"
	_ "github.com/shirou/gopsutil/v4/cpu"
	_ "github.com/shirou/gopsutil/v4/disk"
	_ "github.com/shirou/gopsutil/v4/host"
	_ "github.com/shirou/gopsutil/v4/mem"
	_ "github.com/shirou/gopsutil/v4/net"
	_ "github.com/shirou/gopsutil/v4/process"
	"golang.org/x/mod/modfile"
	_ "golang.org/x/net/http2"
	_ "golang.org/x/net/http2/h2c"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sys/unix"
	_ "golang.org/x/term"
	"golang.org/x/tools/go/packages"
	_ "gopkg.in/yaml.v3"
	_ "gorm.io/driver/mysql"
	_ "gorm.io/driver/postgres"
	_ "gorm.io/gorm"
)

type Sink struct {
	label string
}

func NewSink() *Sink {
	_ = cmp.Equal("a", "b")
	_ = difflib.UnifiedDiff{}
	_, _ = modfile.Parse("go.mod", []byte("module example.com/appbench"), nil)
	_, _ = packages.Load(&packages.Config{Mode: packages.NeedName}, "fmt")
	var g errgroup.Group
	g.Go(func() error { return nil })
	_ = unix.Getpid()
	_ = fsnotify.Event{Name: os.TempDir()}
	_ = subcommands.ExitSuccess
	return &Sink{label: fmt.Sprintf("sink:%v", context.Background() != nil)}
}
`
}

func appShapeFeatureFile(modulePath, wireModulePath string, index, depPkgs int, external bool) string {
	pkg := fmt.Sprintf("feature%04d", index)
	var depImports strings.Builder
	var depUse strings.Builder
	for i := 0; i < depPkgs; i++ {
		depImports.WriteString(fmt.Sprintf("\tdep%04d %q\n", i, fmt.Sprintf("%s/internal/dep%04d", modulePath, i)))
		depUse.WriteString(fmt.Sprintf("\t_ = dep%04d.Provide()\n", i))
	}
	externalImport := ""
	externalArg := ""
	externalField := ""
	externalUse := ""
	if external {
		externalImport = fmt.Sprintf("\t%q\n", modulePath+"/internal/extsink")
		externalArg = ", sink *extsink.Sink"
		externalField = "\tsink   *extsink.Sink\n"
		externalUse = "\t_ = sink\n"
	}
	return fmt.Sprintf(`package %s

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"
	wire %q
	%q
	%q
	%q
	%q
	%q
%s
%s
)

type Repo struct {
	db      *db.DB
	config  *config.Config
	metrics *metrics.Metrics
%s}

type Service struct {
	repo   *Repo
	logger *logger.Logger
	client *httpx.Client
}

func NewRepo(dbConn *db.DB, cfg *config.Config, m *metrics.Metrics, l *logger.Logger%s) *Repo {
	_, _ = json.Marshal(map[string]any{"feature": %d, "service": cfg.Service})
	l.Log(context.Background(), "repo.init", map[string]string{"feature": strconv.Itoa(%d)})
%s	return &Repo{db: dbConn, config: cfg, metrics: m}
}

func NewService(repo *Repo, l *logger.Logger, client *httpx.Client) *Service {
	_, _ = url.Parse(fmt.Sprintf("https://example.com/%%04d", %d))
	_ = time.Second
	return &Service{repo: repo, logger: l, client: client}
}

var Set = wire.NewSet(NewRepo, NewService, NewController)
`, pkg, wireModulePath, modulePath+"/internal/config", modulePath+"/internal/db", modulePath+"/internal/httpx", modulePath+"/internal/logger", modulePath+"/internal/metrics", depImports.String(), externalImport, externalField, externalArg, index, index, depUse.String()+externalUse, index)
}

func appShapeControllerFile(modulePath string, index int, variant string) string {
	pkg := fmt.Sprintf("feature%04d", index)
	imports := []string{
		`"context"`,
		`"fmt"`,
		`"net/http"`,
		`"strconv"`,
		`"` + modulePath + `/internal/logger"`,
	}
	if variant == "shape" {
		imports = append(imports, `"`+modulePath+`/internal/db"`)
	}
	if variant == "import" {
		imports = append(imports, `"strings"`)
	}
	bodyLine := ""
	switch variant {
	case "body":
		bodyLine = "\t_ = \"body-edit\"\n"
	case "import":
		bodyLine = "\t_ = strings.TrimSpace(\" import-edit \")\n"
	}
	extraField := ""
	extraArg := ""
	extraInit := ""
	if variant == "shape" {
		extraField = "\tdb *db.DB\n"
		extraArg = ", d *db.DB"
		extraInit = "\t\tdb: d,\n"
	}
	return fmt.Sprintf(`package %s

import (
	%s
)

type Controller struct {
	logger *logger.Logger
	service *Service
%s}

func NewController(l *logger.Logger, s *Service%s) *Controller {
%s	l.Log(context.Background(), "controller.init", map[string]string{"feature": strconv.Itoa(%d)})
	_ = http.MethodGet
	_ = fmt.Sprintf("feature-%%d", %d)
	return &Controller{
		logger: l,
		service: s,
%s	}
}
`, pkg, strings.Join(imports, "\n\t"), extraField, extraArg, bodyLine, index, index, extraInit)
}

func appShapeAppFile(modulePath string, features int) string {
	var b strings.Builder
	b.WriteString("package wire\n\n")
	if features > 0 {
		b.WriteString("import (\n")
		for i := 0; i < features; i++ {
			b.WriteString(fmt.Sprintf("\tfeature%04d %q\n", i, fmt.Sprintf("%s/internal/feature%04d", modulePath, i)))
		}
		b.WriteString(")\n\n")
	}
	b.WriteString("type App struct{}\n\n")
	b.WriteString("func NewApp(")
	for i := 0; i < features; i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(fmt.Sprintf("_ *feature%04d.Controller", i))
	}
	b.WriteString(") *App {\n\treturn &App{}\n}\n")
	return b.String()
}

func appShapeWireFile(modulePath, wireModulePath string, features int, external bool) string {
	var b strings.Builder
	b.WriteString("//go:build wireinject\n\n")
	b.WriteString("package wire\n\n")
	b.WriteString("import (\n")
	b.WriteString(fmt.Sprintf("\twire %q\n", wireModulePath))
	b.WriteString(fmt.Sprintf("\t%q\n", modulePath+"/internal/config"))
	b.WriteString(fmt.Sprintf("\t%q\n", modulePath+"/internal/db"))
	if external {
		b.WriteString(fmt.Sprintf("\t%q\n", modulePath+"/internal/extsink"))
	}
	b.WriteString(fmt.Sprintf("\t%q\n", modulePath+"/internal/httpx"))
	b.WriteString(fmt.Sprintf("\t%q\n", modulePath+"/internal/logger"))
	b.WriteString(fmt.Sprintf("\t%q\n", modulePath+"/internal/metrics"))
	for i := 0; i < features; i++ {
		b.WriteString(fmt.Sprintf("\t%q\n", fmt.Sprintf("%s/internal/feature%04d", modulePath, i)))
	}
	b.WriteString(")\n\n")
	b.WriteString("func Initialize() *App {\n\twire.Build(\n")
	b.WriteString("\t\tconfig.NewConfig,\n")
	b.WriteString("\t\tlogger.NewLogger,\n")
	b.WriteString("\t\tdb.NewDB,\n")
	if external {
		b.WriteString("\t\textsink.NewSink,\n")
	}
	b.WriteString("\t\thttpx.NewClient,\n")
	b.WriteString("\t\tmetrics.NewMetrics,\n")
	for i := 0; i < features; i++ {
		b.WriteString(fmt.Sprintf("\t\tfeature%04d.Set,\n", i))
	}
	b.WriteString("\t\tNewApp,\n\t)\n\treturn nil\n}\n")
	return b.String()
}

func runBodyEditTrials(t *testing.T, bin string, imports int, wireModulePath, wireReplaceDir string, trials int) []time.Duration {
	t.Helper()
	durations := make([]time.Duration, 0, trials)
	caches := newBenchCaches(t)
	for i := 0; i < trials; i++ {
		pkgDir := createImportBenchFixture(t, imports, wireModulePath, wireReplaceDir)
		prewarmGoBenchCache(t, pkgDir, caches)
		root := filepath.Dir(pkgDir)
		_ = runWireBenchCommand(t, bin, pkgDir, caches)
		writeImportBenchDepFile(t, root, 0, "body")
		durations = append(durations, runWireBenchCommand(t, bin, pkgDir, caches))
	}
	return durations
}

func runShapeEditTrials(t *testing.T, bin string, imports int, wireModulePath, wireReplaceDir string, trials int) []time.Duration {
	t.Helper()
	durations := make([]time.Duration, 0, trials)
	caches := newBenchCaches(t)
	for i := 0; i < trials; i++ {
		pkgDir := createImportBenchFixture(t, imports, wireModulePath, wireReplaceDir)
		prewarmGoBenchCache(t, pkgDir, caches)
		root := filepath.Dir(pkgDir)
		_ = runWireBenchCommand(t, bin, pkgDir, caches)
		writeImportBenchDepFile(t, root, 0, "shape")
		durations = append(durations, runWireBenchCommand(t, bin, pkgDir, caches))
	}
	return durations
}

func runImportChangeTrials(t *testing.T, bin string, imports int, wireModulePath, wireReplaceDir string, trials int) []time.Duration {
	t.Helper()
	durations := make([]time.Duration, 0, trials)
	caches := newBenchCaches(t)
	for i := 0; i < trials; i++ {
		pkgDir := createImportBenchFixture(t, imports+1, wireModulePath, wireReplaceDir)
		prewarmGoBenchCache(t, pkgDir, caches)
		root := filepath.Dir(pkgDir)
		writeImportBenchWireFile(t, root, imports, wireModulePath)
		_ = runWireBenchCommand(t, bin, pkgDir, caches)
		writeImportBenchWireFile(t, root, imports+1, wireModulePath)
		durations = append(durations, runWireBenchCommand(t, bin, pkgDir, caches))
	}
	return durations
}

func runKnownImportToggleTrials(t *testing.T, bin string, imports int, wireModulePath, wireReplaceDir string, trials int) []time.Duration {
	t.Helper()
	durations := make([]time.Duration, 0, trials)
	caches := newBenchCaches(t)
	for i := 0; i < trials; i++ {
		pkgDir := createImportBenchFixture(t, imports+1, wireModulePath, wireReplaceDir)
		prewarmGoBenchCache(t, pkgDir, caches)
		root := filepath.Dir(pkgDir)
		writeImportBenchWireFile(t, root, imports, wireModulePath)
		_ = runWireBenchCommand(t, bin, pkgDir, caches)
		writeImportBenchWireFile(t, root, imports+1, wireModulePath)
		_ = runWireBenchCommand(t, bin, pkgDir, caches)
		writeImportBenchWireFile(t, root, imports, wireModulePath)
		durations = append(durations, runWireBenchCommand(t, bin, pkgDir, caches))
	}
	return durations
}

func runWireBenchCommand(t *testing.T, bin, pkgDir string, caches benchCaches) time.Duration {
	t.Helper()
	d, _ := runWireBenchCommandOutput(t, bin, pkgDir, caches)
	return d
}

func runWireBenchCommandOutput(t *testing.T, bin, pkgDir string, caches benchCaches, extraArgs ...string) (time.Duration, string) {
	t.Helper()
	args := []string{"gen"}
	args = append(args, extraArgs...)
	cmd := exec.Command(bin, args...)
	cmd.Dir = pkgDir
	cmd.Env = append(benchEnv(caches.home, caches.goCache), "WIRE_LOADER_ARTIFACTS=1")
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	start := time.Now()
	if err := cmd.Run(); err != nil {
		t.Fatalf("run %s in %s: %v\n%s", bin, pkgDir, err, stderr.String())
	}
	return time.Since(start), stderr.String()
}

func prewarmGoBenchCache(t *testing.T, pkgDir string, caches benchCaches) {
	t.Helper()
	prepareBenchModule(t, pkgDir, caches)
	cmd := exec.Command("go", "list", "-deps", "./...")
	cmd.Dir = pkgDir
	cmd.Env = benchEnv(caches.home, caches.goCache)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("prewarm go cache in %s: %v\n%s", pkgDir, err, output)
	}
}

func goListGraphCounts(t *testing.T, pkgDir, modulePath string, caches benchCaches) benchGraphCounts {
	t.Helper()
	prepareBenchModule(t, pkgDir, caches)
	cmd := exec.Command("go", "list", "-deps", "-json", "./...")
	cmd.Dir = pkgDir
	cmd.Env = benchEnv(caches.home, caches.goCache)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list graph counts in %s: %v\n%s", pkgDir, err, output)
	}
	dec := json.NewDecoder(bytes.NewReader(output))
	seen := make(map[string]struct{})
	var counts benchGraphCounts
	for {
		var pkg struct {
			ImportPath string
			Standard   bool
		}
		if err := dec.Decode(&pkg); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode graph counts for %s: %v", pkgDir, err)
		}
		if pkg.ImportPath == "" {
			continue
		}
		if _, ok := seen[pkg.ImportPath]; ok {
			continue
		}
		seen[pkg.ImportPath] = struct{}{}
		switch {
		case pkg.Standard:
			counts.stdlib++
		case pkg.ImportPath == modulePath || strings.HasPrefix(pkg.ImportPath, modulePath+"/"):
			counts.local++
		default:
			counts.external++
		}
	}
	return counts
}

func prepareBenchModule(t *testing.T, pkgDir string, caches benchCaches) {
	t.Helper()
	marker := filepath.Join(filepath.Dir(pkgDir), ".bench-module-ready")
	if _, err := os.Stat(marker); err == nil {
		return
	}
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = filepath.Dir(pkgDir)
	cmd.Env = benchEnv(caches.home, caches.goCache)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("prepare bench module in %s: %v\n%s", pkgDir, err, output)
	}
	if err := os.WriteFile(marker, []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("write module marker %s: %v", marker, err)
	}
}

func runColdTrials(t *testing.T, bin, pkgDir string, caches benchCaches, trials int) []time.Duration {
	t.Helper()
	durations := make([]time.Duration, 0, trials)
	for i := 0; i < trials; i++ {
		prewarmGoBenchCache(t, pkgDir, caches)
		durations = append(durations, runWireBenchCommand(t, bin, pkgDir, caches))
	}
	return durations
}

func runWarmTrials(t *testing.T, bin, pkgDir string, caches benchCaches, trials int) []time.Duration {
	t.Helper()
	durations := make([]time.Duration, 0, trials)
	for i := 0; i < trials; i++ {
		prewarmGoBenchCache(t, pkgDir, caches)
		_ = runWireBenchCommand(t, bin, pkgDir, caches)
		durations = append(durations, runWireBenchCommand(t, bin, pkgDir, caches))
	}
	return durations
}

func medianDuration(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), durations...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	return sorted[len(sorted)/2]
}

func benchEnv(home, goCache string) []string {
	env := append([]string(nil), os.Environ()...)
	env = append(env,
		"HOME="+home,
		"GOCACHE="+goCache,
		"GOMODCACHE="+benchModCache(),
		"GOSUMDB=off",
	)
	return env
}

func benchModCache() string {
	if path := os.Getenv("GOMODCACHE"); path != "" {
		return path
	}
	return filepath.Join(os.TempDir(), "gomodcache")
}

func importBenchGoMod(wireModulePath, wireReplaceDir string) string {
	return fmt.Sprintf(`module example.com/importbench

go 1.19

require %s v0.0.0

replace %s => %s
`, wireModulePath, wireModulePath, wireReplaceDir)
}

func importBenchWireFile(imports int, wireModulePath string) string {
	var b strings.Builder
	b.WriteString("//go:build wireinject\n\n")
	b.WriteString("package app\n\n")
	b.WriteString("import (\n")
	b.WriteString(fmt.Sprintf("\twire %q\n", wireModulePath))
	for i := 0; i < imports; i++ {
		b.WriteString(fmt.Sprintf("\t%[1]q\n", fmt.Sprintf("example.com/importbench/dep%04d", i)))
	}
	b.WriteString(")\n\n")
	b.WriteString("type App struct{}\n\n")
	b.WriteString("func provideApp(")
	for i := 0; i < imports; i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(fmt.Sprintf("d%d *dep%04d.T", i, i))
	}
	b.WriteString(") *App {\n\treturn &App{}\n}\n\n")
	b.WriteString("func Initialize() *App {\n\twire.Build(wire.NewSet(\n")
	for i := 0; i < imports; i++ {
		b.WriteString(fmt.Sprintf("\t\tdep%04d.Provide,\n", i))
	}
	b.WriteString("\t\tprovideApp,\n\t))\n\treturn nil\n}\n")
	return b.String()
}

func importBenchDepFile(i int, variant string) string {
	switch variant {
	case "body":
		return fmt.Sprintf("package dep%04d\n\ntype T struct{}\n\nfunc Provide() *T {\n\t_ = \"body-edit\"\n\treturn &T{}\n}\n", i)
	case "shape":
		return fmt.Sprintf("package dep%04d\n\ntype T struct{ Extra int }\n\nfunc Provide() *T { return &T{} }\n", i)
	default:
		return fmt.Sprintf("package dep%04d\n\ntype T struct{}\n\nfunc Provide() *T { return &T{} }\n", i)
	}
}

func writeImportBenchWireFile(t *testing.T, root string, imports int, wireModulePath string) {
	t.Helper()
	path := filepath.Join(root, "app", "wire.go")
	if err := os.WriteFile(path, []byte(importBenchWireFile(imports, wireModulePath)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeImportBenchDepFile(t *testing.T, root string, index int, variant string) {
	t.Helper()
	path := filepath.Join(root, fmt.Sprintf("dep%04d", index), "dep.go")
	if err := os.WriteFile(path, []byte(importBenchDepFile(index, variant)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func printImportBenchTable(t *testing.T, rows []importBenchRow) {
	t.Helper()
	fmt.Println("+-----------+-----------+--------------+-------------------+--------------+-------------------+")
	fmt.Println("| repo size | stock     | current cold | current unchanged | cold speedup | unchanged speedup |")
	fmt.Println("+-----------+-----------+--------------+-------------------+--------------+-------------------+")
	for _, row := range rows {
		fmt.Printf("| %-9d | %-9s | %-12s | %-17s | %-12s | %-17s |\n",
			row.imports,
			formatMs(row.stockCold),
			formatMs(row.currentCold),
			formatMs(row.currentWarm),
			formatSpeedup(row.stockCold, row.currentCold),
			formatSpeedup(row.stockCold, row.currentWarm),
		)
	}
	fmt.Println("+-----------+-----------+--------------+-------------------+--------------+-------------------+")
}

func printImportScenarioBenchTable(t *testing.T, rows []importBenchScenarioRow) {
	t.Helper()
	profileWidth := len("profile")
	localWidth := len("local")
	stdlibWidth := len("stdlib")
	externalWidth := len("external")
	changeTypeWidth := len("change type")
	stockWidth := len("stock")
	currentWidth := len("current")
	speedupWidth := len("speedup")
	for _, row := range rows {
		profileWidth = maxInt(profileWidth, len(row.profile))
		localWidth = maxInt(localWidth, len(fmt.Sprintf("%d", row.localCount)))
		stdlibWidth = maxInt(stdlibWidth, len(fmt.Sprintf("%d", row.stdlibCount)))
		externalWidth = maxInt(externalWidth, len(fmt.Sprintf("%d", row.externalCount)))
		changeTypeWidth = maxInt(changeTypeWidth, len(row.name))
		stockWidth = maxInt(stockWidth, len(formatMs(row.stock)))
		currentWidth = maxInt(currentWidth, len(formatMs(row.current)))
		speedupWidth = maxInt(speedupWidth, len(formatSpeedup(row.stock, row.current)))
	}
	sep := fmt.Sprintf("+-%s-+-%s-+-%s-+-%s-+-%s-+-%s-+-%s-+-%s-+",
		strings.Repeat("-", profileWidth),
		strings.Repeat("-", localWidth),
		strings.Repeat("-", stdlibWidth),
		strings.Repeat("-", externalWidth),
		strings.Repeat("-", changeTypeWidth),
		strings.Repeat("-", stockWidth),
		strings.Repeat("-", currentWidth),
		strings.Repeat("-", speedupWidth),
	)
	fmt.Println(sep)
	fmt.Printf("| %*s | %-*s | %-*s | %-*s | %-*s | %-*s | %-*s | %-*s |\n",
		profileWidth, "profile",
		localWidth, "local",
		stdlibWidth, "stdlib",
		externalWidth, "external",
		changeTypeWidth, "change type",
		stockWidth, "stock",
		currentWidth, "current",
		speedupWidth, "speedup",
	)
	fmt.Println(sep)
	for _, row := range rows {
		fmt.Printf("| %*s | %-*d | %-*d | %-*d | %-*s | %-*s | %-*s | %-*s |\n",
			profileWidth, row.profile,
			localWidth, row.localCount,
			stdlibWidth, row.stdlibCount,
			externalWidth, row.externalCount,
			changeTypeWidth, row.name,
			stockWidth, formatMs(row.stock),
			currentWidth, formatMs(row.current),
			speedupWidth, formatSpeedup(row.stock, row.current),
		)
	}
	fmt.Println(sep)
	fmt.Println()
	fmt.Println("change types:")
	fmt.Println("  cold run: first wire gen on a fresh Wire cache for that repo shape")
	fmt.Println("  unchanged rerun: run wire gen again without changing any files")
	fmt.Println("  body-only local edit: change local function body/content without changing imports, types, or constructor signatures")
	fmt.Println("  shape change: change local provider/type shape such as constructor params, fields, or return shape")
	fmt.Println("  import change: add or remove a local import, which can change discovered package shape")
	fmt.Println("  known import toggle: switch back to a previously seen import/shape state in the same repo")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func printScenarioTimingLines(output string) {
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "wire: timing:") {
			continue
		}
		if strings.Contains(line, "loader.custom.root.discovery=") ||
			strings.Contains(line, "loader.discovery.") ||
			strings.Contains(line, "load.packages.load=") ||
			strings.Contains(line, "load.debug") ||
			strings.Contains(line, "loader.custom.typed.artifact_read=") ||
			strings.Contains(line, "loader.custom.typed.artifact_decode=") ||
			strings.Contains(line, "loader.custom.typed.artifact_import_link=") ||
			strings.Contains(line, "loader.custom.typed.artifact_write=") ||
			strings.Contains(line, "loader.custom.typed.root_load.wall=") ||
			strings.Contains(line, "loader.custom.typed.discovery.wall=") ||
			strings.Contains(line, "loader.custom.typed.artifact_hits=") ||
			strings.Contains(line, "loader.custom.typed.artifact_misses=") ||
			strings.Contains(line, "loader.custom.typed.artifact_writes=") ||
			strings.Contains(line, "generate.package.") ||
			strings.Contains(line, "wire.Generate=") ||
			strings.Contains(line, "total=") {
			fmt.Println(line)
		}
	}
}

func formatMs(d time.Duration) string {
	return fmt.Sprintf("%.1fms", float64(d)/float64(time.Millisecond))
}

func formatSpeedup(oldDur, newDur time.Duration) string {
	if newDur == 0 {
		return "inf"
	}
	return fmt.Sprintf("%.2fx", float64(oldDur)/float64(newDur))
}

func TestImportBenchFixtureGenerates(t *testing.T) {
	repoRoot, err := importBenchRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	bin := buildWireBinary(t, repoRoot, "fixture-wire")
	fixture := createImportBenchFixture(t, 10, currentWireModulePath, repoRoot)
	caches := newBenchCaches(t)
	prewarmGoBenchCache(t, fixture, caches)
	_ = runWireBenchCommand(t, bin, fixture, caches)
}

func TestImportBenchUsesStockArchive(t *testing.T) {
	repoRoot, err := importBenchRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	check := exec.Command("git", "cat-file", "-e", stockWireCommit+"^{commit}")
	check.Dir = repoRoot
	if err := check.Run(); err != nil {
		t.Skipf("stock archive commit %s not available in checkout", stockWireCommit)
	}
	stockDir := extractStockWire(t, repoRoot, stockWireCommit)
	if _, err := os.Stat(filepath.Join(stockDir, "cmd", "wire", "main.go")); err != nil {
		t.Fatalf("stock archive missing cmd/wire: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "list", "./cmd/wire")
	cmd.Dir = stockDir
	cmd.Env = benchEnv(t.TempDir(), filepath.Join(t.TempDir(), "gocache"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("stock archive not buildable: %v\n%s", err, out)
	}
}

func importBenchRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(wd, "..", "..")), nil
}
