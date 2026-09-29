# Local targets mirror CI. Service-specific overrides go in Makefile.local (untracked, not regenerated).

SHELL := /bin/bash
.DEFAULT_GOAL := help

.PHONY: help
help:
	@echo "pokedex-cli"
	@echo
	@echo "  make build      compile"
	@echo "  make test       vet and tests (what CI runs)"
	@echo "  make run        run it (make run ARGS='--help')"
	@echo "  make clean      remove build output"
ifneq (,$(wildcard Makefile.local))
	@$(MAKE) --no-print-directory help-local 2>/dev/null || true
endif

# `go build ./...` throws the binary away; -o keeps it.
.PHONY: build
build:
	go build -o pokedex-cli .

.PHONY: test
test:
	go vet ./...
	go test -race ./...

.PHONY: run
run:
	go run . $(ARGS)

.PHONY: clean
clean:
	go clean
	rm -f pokedex-cli

-include Makefile.local
