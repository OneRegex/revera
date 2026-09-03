# The hosts run straight from the TypeScript sources under Node, so a build is a syntax check of each entry point.
# check runs the type checker, which needs the typescript and @types/node packages that npm ci installs.
# A tsc found elsewhere on the PATH is not enough, since tsconfig.json asks for the node type definitions.

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
	rm -rf dist node_modules
