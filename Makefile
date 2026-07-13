BIN_DIR := bin
DIST_DIR := dist
BENCH_DIR := testdata/bench
FIXTURE := $(BENCH_DIR)/fixture-10k.sqlite
MANIFEST := $(BENCH_DIR)/fixture-10k.manifest.json
REPORT := $(BENCH_DIR)/report.json

.PHONY: build dist release fixture bench clean test fuzz-short

# Per-target duration for fuzz-short: short enough that the full set stays
# CI-friendly. See docs/security-review.md for longer, exploratory local
# fuzz runs.
FUZZTIME := 3s

# Falls back to "dev" outside a git checkout (e.g. an extracted source
# tarball with no .git directory).
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

build:
	go build -trimpath -ldflags "-X main.version=$(VERSION)" -o $(BIN_DIR)/bdd ./cmd/bdd

# Cross-compile and package release archives + checksums for every
# supported platform. See docs/release.md for the full release procedure.
dist release:
	VERSION=$(VERSION) ./scripts/release.sh

# Regenerate the 10k-card benchmark fixture. Deterministic: same seed and
# card count always produce a byte-identical SQLite file.
fixture:
	mkdir -p $(BENCH_DIR)
	rm -f $(FIXTURE) $(MANIFEST)
	go run ./cmd/bddfixture -out $(FIXTURE) -manifest $(MANIFEST) -cards 10000 -seed 42

# Run the section 7 subprocess latency benchmark against the fixture and
# write a machine-readable report. See docs/benchmark.md for methodology
# and reference-machine assumptions.
bench: build fixture
	go run ./cmd/bddbench -binary $(BIN_DIR)/bdd -manifest $(MANIFEST) -iterations 50 -warmup 5 -out $(REPORT)
	@echo "report written to $(REPORT)"

test:
	go build ./...
	go vet ./...
	go test ./...
	$(MAKE) fuzz-short

# Short, seed-corpus-plus-a-few-seconds-of-mutation smoke run for every
# fuzz target (bd bdd-ifik / docs/security-review.md), safe to run on every
# CI build. Each target runs standalone (go test -fuzz allows only one fuzz
# function per invocation).
fuzz-short:
	go test -run '^$$' -fuzz '^FuzzParseStatusCustom$$' -fuzztime $(FUZZTIME) .
	go test -run '^$$' -fuzz '^FuzzParseTypesCustom$$' -fuzztime $(FUZZTIME) .
	go test -run '^$$' -fuzz '^FuzzCreateCardDecode$$' -fuzztime $(FUZZTIME) .
	go test -run '^$$' -fuzz '^FuzzUpdateCardDecode$$' -fuzztime $(FUZZTIME) .
	go test -run '^$$' -fuzz '^FuzzCycleDetection$$' -fuzztime $(FUZZTIME) .
	go test -run '^$$' -fuzz '^FuzzParseGlobalFlags$$' -fuzztime $(FUZZTIME) ./internal/cli
	# -parallel 2: FuzzRun spins up a full SQLite workspace per execution;
	# the default worker count (GOMAXPROCS) made the fuzz coordinator's
	# worker handshake flaky (spurious "context deadline exceeded") under
	# load in short runs. See docs/security-review.md.
	go test -run '^$$' -fuzz '^FuzzRun$$' -fuzztime $(FUZZTIME) -parallel 2 ./internal/cli

clean:
	rm -rf $(BIN_DIR) $(DIST_DIR) $(BENCH_DIR)
