.PHONY: lint typecheck fmt fmt-check build test test-e2e test-stack e2e-up e2e-down check sbom sbom-check sbom-verify hooks cover cover-e2e fuzz

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

## Bring the end-to-end environment up with fixtures, and run cetacean against it
# CETACEAN_CONFIG=/dev/null blocks the config-file autodiscovery a plain
# `./cetacean` invocation would otherwise do (./cetacean.toml in the repo
# root, then the user's own ~/.cetacean.toml) -- a developer's own local
# config silently changing this environment is exactly what the harness
# exists to rule out. CETACEAN_OPERATIONS_LEVEL=3 is set because
# frontend/e2e's specs assert write affordances (Remove buttons, editors) are
# present unconditionally, not gated on CETACEAN_E2E_WRITE -- at the default
# level 1 those assertions fail not because anything is broken, but because
# the Allow-header-gated buttons the specs look for are correctly absent.
e2e-up: build
	go run -tags e2e ./test/e2e/cmd/e2eenv
	env -i \
	PATH="$$PATH" \
	HOME="$$HOME" \
	CETACEAN_CONFIG=/dev/null \
	CETACEAN_AUTH_MODE=none \
	CETACEAN_OPERATIONS_LEVEL=3 \
	CETACEAN_LISTEN_ADDR=:19001 \
	CETACEAN_DOCKER_HOST=tcp://127.0.0.1:12375 \
	CETACEAN_SNAPSHOT=false \
	./cetacean & echo $$! > test/e2e/.sut.pid
	@echo "Cetacean running at http://localhost:19001"
	@echo "Run the browser suite with:"
	@echo "  CETACEAN_E2E_URL=http://localhost:19001 make test-e2e"

## Tear the end-to-end environment down, including volumes
# Tolerates a stale pidfile (process already exited) or none at all (e.g.
# torn down after test-stack, which never wrote one) — the pidfile is always
# removed once a kill has been attempted, so a dead PID cannot wedge teardown.
e2e-down:
	-@if [ -f test/e2e/.sut.pid ]; then \
		kill $$(cat test/e2e/.sut.pid) 2>/dev/null || true; \
		rm -f test/e2e/.sut.pid; \
	fi
	docker compose -f test/e2e/compose.e2e.yaml down -v
	-docker run --rm -v $(PWD)/test/e2e/certs:/certs alpine sh -c 'rm -rf /certs/..?* /certs/.[!.]* /certs/*' 2>/dev/null
	-rm -rf test/e2e/certs

## Measure the repository coverage baseline: what `go test ./...` reaches.
cover:
	go test -coverprofile=cover.out -covermode=atomic ./...
	go tool cover -func=cover.out | tail -1
	@echo "HTML report: go tool cover -html=cover.out"

## Measure what the e2e harness reaches on top of that, by running an
## instrumented binary. Leaves ./cetacean instrumented — run `make build` after.
cover-e2e: frontend/node_modules
	cd frontend && npm run build
	cd frontend && npm run build:widgets
	go build -cover -ldflags "$(LDFLAGS)" -o cetacean .
	rm -rf coverdata && mkdir -p coverdata
	@GOCOVERDIR=$(PWD)/coverdata go test -tags e2e -p 1 -count=1 -timeout 30m ./test/e2e/...; \
	status=$$?; \
	if [ $$status -eq 0 ]; then \
		$(MAKE) e2e-down; \
	else \
		echo "e2e environment left running for inspection; tear down with: make e2e-down"; \
	fi; \
	go tool covdata percent -i=coverdata; \
	echo ""; \
	echo "NOTE: ./cetacean is coverage-instrumented. Run 'make build' before anything else uses it."; \
	exit $$status

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
