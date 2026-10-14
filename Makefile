.DEFAULT_GOAL := help
.NOTPARALLEL:

GO ?= go
export AF_GO = $(GO)
DEV = $(GO) run ./tools/dev
TARGET_OS ?= $(shell $(GO) env GOOS)
TARGET_ARCH ?= $(shell $(GO) env GOARCH)
OUT ?= build
RELEASE_DIR ?= dist
DATA ?= private/go-platform
LISTEN ?= 127.0.0.1:8788
PUBLIC_URL ?= http://127.0.0.1:8788
BROWSER ?=
MAX_BROWSERS ?= 3
SERVICE ?= afplatform.service

.PHONY: help deps frontend build release test test-race test-api vet fmt check ui-test verify run serve status clean service-start service-stop service-restart service-status service-logs

help:
	@$(DEV) help

deps:
	$(GO) mod download
	$(DEV) npm ci

frontend:
	$(DEV) frontend

# Go-only build: embeds the committed UI; Node is not required.
build:
	$(DEV) build -os "$(TARGET_OS)" -arch "$(TARGET_ARCH)" -out "$(OUT)"

release:
	$(DEV) release -out "$(RELEASE_DIR)"

test:
	$(GO) test ./cmd/... ./internal/... ./tools/... -count=1

test-race:
	$(GO) test -race ./cmd/... ./internal/... ./tools/... -count=1

test-api:
	$(GO) test ./internal/platform -run TestDocumentedAPI -count=1 -v

vet:
	$(GO) vet ./cmd/... ./internal/... ./tools/...

fmt:
	$(GO) fmt ./cmd/... ./internal/... ./tools/...

check: vet test

ui-test:
	$(DEV) ui-test

# Full local integration check; does not submit real AlphaFold jobs.
verify:
	$(DEV) verify

run:
	$(DEV) build -out "$(OUT)"
	$(DEV) serve -out "$(OUT)" -data "$(DATA)" -listen "$(LISTEN)" -public-url "$(PUBLIC_URL)" -browser "$(BROWSER)" -max-active-browsers "$(MAX_BROWSERS)"

serve:
	$(DEV) serve -out "$(OUT)" -data "$(DATA)" -listen "$(LISTEN)" -public-url "$(PUBLIC_URL)" -browser "$(BROWSER)" -max-active-browsers "$(MAX_BROWSERS)"

status:
	$(DEV) status -url "$(PUBLIC_URL)"

# Deliberately leaves binaries, data, profiles, embedded assets and evidence intact.
clean:
	$(DEV) clean

ifeq ($(OS),Windows_NT)
service-start service-stop service-restart service-status service-logs:
	$(error service-* targets require Linux/systemd; on Windows use make run and Ctrl+C)
else
service-start:
	systemctl start "$(SERVICE)"
service-stop:
	systemctl stop "$(SERVICE)"
service-restart:
	systemctl restart "$(SERVICE)"
service-status:
	systemctl status "$(SERVICE)" --no-pager
service-logs:
	journalctl -u "$(SERVICE)" -n 100 -f
endif
