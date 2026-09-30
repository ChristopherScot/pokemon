# pokedex-mobile
#
# Include Makefile.local for service-specific setup; it survives regenerate.

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
	@echo "  make regen      regenerate code derived from the spec"
	@echo "  make clean      remove build output"
ifneq (,$(wildcard Makefile.local))
	@$(MAKE) --no-print-directory help-local 2>/dev/null || true
endif

TARGET ?= desktop

# Changing APPID makes it a DIFFERENT app to Android, so an install sits
# beside the old one rather than upgrading it.
APPID   ?= com.github.christopherscot.pokemon.services.pokedex_mobile
APPNAME ?= Pokedex Mobile
MINSDK  ?= 31

# Android's integer versionCode must increase on every release or a phone
# refuses the upgrade as a downgrade.
VERSION_CODE ?= 1
VERSION      ?= $(shell head -n1 VERSION 2>/dev/null || echo v0.0.1)

# Newest build-tools installed; pinning here would break on a runner bump.
BUILD_TOOLS ?= $(shell ls -d "$(ANDROID_HOME)"/build-tools/* 2>/dev/null | sort -V | tail -1)
APKSIGNER   ?= $(BUILD_TOOLS)/apksigner
KEYSTORE    ?= $(HOME)/.android/debug.keystore

.PHONY: build
build: build-$(TARGET)

# `go build ./...` compiles every package and throws the binary away, so
# -o and the package are load-bearing.
.PHONY: build-desktop
build-desktop:
	go build -o pokedex-mobile .

# gogio wants major.minor.patch.versioncode - FOUR components. Three is
# rejected with an error that does not say which one is missing.
.PHONY: build-apk
build-apk: apk-tools
	@command -v gogio >/dev/null || go install gioui.org/cmd/gogio@latest
	gogio -target android -appid $(APPID) -name '$(APPNAME)' \
		-version "$(patsubst v%,%,$(VERSION)).$(VERSION_CODE)" \
		-minsdk $(MINSDK) -ldflags "-X main.version=$(VERSION)" \
		-o pokedex-mobile.raw.apk .
	@$(MAKE) --no-print-directory repack

# Rewrite native libraries as STORED and 16 KB aligned, then re-sign.
# gogio deflates them and Android mmaps a .so straight out of the APK, so
# a compressed one cannot be installed. `zipalign -p 16` alone does NOT
# fix it: it leaves compressed entries alone.
.PHONY: repack
repack:
	@python3 repack_apk.py pokedex-mobile.raw.apk pokedex-mobile.aligned.apk
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
	@python3 check_apk.py pokedex-mobile.apk
	@echo "built pokedex-mobile.apk ($(VERSION))"

# Report missing tools up front, or the build reaches apksigner and fails
# with "command not found" that says nothing about the Android SDK.
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

# -race matches CI.
.PHONY: test
test:
	go vet ./...
	go test -race ./...

.PHONY: run
run:
	go run . $(ARGS)

.PHONY: apk
apk:
	@$(MAKE) --no-print-directory build TARGET=apk

# gogio needs JDK 17+.
.PHONY: apk-check
apk-check:
	@java -version 2>&1 | head -1
	@command -v gogio >/dev/null && echo "gogio present" || echo "gogio missing (make apk installs it)"

.PHONY: regen
regen:
	@command -v homelabctl >/dev/null || { \
		echo "homelabctl not found - https://github.com/ChristopherScot/homelabctl"; exit 1; }
	@homelabctl regen

.PHONY: clean
clean:
	go clean
	rm -f pokedex-mobile pokedex-mobile.apk

-include Makefile.local
