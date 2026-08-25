CXX = clang++
CXXFLAGS = -std=c++20 -O2 -g -fwrapv -Wall -Wextra -Wno-c23-extensions -Wno-parentheses-equality

all: driver probe

driver: driver.cpp engine.cpp engine.hpp vg.hpp data.bin
	$(CXX) $(CXXFLAGS) -o $@ driver.cpp engine.cpp

probe: probe_main.cpp probe_engine.cpp probe_engine.hpp vg.hpp
	$(CXX) $(CXXFLAGS) -o $@ probe_main.cpp probe_engine.cpp

.PHONY: all generate clean
generate:
	cd ../go1 && go run ./cmd/json2cpp -hpp ../cpp1/engine.hpp -cpp ../cpp1/engine.cpp revera.vego.json
	cd ../go1 && go run ./cmd/json2cpp -hpp ../cpp1/probe_engine.hpp -cpp ../cpp1/probe_engine.cpp probe.vego.json

clean:
	rm -f driver probe
