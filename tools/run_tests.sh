#!/usr/bin/env bash
# Fast test lanes for coding-agent feedback loops. See README.md.
#
# Lanes:
#   quick       unit + in-process CLI tests only. No production-length lock
#               waits, no cross-platform release builds, no external
#               migration e2e, no fuzzing. Target: well under a minute.
#   integration adds subprocess CLI + migration coverage (./e2e).
#   release     everything, including archive reproducibility, the full
#               fuzz budget, and long concurrency/lock tests.
#
# tools/build.sh is left untouched; this script is the lane runner.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

FUZZTIME=3s

usage() {
  echo "usage: $0 --lane <quick|integration|release>" >&2
  exit 2
}

fuzz_short() {
  go test -run '^$' -fuzz '^FuzzParseStatusCustom$' -fuzztime "$FUZZTIME" .
  go test -run '^$' -fuzz '^FuzzParseTypesCustom$' -fuzztime "$FUZZTIME" .
  go test -run '^$' -fuzz '^FuzzCreateCardDecode$' -fuzztime "$FUZZTIME" .
  go test -run '^$' -fuzz '^FuzzUpdateCardDecode$' -fuzztime "$FUZZTIME" .
  go test -run '^$' -fuzz '^FuzzCycleDetection$' -fuzztime "$FUZZTIME" .
  go test -run '^$' -fuzz '^FuzzParseGlobalFlags$' -fuzztime "$FUZZTIME" ./internal/cli
  go test -run '^$' -fuzz '^FuzzRun$' -fuzztime "$FUZZTIME" -parallel 2 ./internal/cli
}

# non_e2e_packages lists every package except ./e2e, which builds real
# binaries and execs them as subprocesses (subprocess CLI + migration
# coverage belongs to the integration and release lanes).
non_e2e_packages() {
  go list ./... | grep -v '^github.com/viq111/bdd/e2e$'
}

lane="${2:-}"
case "${1:-}" in
  --lane)
    ;;
  *)
    usage
    ;;
esac
[[ -z "$lane" ]] && usage

case "$lane" in
  quick)
    go build ./...
    go vet ./...
    go test -short $(non_e2e_packages)
    ;;
  integration)
    go build ./...
    go vet ./...
    go test -short ./...
    ;;
  release)
    go build ./...
    go vet ./...
    go test ./...
    fuzz_short
    ;;
  *)
    usage
    ;;
esac
