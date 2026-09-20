# pokedex-mobile — the same commands locally and in CI.
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
	@echo "pokedex-mobile"
	@echo
	@echo "  make build      compile"
	@echo "  make test       vet and tests (what CI runs)"
	@echo "  make run        run it (make run ARGS='--help')"
	@echo "  make apk        build an installable Android APK"
	@echo "  make clean      remove build output"
ifneq (,$(wildcard Makefile.local))
	@$(MAKE) --no-print-directory help-local 2>/dev/null || true
endif

.PHONY: build
build:
	go build ./...

# -race because CI runs it that way, and a data race that only appears
# under load is the kind of thing you want to find here instead.
.PHONY: test
test:
	go vet ./...
	go test -race ./...

.PHONY: run
run:
	go run . $(ARGS)

.PHONY: apk
apk:
	@command -v gogio >/dev/null || go install gioui.org/cmd/gogio@latest
	gogio -target android -appid com.github.christopherscot.pokemon.services.pokedex_mobile -name 'Pokedex Mobile' \
		-version 0.0.1.1 -minsdk 31 -o pokedex-mobile.apk .
	@echo "built pokedex-mobile.apk - CI stamps the real version; this one is for a phone on your desk"

# gogio needs a JDK, and a modern one: JDK 8's d8 cannot load what it
# emits. CI pins 17.
.PHONY: apk-check
apk-check:
	@java -version 2>&1 | head -1
	@command -v gogio >/dev/null && echo "gogio present" || echo "gogio missing (make apk installs it)"

.PHONY: clean
clean:
	go clean
	rm -f pokedex-mobile pokedex-mobile.apk

-include Makefile.local
