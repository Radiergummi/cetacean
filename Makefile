.PHONY: lint fmt fmt-check build test check

## Lint all code
lint:
	golangci-lint run ./...
	cd frontend && npx oxlint

## Format all code in place
fmt:
	golangci-lint fmt ./...
	cd frontend && npx oxfmt --write .

## Check formatting without modifying files
fmt-check:
	gofmt -l . | grep -v frontend/ | diff /dev/null -
	cd frontend && npx oxfmt --check .

## Build everything
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X cetacean/internal/version.Version=$(VERSION) \
           -X cetacean/internal/version.Commit=$(COMMIT) \
           -X cetacean/internal/version.Date=$(DATE)

build:
	cd frontend && npm run build
	go build -ldflags "$(LDFLAGS)" -o cetacean .

## Run all tests
test:
	go test ./...

## Run all checks (lint + format check + test)
check: lint fmt-check test
