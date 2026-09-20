# pokedex-tui — the same commands locally and in CI.
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

.PHONY: help
help:
	@echo "pokedex-tui"
	@echo
	@echo "  make build      compile"
	@echo "  make test       vet and tests (what CI runs)"
	@echo "  make run        run it (make run ARGS='--help')"
	@echo "  make clean      remove build output"
ifneq (,$(wildcard Makefile.local))
	@$(MAKE) --no-print-directory help-local 2>/dev/null || true
endif

# -o, and the package rather than ./... - `go build ./...` compiles
# every package and THROWS THE BINARY AWAY, so `make build` looked like
# it worked and left nothing to run. It writes ./pokedex-tui,
# which is what `make clean` removes and what you type to run it.
.PHONY: build
build:
	go build -o pokedex-tui .

# -race because CI runs it that way, and a data race that only appears
# under load is the kind of thing you want to find here instead.
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
	rm -f pokedex-tui

-include Makefile.local
