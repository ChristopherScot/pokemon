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
	@echo "  make build      compile for the desktop (TARGET=apk for Android)"
	@echo "  make test       vet and tests (what CI runs)"
	@echo "  make run        run it (make run ARGS='--help')"
	@echo "  make apk        build an installable Android APK"
	@echo "  make clean      remove build output"
ifneq (,$(wildcard Makefile.local))
	@$(MAKE) --no-print-directory help-local 2>/dev/null || true
endif

# What to build is decided here, not at each call site.
#
#   make build              the desktop binary, for playing it while working
#   make build TARGET=apk   the Android APK, what a phone installs
#
# The two differ in more than a flag: the APK needs a JDK, the Android
# SDK and NDK, a four-part version, and a repack afterwards because
# gogio compresses the native libraries and modern Android refuses to
# install those. Keeping that here rather than in the workflow means CI
# runs the same command as a person does, so a break shows up locally.
TARGET ?= desktop

# The app id and display name are the phone's identity for this app.
# Changing the id makes it a DIFFERENT app to Android, so an install
# sits beside the old one rather than upgrading it.
APPID   ?= com.github.christopherscot.pokemon.services.pokedex_mobile
APPNAME ?= Pokedex Mobile
MINSDK  ?= 31

# Android's integer versionCode must increase on every release or a
# phone refuses the upgrade as a downgrade. CI passes its run number;
# locally 1 is fine, since a local build is for one phone on a desk.
VERSION_CODE ?= 1
VERSION      ?= $(shell head -n1 VERSION 2>/dev/null || echo v0.0.1)

.PHONY: build
build: build-$(TARGET)

# -o, and the package rather than ./... - `go build ./...` compiles
# every package and THROWS THE BINARY AWAY, so `make build` looked like
# it worked and left nothing to run. It writes ./pokedex-mobile, which
# is what `make clean` removes and what you type to run it.
.PHONY: build-desktop
build-desktop:
	go build -o pokedex-mobile .

# gogio wants major.minor.patch.versioncode - FOUR components. Three is
# rejected, and the error does not say which one is missing.
.PHONY: build-apk
build-apk: apk-tools
	@command -v gogio >/dev/null || go install gioui.org/cmd/gogio@latest
	gogio -target android -appid $(APPID) -name '$(APPNAME)' \
		-version "$(patsubst v%,%,$(VERSION)).$(VERSION_CODE)" \
		-minsdk $(MINSDK) -ldflags "-X main.version=$(VERSION)" \
		-o pokedex-mobile.raw.apk .
	@$(MAKE) --no-print-directory repack

# Rewrite the native libraries as STORED and 16 KB aligned, then
# re-sign. gogio deflates them and runs `zipalign 4`; Android mmaps a
# .so straight out of the APK, so a compressed one cannot be installed
# and the installer says only "app not installed" with the reason
# buried in logcat. `zipalign -p 16` does NOT fix it - it aligns
# entries that are already uncompressed and leaves compressed ones
# alone - which is why this is a script.
#
# Signing comes after, because rewriting the zip invalidates the v2
# signature gogio produced.
.PHONY: repack
repack:
	@python3 ../../scripts/repack_apk.py pokedex-mobile.raw.apk pokedex-mobile.aligned.apk
	@# gogio generates a debug keystore; reuse it so the signing
	@# identity does not change within a build. Android identifies an
	@# app by (app id, signing key), so a fresh key means an install
	@# beside the old app rather than an upgrade of it.
	@if [ ! -f "$(KEYSTORE)" ]; then \
		mkdir -p "$(dir $(KEYSTORE))"; \
		keytool -genkeypair -keystore "$(KEYSTORE)" -storepass android \
			-keypass android -alias androiddebugkey -keyalg RSA \
			-keysize 2048 -validity 10000 \
			-dname "CN=Android Debug,O=Android,C=US"; \
	fi
	@"$(APKSIGNER)" sign \
		--ks "$(KEYSTORE)" --ks-pass pass:android --key-pass pass:android \
		--ks-key-alias androiddebugkey \
		--v1-signing-enabled true --v2-signing-enabled true \
		--v3-signing-enabled true --v4-signing-enabled false \
		--out pokedex-mobile.apk pokedex-mobile.aligned.apk
	@"$(APKSIGNER)" verify --print-certs pokedex-mobile.apk
	@rm -f pokedex-mobile.raw.apk pokedex-mobile.aligned.apk
	@# Fails rather than publishing an APK a phone refuses. Both things
	@# this checks shipped broken once.
	@python3 ../../scripts/check_apk.py pokedex-mobile.apk
	@echo "built pokedex-mobile.apk ($(VERSION))"

# Say what is missing, up front. Without this the build gets most of
# the way to an APK and then runs "/apksigner", whose "command not
# found" says nothing about the Android SDK being absent.
.PHONY: apk-tools
apk-tools:
	@fail=0; \
	[ -n "$(ANDROID_HOME)" ] || { \
		echo "ANDROID_HOME is unset - the Android SDK is needed to build an APK"; fail=1; }; \
	[ -n "$(BUILD_TOOLS)" ] || { \
		echo "no build-tools under $(ANDROID_HOME) - install them with sdkmanager"; fail=1; }; \
	command -v java >/dev/null || { \
		echo "no java - gogio needs a JDK, 17 or newer"; fail=1; }; \
	command -v python3 >/dev/null || { echo "no python3 - needed to repack the APK"; fail=1; }; \
	[ $$fail = 0 ] || exit 1

# The newest build-tools installed, whatever that is. Pinning a version
# here means a runner image bump breaks the build.
BUILD_TOOLS ?= $(shell ls -d "$(ANDROID_HOME)"/build-tools/* 2>/dev/null | sort -V | tail -1)
APKSIGNER   ?= $(BUILD_TOOLS)/apksigner
KEYSTORE    ?= $(HOME)/.android/debug.keystore

# -race because CI runs it that way, and a data race that only appears
# under load is the kind of thing you want to find here instead.
.PHONY: test
test:
	go vet ./...
	go test -race ./...

.PHONY: run
run:
	go run . $(ARGS)

# Kept as the obvious name for the obvious thing.
.PHONY: apk
apk:
	@$(MAKE) --no-print-directory build TARGET=apk

# gogio needs a JDK, and a modern one: JDK 8's d8 cannot load what it
# emits. CI pins 17.
.PHONY: apk-check
apk-check:
	@java -version 2>&1 | head -1
	@command -v gogio >/dev/null && echo "gogio present" || echo "gogio missing (make apk installs it)"

.PHONY: clean
clean:
	go clean
	rm -f pokedex-mobile pokedex-mobile.apk \
		pokedex-mobile.raw.apk pokedex-mobile.aligned.apk

-include Makefile.local
