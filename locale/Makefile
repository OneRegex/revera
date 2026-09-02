CC ?= cc
CFLAGS ?= -O2
CPPFLAGS ?=

WARNINGS = -Wall -Wextra -Wpedantic -Werror

.PHONY: all test clean

all: build/test_locale build/test_locale_internal

build:
	mkdir -p build

build/test_locale: rv_locale.c rv_locale.h rv_locale_data.inc tests/test_locale.c | build
	$(CC) $(CPPFLAGS) $(CFLAGS) $(WARNINGS) -std=c11 -I. \
		rv_locale.c tests/test_locale.c -o $@

build/test_locale_internal: rv_locale.c rv_locale.h rv_locale_data.inc \
		tests/test_locale_internal.c | build
	$(CC) $(CPPFLAGS) $(CFLAGS) $(WARNINGS) -std=c11 -I. \
		tests/test_locale_internal.c -o $@

test: build/test_locale build/test_locale_internal
	./build/test_locale
	./build/test_locale_internal

clean:
	rm -rf build
