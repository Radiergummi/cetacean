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
build:
	cd frontend && npm run build
	go build -o cetacean .

## Run all tests
test:
	go test ./...

## Run all checks (lint + format check + test)
check: lint fmt-check test
