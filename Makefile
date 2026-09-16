.PHONY: lint typecheck fmt fmt-check build test test-e2e test-stack test-stack-race e2e-up e2e-down check bench bench-baseline bench-diff sbom sbom-check sbom-verify hooks cover spec spec-genmcp spec-report spec-report-full

# Where test-stack puts its instrumented binary and the profiles it writes.
# Both are gitignored, and neither replaces ./cetacean.
E2E_BINARY   := cetacean.cover
E2E_RACE_BIN := cetacean.race
E2E_COVERDIR := coverdata

# Where a run writes what it exercised. Gitignored.
SPEC_CLAIMS  := .spec-claims

## Lint all code
lint:
	golangci-lint run ./...
	actionlint
	zizmor --config .github/zizmor.yml .github/workflows/
	pnpm --filter frontend exec oxlint
	pnpm --filter website exec oxlint

## Type-check the frontend and the website
# The website's error reference is generated from Go source, and `astro check`
# type-checks the module that imports it, so the generated file has to exist
# before the check runs.
typecheck:
	pnpm --filter frontend check
	pnpm --filter website sync-assets && pnpm --filter website check

## Format all code in place
fmt:
	golangci-lint fmt ./...
	pnpm --filter frontend exec oxfmt --write .
	pnpm --filter website exec oxfmt --write .

## Check formatting without modifying files
fmt-check:
	golangci-lint fmt --diff ./... 2>&1 | diff /dev/null -
	pnpm --filter frontend exec oxfmt --check .
	pnpm --filter website exec oxfmt --check .

## Build everything
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/radiergummi/cetacean/internal/version.Version=$(VERSION) \
           -X github.com/radiergummi/cetacean/internal/version.Commit=$(COMMIT) \
           -X github.com/radiergummi/cetacean/internal/version.Date=$(DATE)

build: node_modules
	pnpm --filter frontend build
	pnpm --filter frontend build:widgets
	go build -ldflags "$(LDFLAGS)" -o cetacean .

## Install the workspace the build embeds. Not phony: the stamp is the
## directory itself, so a second `make build` skips the install.
node_modules: pnpm-lock.yaml
	pnpm install --frozen-lockfile
	@touch $@

## Run all tests
test:
	go test ./...

## Run end-to-end tests
test-e2e:
	pnpm --filter frontend exec playwright test

## Run the end-to-end stack suite (local only; needs Docker)
## -p 1 serialises packages: each reserves the same lane ports, so parallel
## packages would fight over them. The environment is torn down on success and
## deliberately left running on failure, so a failed case can be inspected.
test-stack: build
	go build -cover -coverpkg=./... -ldflags "$(LDFLAGS)" -o $(E2E_BINARY) .
	rm -rf $(E2E_COVERDIR) && mkdir -p $(E2E_COVERDIR)
	@GOCOVERDIR=$(PWD)/$(E2E_COVERDIR) CETACEAN_E2E_BINARY=$(PWD)/$(E2E_BINARY) \
		go test -tags e2e -p 1 -count=1 -timeout 30m ./test/e2e/...; \
	status=$$?; \
	if [ $$status -eq 0 ]; then \
		$(MAKE) e2e-down; \
	else \
		echo "e2e environment left running for inspection; tear down with: make e2e-down"; \
	fi; \
	echo ""; \
	echo "── what the e2e suite reached ────────────────────────────"; \
	go tool covdata percent -i=$(E2E_COVERDIR) \
		| sed 's|github.com/radiergummi/cetacean|@.|g' | tr '@' '\n' \
		| grep 'coverage:' | sed 's/[[:space:]]*coverage: / /' | sort \
		| awk '{ printf "  %-28s %s\n", $$1, $$2 }'; \
	go tool covdata func -i=$(E2E_COVERDIR) \
		| awk 'END { printf "  %-28s %s\n\n", "TOTAL", $$NF }'; \
	exit $$status

## Run the end-to-end stack suite against a race-instrumented binary
## CI's `go test -race ./...` covers the packages in isolation; nothing until
## now ran the real server's concurrency -- the watcher goroutines, the cache
## mutex, the SSE fan-out, the MCP notification manager and the ACL hot reload
## -- against real Docker events. halt_on_error=1 makes the first race kill
## the SUT, which sut.assertExitedCleanly then reports against the case that
## provoked it; without it the detector logs and the run carries on.
## No coverage here: -cover and -race together make an already slow suite
## slower for a number `make test-stack` already reports.
test-stack-race: build
	go build -race -ldflags "$(LDFLAGS)" -o $(E2E_RACE_BIN) .
	@GORACE=halt_on_error=1 CETACEAN_E2E_BINARY=$(PWD)/$(E2E_RACE_BIN) \
		go test -tags e2e -p 1 -count=1 -timeout 60m ./test/e2e/...; \
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
	CETACEAN_PROMETHEUS_URL=http://127.0.0.1:19090 \
	CETACEAN_SNAPSHOT=false \
	./cetacean & echo $$! > test/e2e/.sut.pid
	@# Wait for the first full sync, not just for the port to open. Backgrounding
	@# the binary and printing straight away hands the browser suite a server
	@# whose cache is still filling, and its first spec fails on a cluster that
	@# is not there yet.
	@for i in $$(seq 1 60); do \
		if curl -fsS -m 2 http://localhost:19001/-/ready >/dev/null 2>&1; then break; fi; \
		if [ $$i -eq 60 ]; then echo "cetacean did not become ready on :19001" >&2; exit 1; fi; \
		sleep 1; \
	done
	@# Relabel the fixtures now that the SUT is watching. Its history ring
	@# buffer is fed by watcher events, and the initial full sync records
	@# none, so without this every Recent Activity section is empty and the
	@# specs that assert one skip.
	go run -tags e2e ./test/e2e/cmd/e2eenv -history
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

## Check the requirement registry against the test suite
spec:
	go run ./scripts/spec-gate static
	go run ./scripts/spec-gate sweep

## Report what the unit suite exercised
#
# Both report targets clear the claims directory first: a stale <pid>.claims
# from an earlier run would satisfy a requirement this run never reached.
spec-report:
	rm -rf $(SPEC_CLAIMS) && mkdir -p $(SPEC_CLAIMS)
	CETACEAN_SPEC_CLAIMS=$(PWD)/$(SPEC_CLAIMS) go test -count=1 ./...
	go run ./scripts/spec-gate report --claims $(SPEC_CLAIMS) --suites unit

## Report what the unit and e2e suites exercised together
spec-report-full: build
	rm -rf $(SPEC_CLAIMS) && mkdir -p $(SPEC_CLAIMS)
	CETACEAN_SPEC_CLAIMS=$(PWD)/$(SPEC_CLAIMS) go test -count=1 ./...
	CETACEAN_SPEC_CLAIMS=$(PWD)/$(SPEC_CLAIMS) CETACEAN_E2E_BINARY=$(PWD)/$(E2E_BINARY) \
	  go test -tags e2e -p 1 -count=1 -timeout 30m ./test/e2e/...
	go run ./scripts/spec-gate report --claims $(SPEC_CLAIMS) --suites unit,e2e

## Regenerate the MCP registry families from upstream
#
# Needs network: it fetches the conformance suite's own requirement files at
# the revision scripts/spec-genmcp pins. Not part of `check`.
spec-genmcp:
	go run ./scripts/spec-genmcp
	git diff --exit-code internal/spec/registry/mcp/

## Run all checks (lint + type check + format check + test + spec)
check: lint typecheck fmt-check test spec

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
SBOM_ARTIFACTS := $(shell grep -v '^\#' scripts/sbom-artifacts.txt)

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
