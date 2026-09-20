# One way to build, test and run every service, whatever it is written
# in and wherever it runs.
#
#   make            what you can do
#   make setup      check your tools, install deps, start postgres
#   make build      build everything
#   make test       test everything
#   make run        the API and both front ends, together
#
# Each target works on one service too: `make test-pokedex`,
# `make run-web`. The same targets are what CI runs, so "it passes
# locally" means something.
#
# Go services and node services need different commands; this knows
# which is which by looking for go.mod or package.json, so adding a
# service does not mean editing this file.

SHELL := /bin/bash
.DEFAULT_GOAL := help

SERVICES := $(notdir $(wildcard services/*))
GO_SERVICES := $(foreach s,$(SERVICES),$(if $(wildcard services/$(s)/go.mod),$(s)))
NODE_SERVICES := $(foreach s,$(SERVICES),$(if $(wildcard services/$(s)/package.json),$(s)))

# The services homelabctl manages, which is not the same as the Go ones:
# it scaffolds both node front ends, and the CLI, TUI and mobile client
# were written by hand and have no config.yaml. Keyed on that file
# rather than a list, so this does not need editing when one gains or
# loses it.
MANAGED_SERVICES := $(foreach s,$(SERVICES),$(if $(wildcard services/$(s)/config.yaml),$(s)))

# Where postgres is. Overridable, because CI's is on 5432 and yours is
# on 15432 so it cannot collide with a postgres you already run.
PGPORT ?= 15432
DATABASE_URL ?= postgres://postgres:test@127.0.0.1:$(PGPORT)/pokedex?sslmode=disable
export DATABASE_URL

# Ports. Overridable in one place, because 3000 is a popular port and
# whatever else you run probably wants it too:
#   make run PORT_API=19000 PORT_WEB=19001 PORT_HTMX=19002
PORT_API  ?= 3000
PORT_WEB  ?= 3001
PORT_HTMX ?= 3002

# The API the front ends and the CLI talk to.
POKEDEX_URL ?= http://127.0.0.1:$(PORT_API)
export POKEDEX_URL

# CI sets CI=true. Locally it is empty. The only thing that changes is
# whether we start postgres ourselves: CI brings its own as a service
# container, and starting a second one would bind a port twice.
ifeq ($(CI),)
  PG_UP := docker compose -f services/pokedex/compose.yaml up -d --wait
else
  PG_UP := @echo "CI provides postgres"
endif

.PHONY: help
help:
	@echo "pokemon — one repo, six services"
	@echo
	@echo "  make setup        check tools, install deps, start postgres"
	@echo "  make build        build every service"
	@echo "  make test         test every service"
	@echo "  make run          API + both front ends together"
	@echo "  make db           just start postgres"
	@echo "  make seed         load the 100 pokemon"
	@echo "  make regen        regenerate everything derived from openapi.yml and SQL"
	@echo "  make clean        stop postgres, remove build output"
	@echo
	@echo "  per service:  make test-pokedex   make build-htmx   make run-web"
	@echo
	@echo "services: $(SERVICES)"

# --- setup ------------------------------------------------------------

.PHONY: setup
setup: tools deps db seed
	@echo
	@echo "Ready. Now: make run"

.PHONY: tools
tools:
	@fail=0; \
	command -v go      >/dev/null || { echo "MISSING go      https://go.dev/dl/"; fail=1; }; \
	command -v node    >/dev/null || { echo "MISSING node    https://nodejs.org/"; fail=1; }; \
	command -v docker  >/dev/null || { echo "MISSING docker  https://docs.docker.com/get-started/get-docker/"; fail=1; }; \
	docker info >/dev/null 2>&1   || { echo "docker is installed but not running — start Docker Desktop"; fail=1; }; \
	[ $$fail = 0 ] || exit 1; \
	echo "go     $$(go version | cut -d' ' -f3)"; \
	echo "node   $$(node --version)"; \
	echo "docker $$(docker --version | cut -d' ' -f3 | tr -d ,)"

.PHONY: deps
deps: $(addprefix deps-,$(NODE_SERVICES))
deps-%:
	@echo "==> npm install ($*)"
	@cd services/$* && npm install --silent

# --- database ---------------------------------------------------------

.PHONY: db
db:
	@echo "==> postgres"
	@$(PG_UP)

.PHONY: seed
seed: db
	@echo "==> seeding"
	@cd services/pokedex && go run . seed

# --- generated code ---------------------------------------------------

# Where homelabctl is. Defined before the rules that use it, because a
# pattern rule's prerequisites are expanded when the Makefile is read.
#
# An absolute path: each recipe cds into the service directory first, so
# a relative one would resolve against services/<name>/ and miss.
# Override to test a local build:
#   make regen HOMELABCTL=$(CURDIR)/../homelabctl/homelabctl
HOMELABCTL ?= $(or $(shell command -v homelabctl 2>/dev/null),$(CURDIR)/.bin/homelabctl)

# Fetched on demand rather than assumed installed, the same binary CI
# downloads. Only fetched when HOMELABCTL points into .bin - an installed
# or overridden one is not a file make knows how to build.
$(CURDIR)/.bin/homelabctl:
	@mkdir -p $(CURDIR)/.bin
	@echo "==> fetching homelabctl"
	@curl -fsSL https://github.com/ChristopherScot/homelabctl/releases/latest/download/homelabctl_$(shell uname -s | tr A-Z a-z)_$(shell uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz \
		| tar -xz -C $(CURDIR)/.bin homelabctl

# Run this after editing openapi.yml, migrations/ or queries/.
#
# CI runs `homelabctl regen` and fails if the result differs from what is
# committed. Without this target that command lived only in the workflow,
# so the first thing to notice stale generated code was a red build on a
# branch - and the fix was a command you had to read the workflow to find.
#
# regen covers both generators and the module: it runs `go generate
# ./...`, which is where the ogen and sqlc directives are, then
# `go mod tidy`. One target rather than three, and it cannot drift from
# what CI does because it is the same command.
.PHONY: regen
regen: $(addprefix regen-,$(MANAGED_SERVICES))
	@echo "regenerated: $(MANAGED_SERVICES)"

# A service with no openapi.yml is not an error: regen says so and exits
# 0, because CI runs it across every service.
regen-%: $(HOMELABCTL)
	@echo "==> regen $*"
	@cd services/$* && $(HOMELABCTL) regen

# --- build ------------------------------------------------------------

.PHONY: build
build: $(addprefix build-,$(SERVICES))
	@echo "built: $(SERVICES)"

# Delegates. Each service has its own Makefile, generated by homelabctl
# from its runtime's template, so the commands live with the service
# rather than being restated here per toolchain.
build-%:
	@echo "==> build $*"
	@$(MAKE) -C services/$* build

# --- test -------------------------------------------------------------

.PHONY: test
test: db $(addprefix test-,$(SERVICES))
	@echo "tested: $(SERVICES)"

# POKEDEX_TEST_DSN is what makes the database tests RUN rather than
# skip, and a skip reads as a pass - so it is set here rather than left
# to whoever remembers.
test-%: db
	@echo "==> test $*"
	@cd services/$* && POKEDEX_TEST_DSN="$(DATABASE_URL)" $(MAKE) test

# --- run --------------------------------------------------------------

.PHONY: run
run: db ports
	@echo "API :$(PORT_API) · react :$(PORT_WEB) · htmx :$(PORT_HTMX) — ctrl-c stops all three"
	@trap 'kill 0' EXIT INT TERM; \
	( cd services/pokedex && PORT=$(PORT_API) go run . ) & \
	sleep 6; \
	( cd services/pokedex-web  && PORT=$(PORT_WEB) npm start ) & \
	( cd services/pokedex-htmx && PORT=$(PORT_HTMX) INSECURE_COOKIES=1 npm start ) & \
	wait

.PHONY: run-api
run-api: db
	cd services/pokedex && go run .

run-%:
	@$(MAKE) -C services/pokedex-$* run

# A port already in use fails deep inside one of three processes racing
# to start, and the error scrolls past. Say it up front instead.
.PHONY: ports
ports:
	@busy=""; \
	for p in $(PORT_API) $(PORT_WEB) $(PORT_HTMX); do \
		lsof -ti:$$p >/dev/null 2>&1 && busy="$$busy $$p"; \
	done; \
	if [ -n "$$busy" ]; then \
		echo "in use:$$busy"; \
		for p in $$busy; do \
			echo "  $$p  $$(lsof -ti:$$p | head -1 | xargs -I{} ps -o command= -p {} | cut -c1-60)"; \
		done; \
		echo "stop them, or pick others:"; \
		echo "  make run PORT_API=19000 PORT_WEB=19001 PORT_HTMX=19002"; \
		exit 1; \
	fi

# --- housekeeping -----------------------------------------------------

.PHONY: clean
clean:
	@docker compose -f services/pokedex/compose.yaml down -v 2>/dev/null || true
	@rm -rf $(foreach s,$(NODE_SERVICES),services/$(s)/dist)
	@echo "stopped postgres, removed dist/"
