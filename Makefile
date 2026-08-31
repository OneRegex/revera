CC ?= cc
CFLAGS ?= -O2
CPPFLAGS ?=
GENERATION_TARGETS ?= all

WARNINGS = -Wall -Wextra -Wpedantic -Werror

.PHONY: all clean test generate check-generated

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

clean:
	rm -f build/test_locale build/test_locale_internal
