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
deps: node_modules
node_modules: package.json
	npm install
	@touch node_modules

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
