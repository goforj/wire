#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

export GOCACHE="${GOCACHE:-/tmp/gocache}"
export GOMODCACHE="${GOMODCACHE:-/tmp/gomodcache}"

usage() {
  cat <<'EOF'
Usage:
  scripts/import-benchmarks.sh table
  scripts/import-benchmarks.sh scenarios [profile]
  scripts/import-benchmarks.sh breakdown

Commands:
  table      Print the 10/100/1000 import stock-vs-current benchmark table.
  scenarios  Print the stock-vs-current change-type scenario table.
             Optional profiles: local, local-high, external.
  breakdown  Print a focused 1000-import cold/unchanged breakdown.
EOF
}

case "${1:-}" in
  table)
    WIRE_IMPORT_BENCH_TABLE=1 go test ./internal/wire -run TestPrintImportScaleBenchmarkTable -count=1 -v
    ;;
  scenarios)
    if [[ -n "${2:-}" ]]; then
      WIRE_IMPORT_BENCH_SCENARIOS=1 WIRE_IMPORT_BENCH_PROFILE="${2}" go test ./internal/wire -run TestPrintImportScenarioBenchmarkTable -count=1 -v
    else
      WIRE_IMPORT_BENCH_SCENARIOS=1 go test ./internal/wire -run TestPrintImportScenarioBenchmarkTable -count=1 -v
    fi
    ;;
  breakdown)
    WIRE_IMPORT_BENCH_BREAKDOWN=1 go test ./internal/wire -run TestPrintImportScaleBenchmarkBreakdown -count=1 -v
    ;;
  ""|-h|--help|help)
    usage
    ;;
  *)
    echo "Unknown command: ${1}" >&2
    usage >&2
    exit 1
    ;;
esac
