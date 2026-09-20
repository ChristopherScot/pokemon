# pokedex-htmx — the same commands locally and in CI.
#
#   make          what you can do
#   make test     what CI runs, so green here means green there
#   make run      start it
#
# Include Makefile.local for anything specific to this service - a
# database, a seed step, extra env - and it will not be overwritten when
# this file is regenerated.

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
	@echo "  make clean      remove build output"
ifneq (,$(wildcard Makefile.local))
	@$(MAKE) --no-print-directory help-local 2>/dev/null || true
endif

.PHONY: deps
deps: node_modules
node_modules: package.json
	npm install
	@touch node_modules

.PHONY: build
build: deps
	npm run build

# Exactly the Typecheck and Test steps from .github/workflows.
.PHONY: test
test: deps
	npm run typecheck --if-present
	npm test

# npm start, not `node server.ts`: the script carries flags the server
# needs, and running the file directly fails in ways that look like a
# code problem.
.PHONY: run
run: deps
	npm start

# What the image actually runs, from an empty directory - the image
# copies dist/server.js and nothing else, so a file read at runtime
# resolves here and 500s in production.
.PHONY: run-dist
run-dist: build
	@tmp=$$(mktemp -d) && cp dist/server.js "$$tmp/" && cd "$$tmp" && PORT=$(PORT) node server.js

.PHONY: clean
clean:
	rm -rf dist

-include Makefile.local
