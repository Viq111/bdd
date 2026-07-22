#!/usr/bin/env bash
# Build and maintain local bdd binaries. See README.md and docs/release.md.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

BIN_DIR="bin"
BENCH_DIR="testdata/bench"
FIXTURE="$BENCH_DIR/fixture-10k.sqlite"
MANIFEST="$BENCH_DIR/fixture-10k.manifest.json"
REPORT="$BENCH_DIR/report.json"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unspecified)}"
FUZZTIME=3s

build() {
  mkdir -p "$BIN_DIR"
  go build -trimpath -ldflags "-X main.version=$VERSION -X main.commit=$COMMIT" -o "$BIN_DIR/bdd" ./cmd/bdd
  go build -trimpath -ldflags "-X main.version=$VERSION -X main.commit=$COMMIT" -o "$BIN_DIR/bdd-migration" ./tools/migrate/cmd/bdd-migration
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

fixture() {
  mkdir -p "$BENCH_DIR"
  rm -f "$FIXTURE" "$MANIFEST"
  go run ./cmd/bddfixture -out "$FIXTURE" -manifest "$MANIFEST" -cards 10000 -seed 42
}

case "${1:-}" in
  "") build ;;
  --install)
    build
    mkdir -p "$HOME/.local/bin"
    cp "$BIN_DIR/bdd" "$BIN_DIR/bdd-migration" "$HOME/.local/bin/"
    ;;
  --test)
    go build ./...
    go vet ./...
    go test ./...
    fuzz_short
    ;;
  --fuzz-short) fuzz_short ;;
  --fixture) fixture ;;
  --bench)
    build
    fixture
    go run ./cmd/bddbench -binary "$BIN_DIR/bdd" -manifest "$MANIFEST" -iterations 50 -warmup 5 -out "$REPORT"
    echo "report written to $REPORT"
    ;;
  --dist|--release) VERSION="$VERSION" ./scripts/release.sh ;;
  --clean) rm -rf "$BIN_DIR" dist "$BENCH_DIR" ;;
  *)
    echo "usage: $0 [--install|--test|--fuzz-short|--fixture|--bench|--dist|--release|--clean]" >&2
    exit 2
    ;;
esac
