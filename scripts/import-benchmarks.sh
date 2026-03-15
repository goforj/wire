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

Commands:
  table  Print the 10/100/1000 import stock-vs-current benchmark table.
EOF
}

case "${1:-}" in
  table)
    WIRE_IMPORT_BENCH_TABLE=1 go test ./internal/wire -run TestPrintImportScaleBenchmarkTable -count=1 -v
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
