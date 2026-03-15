package wire

import (
	"archive/tar"
	"bytes"
	"context"
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
	stockWireCommit       = "9c25c9016f6825302537c4efdd5e897976f9c826"
	stockWireModulePath   = "github.com/google/wire"
	currentWireModulePath = "github.com/goforj/wire"
)

type importBenchRow struct {
	imports      int
	stockCold    time.Duration
	currentCold  time.Duration
	currentWarm  time.Duration
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

	sizes := []int{10, 100, 1000}
	rows := make([]importBenchRow, 0, len(sizes))
	for _, n := range sizes {
		stockFixture := createImportBenchFixture(t, n, stockWireModulePath, stockDir)
		currentFixture := createImportBenchFixture(t, n, currentWireModulePath, repoRoot)
		rows = append(rows, importBenchRow{
			imports:     n,
			stockCold:   medianDuration(runColdTrials(t, stockBin, stockFixture, importBenchTrials)),
			currentCold: medianDuration(runColdTrials(t, currentBin, currentFixture, importBenchTrials)),
			currentWarm: medianDuration(runWarmTrials(t, currentBin, currentFixture, importBenchTrials)),
		})
	}
	printImportBenchTable(t, rows)
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
		src := fmt.Sprintf("package dep%04d\n\ntype T struct{}\n\nfunc Provide() *T { return &T{} }\n", i)
		if err := os.WriteFile(filepath.Join(dir, "dep.go"), []byte(src), 0o644); err != nil {
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

func runWireBenchCommand(t *testing.T, bin, pkgDir, home, goCache string) time.Duration {
	t.Helper()
	cmd := exec.Command(bin, "gen")
	cmd.Dir = pkgDir
	cmd.Env = append(benchEnv(home, goCache), "WIRE_LOADER_ARTIFACTS=1")
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	start := time.Now()
	if err := cmd.Run(); err != nil {
		t.Fatalf("run %s in %s: %v\n%s", bin, pkgDir, err, stderr.String())
	}
	return time.Since(start)
}

func runColdTrials(t *testing.T, bin, pkgDir string, trials int) []time.Duration {
	t.Helper()
	durations := make([]time.Duration, 0, trials)
	for i := 0; i < trials; i++ {
		home := t.TempDir()
		goCache := filepath.Join(t.TempDir(), "gocache")
		durations = append(durations, runWireBenchCommand(t, bin, pkgDir, home, goCache))
	}
	return durations
}

func runWarmTrials(t *testing.T, bin, pkgDir string, trials int) []time.Duration {
	t.Helper()
	durations := make([]time.Duration, 0, trials)
	for i := 0; i < trials; i++ {
		home := t.TempDir()
		goCache := filepath.Join(t.TempDir(), "gocache")
		_ = runWireBenchCommand(t, bin, pkgDir, home, goCache)
		durations = append(durations, runWireBenchCommand(t, bin, pkgDir, home, goCache))
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
		"GOMODCACHE=/tmp/gomodcache",
	)
	return env
}

func importBenchGoMod(wireModulePath, wireReplaceDir string) string {
	return fmt.Sprintf(`module example.com/importbench

go 1.26

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
	_ = runWireBenchCommand(t, bin, fixture, t.TempDir(), filepath.Join(t.TempDir(), "gocache"))
}

func TestImportBenchUsesStockArchive(t *testing.T) {
	repoRoot, err := importBenchRepoRoot()
	if err != nil {
		t.Fatal(err)
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
