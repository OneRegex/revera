CC ?= clang
WARNFLAGS = -Wall -Wextra -Wno-parentheses-equality -Wno-c23-extensions
CFLAGS = -std=c11 -O2 -g -fwrapv $(WARNFLAGS)
CHECKFLAGS = -std=c11 -O1 -g -fwrapv -fno-sanitize-recover=all -fno-omit-frame-pointer $(WARNFLAGS)
SANFLAGS = $(CHECKFLAGS) -fsanitize=address,undefined
FUZZFLAGS = $(CHECKFLAGS) -fsanitize=fuzzer,address,undefined

all: driver probe api_test bench fuzzcase

driver: driver.c engine.c engine.h host.h locale_data.h vg.h data.bin
	$(CC) $(CFLAGS) -o $@ driver.c engine.c

probe: probe_main.c probe_engine.c probe_engine.h vg.h
	$(CC) $(CFLAGS) -o $@ probe_main.c probe_engine.c

api_test: api_test.c revera.c revera.h locale_data.h engine.c engine.h vg.h data.bin
	$(CC) $(CFLAGS) -o $@ api_test.c revera.c engine.c

bench: bench.c engine.c engine.h host.h locale_data.h vg.h data.bin
	$(CC) $(CFLAGS) -o $@ bench.c engine.c

fuzzcase: fuzz.c engine.c engine.h host.h locale_data.h vg.h data.bin
	$(CC) $(CFLAGS) -DREVERA_FUZZ_STANDALONE -o $@ fuzz.c engine.c

sanitize: driver-asan probe-asan fuzzcase-asan

driver-asan: driver.c engine.c engine.h host.h locale_data.h vg.h data.bin
	$(CC) $(SANFLAGS) -o $@ driver.c engine.c

probe-asan: probe_main.c probe_engine.c probe_engine.h vg.h
	$(CC) $(SANFLAGS) -o $@ probe_main.c probe_engine.c

fuzzcase-asan: fuzz.c engine.c engine.h host.h locale_data.h vg.h data.bin
	$(CC) $(SANFLAGS) -DREVERA_FUZZ_STANDALONE -o $@ fuzz.c engine.c

# Apple clang ships no libFuzzer runtime, so this target links only with clang on Linux.
libfuzzer: fuzz.c engine.c engine.h host.h locale_data.h vg.h data.bin
	$(CC) $(FUZZFLAGS) -o $@ fuzz.c engine.c

.PHONY: all generate test sanitize clean
test: api_test
	./api_test

generate:
	cd ../../dev && go run ./cmd/generate -target c

clean:
	rm -f driver probe api_test bench fuzzcase driver-asan probe-asan fuzzcase-asan libfuzzer
