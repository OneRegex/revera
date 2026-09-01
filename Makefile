CC ?= cc
CFLAGS ?= -O2
CPPFLAGS ?=
GENERATION_TARGETS ?= all

WARNINGS = -Wall -Wextra -Wpedantic -Werror

.PHONY: all clean test generate check-generated lint conform bench size profile

all: build/test_locale build/test_locale_internal

build:
	mkdir -p build

build/test_locale: src/rv_locale.c src/rv_locale.h src/rv_locale_data.inc \
		tests/test_locale.c | build
	$(CC) $(CPPFLAGS) $(CFLAGS) $(WARNINGS) -std=c11 -Isrc \
		src/rv_locale.c tests/test_locale.c -o $@

build/test_locale_internal: src/rv_locale.c src/rv_locale.h \
		src/rv_locale_data.inc tests/test_locale_internal.c | build
	$(CC) $(CPPFLAGS) $(CFLAGS) $(WARNINGS) -std=c11 -Isrc \
		tests/test_locale_internal.c -o $@

test: build/test_locale build/test_locale_internal
	./build/test_locale
	./build/test_locale_internal

generate:
	cd go1 && go run ./cmd/revera generate -target $(GENERATION_TARGETS)

check-generated:
	cd go1 && go run ./cmd/revera check-generated -target $(GENERATION_TARGETS)

lint:
	cd go0 && golangci-lint run ./...
	cd go1 && golangci-lint run ./...
	cd rust1 && cargo clippy --all-targets

# CONFORM_FLAGS passes options through, for example CONFORM_FLAGS="-backend ../cpp1 -lean".
conform:
	cd go1 && go run ./cmd/revera conform $(CONFORM_FLAGS)

# Cross-language benchmarks; BENCH_FLAGS passes options through, for example BENCH_FLAGS="-go0 -only hard/".
bench:
	mkdir -p tmp
	cd go1 && go run ./cmd/bench -build -tsv ../tmp/bench-results.tsv $(BENCH_FLAGS)

size:
	cd go1 && go run ./cmd/bench size

# CPU and allocation profiles of the Go engine over the shared cases, in tmp/.
profile:
	mkdir -p tmp
	cd go1 && go test ./revera -run '^$$' -bench 'BenchmarkEngine' -benchmem \
		-cpuprofile ../tmp/cpu.pprof -memprofile ../tmp/mem.pprof -o ../tmp/revera.test
	cd go1 && go tool pprof -top -nodecount 25 ../tmp/revera.test ../tmp/cpu.pprof

clean:
	rm -f build/test_locale build/test_locale_internal
