.PHONY: lint fmt fmt-check build test test-e2e check sbom sbom-check hooks

## Lint all code
lint:
	golangci-lint run ./...
	cd frontend && npx oxlint
	cd website && npx oxlint

## Format all code in place
fmt:
	golangci-lint fmt ./...
	cd frontend && npx oxfmt --write .
	cd website && npx oxfmt --write .

## Check formatting without modifying files
fmt-check:
	golangci-lint fmt --diff ./... 2>&1 | diff /dev/null -
	cd frontend && npx oxfmt --check .
	cd website && npx oxfmt --check .

## Build everything
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/radiergummi/cetacean/internal/version.Version=$(VERSION) \
           -X github.com/radiergummi/cetacean/internal/version.Commit=$(COMMIT) \
           -X github.com/radiergummi/cetacean/internal/version.Date=$(DATE)

build:
	cd frontend && npm run build
	go build -ldflags "$(LDFLAGS)" -o cetacean .

## Run all tests
test:
	go test ./...

## Run end-to-end tests
test-e2e:
	cd frontend && npx playwright test

## Run all checks (lint + format check + test)
check: lint fmt-check test

## Generate the CycloneDX SBOM (Go + frontend npm) embedded into the binary
sbom:
	./scripts/build-sbom.sh

## Verify the committed SBOM is up to date (CI gate)
sbom-check: sbom
	@git diff --exit-code -- internal/api/sbom/sbom.cdx.json \
	  || { echo "ERROR: internal/api/sbom/sbom.cdx.json is stale. Run 'make sbom' and commit." >&2; exit 1; }

## Install the repository git hooks (opt-in)
hooks:
	git config core.hooksPath .githooks
	@echo "git hooks installed (core.hooksPath=.githooks)"
