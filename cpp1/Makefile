CXX = clang++
CXXFLAGS = -std=c++20 -O2 -g -fwrapv -Wall -Wextra -Wno-c23-extensions -Wno-parentheses-equality

all: driver probe api_test

driver: driver.cpp engine.cpp engine.hpp vg.hpp data.bin
	$(CXX) $(CXXFLAGS) -o $@ driver.cpp engine.cpp

probe: probe_main.cpp probe_engine.cpp probe_engine.hpp vg.hpp
	$(CXX) $(CXXFLAGS) -o $@ probe_main.cpp probe_engine.cpp

api_test: api_test.cpp revera.cpp revera.hpp engine.cpp engine.hpp vg.hpp data.bin
	$(CXX) $(CXXFLAGS) -o $@ api_test.cpp revera.cpp engine.cpp

.PHONY: all generate test clean
test: api_test
	./api_test

generate:
	cd ../go1 && go run ./cmd/revera generate -target cpp

clean:
	rm -f driver probe api_test
