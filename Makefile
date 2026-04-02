.PHONY: lint fmt fmt-check build test test-e2e check

## Lint all code
lint:
	golangci-lint run ./...
	cd frontend && npx oxlint
	cd website && npx oxlint

## Format all code in place
fmt:
	golangci-lint fmt ./...
	cd frontend && npx oxfmt --write .
	cd website && npx oxfmt --write src/

## Check formatting without modifying files
fmt-check:
	golangci-lint fmt --diff ./... 2>&1 | diff /dev/null -
	cd frontend && npx oxfmt --check .
	cd website && npx oxfmt --check src/

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
