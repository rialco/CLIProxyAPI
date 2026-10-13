GO ?= $(or $(shell command -v go 2>/dev/null),$(firstword $(wildcard /opt/homebrew/opt/go@1.26/bin/go /usr/local/opt/go@1.26/bin/go)),go)
PYTHON ?= python3
BUN ?= bun
export GOCACHE := $(CURDIR)/.dev/go-build
export GOMODCACHE := $(CURDIR)/.dev/go-mod

.PHONY: help setup deps build web-build web-check dev test check smoke login-claude

help:
	@printf '%s\n' 'make setup        Create private local config and download Go dependencies' 'make build        Build the ATID panel and Go server' 'make dev          Build and run on 127.0.0.1:8318 (Ctrl+C to stop)' 'make test         Run the Go test suite' 'make web-build    Build the ATID management panel' 'make web-check    Run frontend tests and lint' 'make check        Build and verify the Go server and frontend' 'make smoke        Check the running development API without model requests' 'make login-claude Connect Claude to the development instance'

setup:
	@$(PYTHON) scripts/dev.py setup
	@$(MAKE) deps

deps:
	$(GO) mod download

web/node_modules/.atid-ready: web/package.json web/bun.lock
	cd web && $(BUN) install --frozen-lockfile
	@touch $@

web-build: web/node_modules/.atid-ready
	cd web && $(BUN) run build
	@mkdir -p .dev/static bin/static
	@cp web/dist/index.html .dev/static/management.html
	@cp web/dist/index.html bin/static/management.html

web-check: web/node_modules/.atid-ready
	cd web && $(BUN) run test && $(BUN) run lint

build: web-build
	@mkdir -p bin
	$(GO) build -o bin/cli-proxy-api-dev ./cmd/server

dev: build
	@$(PYTHON) scripts/dev.py setup
	@cd .dev && exec ../bin/cli-proxy-api-dev --config config.yaml --local-model

test:
	$(GO) test ./...

check: build test web-check

smoke:
	@$(PYTHON) scripts/dev.py smoke

login-claude: build
	@$(PYTHON) scripts/dev.py setup
	@cd .dev && exec ../bin/cli-proxy-api-dev --config config.yaml --claude-login
