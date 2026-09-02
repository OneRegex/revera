CXX ?= clang++
WARNFLAGS = -Wall -Wextra -Wno-c23-extensions -Wno-parentheses-equality
CXXFLAGS = -std=c++20 -O2 -g -fwrapv $(WARNFLAGS)
CHECKFLAGS = -std=c++20 -O1 -g -fwrapv -fno-sanitize-recover=all -fno-omit-frame-pointer $(WARNFLAGS)
SANFLAGS = $(CHECKFLAGS) -fsanitize=address,undefined
FUZZFLAGS = $(CHECKFLAGS) -fsanitize=fuzzer,address,undefined

all: driver probe api_test bench fuzzcase

driver: driver.cpp engine.cpp engine.hpp host.hpp locale_data.hpp vg.hpp data.bin
	$(CXX) $(CXXFLAGS) -o $@ driver.cpp engine.cpp

probe: probe_main.cpp probe_engine.cpp probe_engine.hpp vg.hpp
	$(CXX) $(CXXFLAGS) -o $@ probe_main.cpp probe_engine.cpp

api_test: api_test.cpp revera.cpp revera.hpp locale_data.hpp engine.cpp engine.hpp vg.hpp data.bin
	$(CXX) $(CXXFLAGS) -o $@ api_test.cpp revera.cpp engine.cpp

bench: bench.cpp engine.cpp engine.hpp host.hpp locale_data.hpp vg.hpp data.bin
	$(CXX) $(CXXFLAGS) -o $@ bench.cpp engine.cpp

fuzzcase: fuzz.cpp engine.cpp engine.hpp host.hpp locale_data.hpp vg.hpp data.bin
	$(CXX) $(CXXFLAGS) -DREVERA_FUZZ_STANDALONE -o $@ fuzz.cpp engine.cpp

sanitize: driver-asan probe-asan fuzzcase-asan

driver-asan: driver.cpp engine.cpp engine.hpp host.hpp locale_data.hpp vg.hpp data.bin
	$(CXX) $(SANFLAGS) -o $@ driver.cpp engine.cpp

probe-asan: probe_main.cpp probe_engine.cpp probe_engine.hpp vg.hpp
	$(CXX) $(SANFLAGS) -o $@ probe_main.cpp probe_engine.cpp

fuzzcase-asan: fuzz.cpp engine.cpp engine.hpp host.hpp locale_data.hpp vg.hpp data.bin
	$(CXX) $(SANFLAGS) -DREVERA_FUZZ_STANDALONE -o $@ fuzz.cpp engine.cpp

# Apple clang ships no libFuzzer runtime, so this target links only with clang on Linux.
libfuzzer: fuzz.cpp engine.cpp engine.hpp host.hpp locale_data.hpp vg.hpp data.bin
	$(CXX) $(FUZZFLAGS) -o $@ fuzz.cpp engine.cpp

.PHONY: all generate test sanitize clean
test: api_test
	./api_test

generate:
	cd ../../dev && go run ./cmd/generate -target cpp

clean:
	rm -f driver probe api_test bench fuzzcase driver-asan probe-asan fuzzcase-asan libfuzzer
