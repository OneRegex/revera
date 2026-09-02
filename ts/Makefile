# The hosts run straight from the TypeScript sources under Node, so a build is a syntax check of each entry point.
# check runs the type checker, which needs the typescript package that npm install provides.

NODE ?= node
HOSTS = src/driver.ts src/probe_main.ts src/bench_main.ts src/fuzzcase_main.ts

.PHONY: all check test clean

all:
	for f in $(HOSTS); do $(NODE) --check $$f || exit 1; done

check:
	npx --no-install tsc --noEmit -p tsconfig.json

test:
	$(NODE) --test test/

clean:
	rm -rf node_modules
