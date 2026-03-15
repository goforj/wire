#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

export GOCACHE="${GOCACHE:-/tmp/gocache}"
export GOMODCACHE="${GOMODCACHE:-/tmp/gomodcache}"

usage() {
  cat <<'EOF'
Usage:
  scripts/incremental-scenarios.sh test
  scripts/incremental-scenarios.sh matrix
  scripts/incremental-scenarios.sh table
  scripts/incremental-scenarios.sh budgets
  scripts/incremental-scenarios.sh bench
  scripts/incremental-scenarios.sh large-table
  scripts/incremental-scenarios.sh large-breakdown
  scripts/incremental-scenarios.sh report
  scripts/incremental-scenarios.sh all

Commands:
  test             Run the full internal/wire test suite.
  matrix           Run the incremental scenario matrix correctness test.
  table            Print the incremental scenario timing table.
  budgets          Enforce the incremental scenario performance budgets.
  bench            Run the incremental scenario benchmark suite.
  large-table      Print the large-repo comparison timing table.
  large-breakdown  Print the large-repo shape-change breakdown table.
  report           Run the main timing report: scenario table, budgets, and large-repo table.
  all              Run matrix, table, budgets, and the large-repo table in sequence.
EOF
}

print_section() {
  local title="$1"
  printf '\n== %s ==\n' "$title"
}

print_test_table() {
  local output_file="$1"
  awk '
    /^\+[-+]+\+$/ { in_table=1 }
    in_table && !/^--- PASS:/ && !/^PASS$/ && !/^ok[[:space:]]/ { print }
    /^--- PASS:/ && in_table { exit }
  ' "$output_file"
}

run_test_table() {
  local env_var="$1"
  local test_name="$2"
  local output_file
  output_file="$(mktemp)"
  env "$env_var"=1 go test ./internal/wire -run "$test_name" -count=1 -v >"$output_file"
  print_test_table "$output_file"
  rm -f "$output_file"
}

run_test() {
  go test ./internal/wire -count=1
}

run_matrix() {
  go test ./internal/wire -run TestGenerateIncrementalScenarioMatrix -count=1
}

run_table() {
  run_test_table WIRE_BENCH_SCENARIOS TestPrintIncrementalScenarioBenchmarkTable
}

run_budgets() {
  WIRE_PERF_BUDGETS=1 go test ./internal/wire -run TestIncrementalScenarioPerformanceBudgets -count=1 >/dev/null
  echo "PASS"
}

run_bench() {
  go test ./internal/wire -run '^$' -bench BenchmarkGenerateIncrementalScenarioMatrix -benchmem -count=1
}

run_large_table() {
  run_test_table WIRE_BENCH_TABLE TestPrintLargeRepoBenchmarkComparisonTable
}

run_large_breakdown() {
  run_test_table WIRE_BENCH_BREAKDOWN TestPrintLargeRepoShapeChangeBreakdownTable
}

run_report() {
  print_section "Scenario Timing Table"
  run_table
  print_section "Scenario Performance Budgets"
  run_budgets
  print_section "Large Repo Comparison Table"
  run_large_table
}

cmd="${1:-}"
case "$cmd" in
  test)
    run_test
    ;;
  matrix)
    run_matrix
    ;;
  table)
    run_table
    ;;
  budgets)
    run_budgets
    ;;
  bench)
    run_bench
    ;;
  large-table)
    run_large_table
    ;;
  large-breakdown)
    run_large_breakdown
    ;;
  report)
    run_report
    ;;
  all)
    run_matrix
    run_report
    ;;
  ""|-h|--help|help)
    usage
    ;;
  *)
    echo "Unknown command: $cmd" >&2
    usage >&2
    exit 1
    ;;
esac
