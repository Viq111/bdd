BIN_DIR := bin
BENCH_DIR := testdata/bench
FIXTURE := $(BENCH_DIR)/fixture-10k.sqlite
MANIFEST := $(BENCH_DIR)/fixture-10k.manifest.json
REPORT := $(BENCH_DIR)/report.json

.PHONY: build fixture bench clean test

build:
	go build -o $(BIN_DIR)/bdd ./cmd/bdd

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

clean:
	rm -rf $(BIN_DIR) $(BENCH_DIR)
