.PHONY: lint typecheck fmt fmt-check build test test-e2e test-stack e2e-up e2e-down check sbom sbom-check sbom-verify hooks

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

## Run the end-to-end stack suite (local only; needs Docker)
## -p 1 serialises packages: each reserves the same lane ports, so parallel
## packages would fight over them. The environment is torn down on success and
## deliberately left running on failure, so a failed case can be inspected.
test-stack: build
	@go test -tags e2e -p 1 -count=1 -timeout 30m ./test/e2e/...; \
	status=$$?; \
	if [ $$status -eq 0 ]; then \
		$(MAKE) e2e-down; \
	else \
		echo "e2e environment left running for inspection; tear down with: make e2e-down"; \
	fi; \
	exit $$status

## Bring the end-to-end environment up and leave it running
e2e-up:
	docker compose -f test/e2e/compose.e2e.yaml up -d

## Tear the end-to-end environment down, including volumes
e2e-down:
	docker compose -f test/e2e/compose.e2e.yaml down -v
	rm -rf test/e2e/certs

## Run all checks (lint + type check + format check + test)
check: lint typecheck fmt-check test

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
