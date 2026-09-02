GENERATION_TARGETS ?= all

.PHONY: all test generate check-generated lint conform bench size profile licenses dist clean

# The locale runtime and its C tests.
all:
	$(MAKE) -C locale all

test:
	$(MAKE) -C locale test

# The Vego IR and the generated engines, rendered by vegoc into tmp/ and installed in one transaction.
generate:
	cd dev && go run ./cmd/generate -target $(GENERATION_TARGETS)

check-generated:
	cd dev && go run ./cmd/generate -check -target $(GENERATION_TARGETS)

lint:
	cd go && golangci-lint run ./...
	cd vego && golangci-lint run ./...
	cd dev && golangci-lint run ./...
	cd rust && cargo clippy --workspace --all-targets
	cd ts && npx --no-install tsc --noEmit -p tsconfig.json

# CONFORM_FLAGS passes options through, for example CONFORM_FLAGS="-backend ../native/cpp -lean".
conform:
	cd dev && go run ./cmd/conform $(CONFORM_FLAGS)

# Cross-language benchmarks; BENCH_FLAGS passes options through, for example BENCH_FLAGS="-reference -only hard/".
bench:
	mkdir -p tmp
	cd dev && go run ./cmd/bench -build -tsv ../tmp/bench-results.tsv $(BENCH_FLAGS)

size:
	cd dev && go run ./cmd/bench size

# CPU and allocation profiles of the Go engine over the shared cases, in tmp/.
profile:
	mkdir -p tmp
	cd dev && go test ./internal/protocol -run '^$$' -bench 'BenchmarkEngine' -benchmem \
		-cpuprofile ../tmp/cpu.pprof -memprofile ../tmp/mem.pprof -o ../tmp/revera.test
	cd dev && go tool pprof -top -nodecount 25 ../tmp/revera.test ../tmp/cpu.pprof

# Copies of LICENSE and LICENSES/ in every package directory.
licenses:
	sh dev/sync-licenses.sh

# The release archives, the IR files and their manifest, in tmp/dist/, built from the recorded commit.
dist:
	cd dev && go run ./cmd/dist

clean:
	$(MAKE) -C locale clean
