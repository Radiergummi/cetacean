.PHONY: lint typecheck fmt fmt-check build test test-e2e check bench bench-baseline bench-diff sbom sbom-check sbom-verify hooks

## Lint all code
lint:
	golangci-lint run ./...
	cd frontend && npx oxlint
	cd website && npx oxlint

## Type-check the frontend and the website
# The website's error reference is generated from Go source, and `astro check`
# type-checks the module that imports it, so the generated file has to exist
# before the check runs.
typecheck:
	cd frontend && npm run check
	cd website && npm run sync-assets && npm run check

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

build: frontend/node_modules
	cd frontend && npm run build
	cd frontend && npm run build:widgets
	go build -ldflags "$(LDFLAGS)" -o cetacean .

## Install the frontend dependencies the build embeds. Not phony: the stamp is
## the directory itself, so a second `make build` skips the install.
frontend/node_modules: frontend/package-lock.json
	cd frontend && npm ci
	@touch $@

## Run all tests
test:
	go test ./...

## Run end-to-end tests
test-e2e:
	cd frontend && npx playwright test

## Run all checks (lint + type check + format check + test)
check: lint typecheck fmt-check test

## Run the Go benchmark suite, writing results to bench.txt
#
# The default is a fixed iteration count rather than a duration: allocs/op and
# B/op are what these are read for, those are counted rather than timed, and a
# fixed count keeps a run to a predictable couple of minutes. Override for
# timing work, where a duration and more samples are worth the wait:
#
#   make bench BENCHTIME=1s BENCHCOUNT=10
#   make bench BENCH='BenchmarkHandleSearch|BenchmarkStackMatcher'
#
# -race is deliberately absent: it perturbs allocation accounting.
BENCH      ?= .
BENCHTIME  ?= 200x
BENCHCOUNT ?= 6
BENCHPKGS  ?= ./internal/...
BENCHOUT   ?= bench.txt

bench:
	go test -run '^$$' -bench '$(BENCH)' -benchmem \
	  -benchtime=$(BENCHTIME) -count=$(BENCHCOUNT) $(BENCHPKGS) \
	  | tee $(BENCHOUT)

## Record the current results as the baseline to compare against
bench-baseline: bench
	@cp $(BENCHOUT) bench-baseline.txt
	@echo "baseline recorded in bench-baseline.txt"

## Compare bench.txt against the recorded baseline
#
# Run `make bench-baseline` before changing anything, then `make bench` after,
# then this. benchstat reports allocs/op with no noise, so any movement there is
# real; sec/op needs a quiet machine before it means much.
bench-diff:
	@test -f bench-baseline.txt || { echo "no baseline: run 'make bench-baseline' first" >&2; exit 1; }
	go run golang.org/x/perf/cmd/benchstat@latest bench-baseline.txt $(BENCHOUT)

## Generate the CycloneDX SBOM (Go + frontend npm) embedded into the binary
# The artifacts build-sbom.sh produces. Listed explicitly because
# internal/api/sbom/ also holds Go source, so the directory is not a usable
# stand-in. scripts/commit-sbom.sh carries the same list for the CI commit path,
# and .githooks/pre-commit for the local one.
SBOM_ARTIFACTS := internal/api/sbom/sbom.cdx.json \
                  internal/api/sbom/licensetexts.json \
                  internal/api/sbom/notices.txt \
                  THIRD_PARTY_LICENSES

sbom:
	./scripts/build-sbom.sh

## Verify the committed SBOM is up to date (CI gate)
sbom-check: sbom sbom-verify

## Compare the committed SBOM artifacts against the working tree, without
## regenerating them. Split out of sbom-check so CI can regenerate once and then
## verify, rather than paying for the build twice.
sbom-verify:
	@git diff --exit-code -- $(SBOM_ARTIFACTS) \
	  || { echo "ERROR: SBOM artifacts are stale. Run 'make sbom' and commit." >&2; exit 1; }

## Install the repository git hooks (opt-in)
hooks:
	git config core.hooksPath .githooks
	@echo "git hooks installed (core.hooksPath=.githooks)"
