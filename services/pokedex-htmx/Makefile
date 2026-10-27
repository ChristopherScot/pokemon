# pokedex-htmx
#
# Include Makefile.local for service-specific setup; it survives regenerate.

SHELL := /bin/bash
.DEFAULT_GOAL := help

PORT ?= 3000
export PORT

.PHONY: help
help:
	@echo "pokedex-htmx"
	@echo
	@echo "  make deps       install dependencies"
	@echo "  make build      the production bundle"
	@echo "  make test       typecheck and tests (what CI runs)"
	@echo "  make run        start it on :$(PORT)"
	@echo "  make regen      regenerate code derived from the spec"
	@echo "  make clean      remove build output"
ifneq (,$(wildcard Makefile.local))
	@$(MAKE) --no-print-directory help-local 2>/dev/null || true
endif

.PHONY: deps
deps: client node_modules
# Order-only: under `make -j` an unordered prerequisite list lets the
# service install while the client is mid-build, resolving `main` into
# a half-written dist/.
node_modules: package.json | client
	npm install
	@touch node_modules

# A generated client ships TypeScript and compiles to dist/, which npm
# resolves `main` into. A `file:` dependency's devDependencies are not
# installed by the consumer, so its own prepare/prepack never runs -
# nothing builds it and tsc reports the package as missing entirely.
#
# A prerequisite of deps rather than of node_modules, because CI runs
# `npm ci` itself before `make test`. node_modules then already exists,
# that target is satisfied, and anything hanging off it never runs.
#
# Both layouts: clients/ts beside the service in a single-service repo,
# and a sibling service's in a monorepo. A service that consumes no
# client matches neither and does nothing. The glob stays literal and
# the -f guard rejects it, which holds because SHELL is bash above.
#
# `|| exit 1` because a for loop exits with the LAST iteration's
# status: without it a client that fails to build in a monorepo with
# two of them is silently skipped, and surfaces later as the same
# confusing tsc error this target exists to prevent.
#
# npm ci, matching the Dockerfile: the client ships a committed
# package-lock.json, and npm install is free to rewrite it.
.PHONY: client
client:
	@for c in ./clients/ts ../*/clients/ts; do \
	  [ -f "$$c/package.json" ] || continue; \
	  ( cd "$$c" && npm ci --no-audit --no-fund && npm run build ) || exit 1; \
	done

.PHONY: build
build: deps
	npm run build

.PHONY: test
test: deps
	npm run typecheck --if-present
	npm test

.PHONY: run
run: deps
	npm start

# What the image runs, from an empty directory - the image copies only
# dist/server.js, so a file read at runtime resolves here and 500s.
.PHONY: run-dist
run-dist: build
	@tmp=$$(mktemp -d) && cp dist/server.js "$$tmp/" && cd "$$tmp" && PORT=$(PORT) node server.js

.PHONY: regen
regen:
	@command -v homelabctl >/dev/null || { \
		echo "homelabctl not found - https://github.com/ChristopherScot/homelabctl"; exit 1; }
	@homelabctl regen

.PHONY: clean
clean:
	rm -rf dist

-include Makefile.local
