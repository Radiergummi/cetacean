# End-to-end test harness

A local-only harness that runs the real `./cetacean` binary against a real Docker Swarm (via
Docker-in-Docker) and asserts over HTTP, SSE and MCP. It also gives the existing
[Playwright suite][the-browser-suite] a reproducible cluster to run against. See
[the design doc][design] for why this exists and how it is put together.

## Not for CI

This suite never runs in CI. It needs a privileged container, takes minutes, and depends on a
locally built binary and fixture image — none of which fit a CI budget. Run it locally before
opening a PR that touches wiring: config loading, auth, TLS, or anything in `main.go`.

## Prerequisites

- Docker with Compose (`docker compose`), and permission to run privileged containers
- `make build` — the suite runs the real binary, not `go run .`

## Running it

```bash
make test-stack
```

This builds the binary, brings up the environment, runs every lane (`go test -tags e2e -p 1
-count=1 -timeout 30m ./test/e2e/...`), and tears the environment down **only on success** — a
failing run is left up so you can inspect it with `docker compose -f test/e2e/compose.e2e.yaml
ps`, poke the engine at `tcp://127.0.0.1:12375`, or attach to the SUT's logs. Tear it down by hand
afterwards with `make e2e-down`.

### Running one lane

`-p 1` matters (see [Constraints](#constraints) below), but only across packages — a single package
is already serial. `make e2e-up` is **not** a prerequisite here: `harness.Up` brings its own
environment up on first use, the same way `make test-stack` does. Run one file's tests directly:

```bash
go test -tags e2e -run TestDuplicateClientCertIsRejected ./test/e2e/...
```

Careful running this against an environment `make e2e-up` already brought up: that command parks
a SUT on port `19001` (see [Reserved ports](#reserved-ports) below), which `api_test.go`,
`sse_test.go`, `write_test.go`, `mcp_test.go` and `sut/sut_test.go` all reserve too. A test in one
of those files will sit in `waitPortFree` and then fail rather than run. Either run a test outside
that set, as above, or stop the `e2e-up` SUT first (`make e2e-down`, or `kill` the PID in
`test/e2e/.sut.pid`) before running one that needs `19001`.

Or narrow to a package:

```bash
go test -tags e2e ./test/e2e/fixtures/...
```

The environment is shared process-wide via `sync.Once` (see `harness.Up`), so the first test in a
run brings it up and nothing tears it down automatically — that's `make e2e-down`'s job, or the
`make test-stack` wrapper above.

## Reserved ports

Caddy's TLS termination is static configuration, so each lane's SUT gets a fixed port rather than
one chosen at runtime. Cases within a lane run serially; lanes can run in parallel with each other.

| Port    | Lane / role                          |
| ------- | ------------------------------------- |
| `12375` | Docker-in-Docker engine (`tcp://127.0.0.1:12375`) |
| `19001` | `none` auth mode (also the `e2e-up` SUT) |
| `19002` | `oidc` auth mode |
| `19003` | `headers` auth mode (reserved; not currently exercised as a dedicated lane — see below) |
| `19004` | `cert` auth mode |
| `19005` | ACL lane |
| `19006` | Write sweep (every mutating route, against a real cluster) |
| `19007` | Read sweep (every GET/HEAD route × five ACL personas) |
| `19008` | MCP sweep (every tool × five ACL personas) |
| `19009` | Hostile proxy cases (malformed/duplicate headers, against `cert` and `headers` modes) |
| `19010` | Dex (OIDC provider) |
| `19011` | MCP OAuth lane (five consecutive SUTs: the flow, theft detection without the resource indicator, the DCR rate limit, the restart, and DCR/CIMD disabled) |
| `19012` | `tailscale` auth mode (local-mode address boundary, plus tsnet startup validation) |
| `19013` | SSE ACL filtering on `GET /events`, and the broadcaster's connection cap |
| `19014` | Conditional requests: `If-Match` on every preconditioned route, `If-None-Match` on the representation it compares against |
| `19015` | Log reads and the SSE log tail, against containers writing known output |
| `19016` | MCP streaming: the subscription notification stream, completions, and the tasks extension |
| `19017` | The representation matrix: every content type each negotiated route declares |
| `19018` | Deployment shape: base path, TLS in the binary, the MCP Origin guard, cache snapshots |
| `19019` | `?filter=` on every list endpoint: each env field, the FLT codes, and the pipeline order |
| `19020` | Metrics, against a real Prometheus seeded with generated TSDB blocks |
| `19021` | Shutdown: what happens to a request in flight when the process is signalled |
| `19022` | Connectivity: the Docker API going away underneath a running SUT, and coming back |
| `19023` | Tracing: CETACEAN_OTEL_ENDPOINT, driven against a stand-in OTLP collector |
| `19024` | Goroutine leaks: what streaming connections leave behind once they close |
| `19025` | SSE fan-out isolation: what a client that has stopped reading costs the others |
| `19090` | Prometheus (the metrics lane's, published for the host-side SUT) |
| `19104` | Caddy, mTLS termination |

`19003` is reserved in the numbering scheme but the `headers`-mode hostile-input cases run on
`19009` alongside `cert` instead of a dedicated lane at `19003`. This is a deviation from the
design doc, not something it called for: the doc's [topology][topology] names a second Caddy site
for header injection and an `nginx` service for the realistic XFF-only misconfiguration, and
neither was built. The hostile-proxy lane's loopback proxy covers the malformed-input cases, but
Caddy header injection and the nginx misconfiguration remain untested — see
[Deferred](#deferred).

## Bringing the environment up by hand

```bash
make e2e-up      # builds, brings up the environment, deploys fixtures, starts cetacean on :19001
make e2e-down    # kills that cetacean, tears the environment down, removes generated certs
```

`e2e-up` prints the URL to point things at. Under the hood it runs `test/e2e/cmd/e2eenv` (the
non-test entry point for `harness.Up` / `SwarmInit` / `fixtures.DeployBaseline`) to bring up
Docker-in-Docker, init the swarm, and deploy the baseline fixtures, then backgrounds `./cetacean`
against it and records its PID in `test/e2e/.sut.pid`. It runs with `CETACEAN_CONFIG=/dev/null`
(so a `./cetacean.toml` or `~/.cetacean.toml` you happen to have lying around cannot silently
change this environment — it did, in testing, until this was added) and
`CETACEAN_OPERATIONS_LEVEL=3`, because `frontend/e2e`'s specs check for write affordances (Remove
buttons, inline editors) being present unconditionally, not gated on `CETACEAN_E2E_WRITE`.

It then adds what the browser suite needs beyond the baseline (`fixtures/browser.go`), which the Go
lanes must not see:

- **Page fillers.** Sixty configs named `zz-page-filler-NNN`, so a list is longer than the
  dashboard's fifty-item page and the load-more sentinel has something to do. They sort last, so
  the first row every detail spec clicks is still a real fixture.
- **A relabelling pass**, run *after* the SUT reports ready — `e2eenv -history`. The cache records
  history from watcher events, and the initial full sync applies `ReplaceAll`, which records none:
  a fixture deployed before startup has an empty activity feed forever. Touching each one
  afterwards is what gives the dashboard's Recent Activity sections anything to show.

`fixtures.DeployBaseline` removes the fillers, so a `make test-stack` run that adopts an
environment `e2e-up` left behind still sees the baseline the lanes assert over.

**Bring it down when you are done with it.** The baseline includes `shop_flaky`, a service that
crash-loops on purpose so failed tasks and the restart counter have real input, and `e2e-up` leaves
it running indefinitely — which is fine for a Playwright run and not fine for an afternoon. Left up
for hours it churns task containers continuously inside Docker-in-Docker, and the host daemon slows
to the point where `docker ps` and even a `PATCH` through the SUT hang for minutes. Tearing the
environment down releases it; nothing else needs restarting.

This is deliberately the same environment `make test-stack` uses, just without the Go test binary
driving it — useful for exploring the fixture cluster by hand, or for the Playwright suite below.

Backgrounding a process from a Makefile recipe and killing it later via a pidfile has sharp edges
worth knowing about:

- `e2e-down` tolerates a stale pidfile (the process already exited) and removes the pidfile either
  way, so a dead PID can't wedge teardown. It does not, however, verify that the PID it kills is
  still `./cetacean` — if something else reused that PID between runs, `e2e-down` would signal the
  wrong process. Unlikely on a short-lived local run, but real.
- If you redirect `make e2e-up`'s output through a pipe (`make e2e-up | tee log`), the backgrounded
  `./cetacean` inherits that pipe's file descriptors, and the pipeline won't reach EOF until
  `./cetacean` exits — so a shell waiting on the whole pipeline will hang. Run it unpiped, or
  redirect the binary's own output separately, if you need to capture the log.
- You can always fall back to `docker compose -f test/e2e/compose.e2e.yaml up -d` /
  `down -v` directly if you just want the swarm without a SUT running against it.

## Pointing Playwright at it

```bash
make e2e-up
CETACEAN_E2E_URL=http://localhost:19001 make test-e2e
make e2e-down
```

(That's `CETACEAN_E2E_URL`, matching `playwright.config.ts`'s `baseURL` resolution.) `e2e-up`
seeds Prometheus, so the specs gated on `/metrics/status` run rather than skip. Specs gated behind
`CETACEAN_E2E_WRITE` still skip unless you set that variable; the baseline fixtures are shared and
idempotent, so mutating them isn't the default.

### Keeping it honest

The browser suite passes in full against this environment: **159 passed, 3 skipped** with
`CETACEAN_E2E_WRITE=1`, repeatably (seven consecutive runs). Only three skips remain, and each is a fact about the environment rather
than about the suite: the profile page's authenticated half (this SUT runs `none`, and its
unauthenticated half runs), the monitoring banner's partially-configured state (monitoring here is
fully healthy), and plugin detail (a DinD engine has no plugins — the Go lanes excuse the same
route for the same reason).

**A skipped spec is not a passing spec**, and the thirteen that were skipping got there the same
way: each decided whether it could run by probing the DOM the instant `page.goto` resolved. The
dashboard fills itself in after mount, so "absent" and "not fetched yet" look identical at that
moment, and `test.skip` makes the wrong answer silent. The worst case was `auth.spec.ts`, a
mutually exclusive pair in which the branch written for `none` mode skipped and the branch written
for authenticated mode ran — asserting only that *some* heading existed, which after the redirect
was the Cluster Overview it had been sent to. Both halves were dead, in every mode.

One spec is a known flake, undiagnosed: `service-editors.spec.ts`'s "labels: Cancel returns to read
mode" failed once in eight consecutive full runs and could not be reproduced — in isolation, or in
seven runs after. The specs each get their own browser context, so the only thing they share is the
cluster; `write-outcomes.spec.ts` already targets `shop_lonely` specifically so that "the first
service" specs are not read while it mutates one. It is recorded here rather than papered over with a
retry: a retry would hide it, and guessing at a mechanism would be the same mistake as the skips
above.

The rule that replaced it: **ask the server, not the DOM.** Whether a resource has history, whether
a stack has configs, whether recommendations exist, whether a write is offered — the API answers
all of these, and `fixtures.ts` exports the helpers (`apiJson`, `hasHistory`, `allowedMethods`,
`authProvider`) that do the asking. A `test.skip` is then a statement about the cluster, which is
what it was always meant to be. Where a spec must watch something transient — the load-more
sentinel, which exists only while a page is outstanding — hold the response open with `page.route`
rather than racing it.

## Running it against the race detector

```bash
make test-stack-race
```

Builds the SUT with `-race` and drives every lane against it, with
`GORACE=halt_on_error=1` so the first race kills the process rather than being logged and outlived by
the run that provoked it. `sut.assertExitedCleanly` then reports it against the case that caused it —
which is also what catches a panic, or any non-zero exit, on a teardown path nothing used to look at.

CI's `go test -race ./...` covers the packages in isolation. This is the only thing that races the
real server's concurrency: the watcher goroutines, the cache mutex, the SSE fan-out, the MCP
notification manager and the ACL hot reload, against real Docker events. It takes roughly twice as
long as `make test-stack` and reports no coverage, so it is a thing to run before a release rather
than on every change.

## Constraints

- **`-p 1` is required** when running more than one package (`./test/e2e/...`). Packages share the
  same fixed lane ports (see the table above), so parallel packages fight over them.
- **No `t.Parallel()`.** Same reason, at the test level.
- **The formatting gate is `golangci-lint fmt`, not `gofmt`.** CI (and this suite's own checks) run
  `golangci-lint fmt --diff ./... | diff /dev/null -`, which fails on any output. Run
  `golangci-lint fmt ./...` before committing.
- Every file here — and `test/e2e/cmd/e2eenv/main.go` — carries `//go:build e2e`, so `go test ./...`
  and `make check` never touch it.

## Seeded Prometheus

`compose.e2e.yaml` runs a Prometheus with no scrape targets, and `harness.SeedPrometheus` writes
TSDB blocks into it with `promtool tsdb create-blocks-from openmetrics`, restarting it onto them.
Real exporters would have to run for hours to produce a history worth querying and would produce a
different one every run; generated blocks give three hours of history immediately and the same
history every time.

The point of "the same history every time" is that the assertions are exact. Every seeded counter
rises by a fixed step per sample, and the step divides the `[5m]` window every query in the product
uses, so `rate()` yields a number rather than a range — which is why `metrics_test.go` can say the
CPU panel reads 50% instead of saying it reads something. `up` is seeded too: with nothing scraped
there is otherwise no `up` for the cAdvisor detection to find.

## Deferred

This pass shipped less than the design doc's [Coverage][coverage] table describes.
Nothing below is exercised by this suite, and — except where noted — nothing else pins it either.
Struck-through entries have since been covered and are kept so the list reads as a record rather
than a standing claim:

- The `headers` lane's Caddy half (real header injection) and its nginx half (an XFF-only
  misconfiguration, no `Forwarded`) — see [Reserved ports](#reserved-ports) above. The lane's
  hostile-input cases (malformed/duplicate headers) still run, against the in-process proxy.
- ~~Write-lane coverage beyond scale and restart: image update, rollback, drain, task removal, and
  the 409 stale-version conflict.~~ Covered: `write_sweep_test.go` drives all five, the conflict by
  `TestWriteSweepConcurrentScaleProducesAStaleVersionConflict`.
- ~~MCP grant-based `tools/list` filtering (only tier gating is covered), a task-augmented mutation
  polled to convergence, and cache-event notifications.~~ Covered: `tools/list` filtering by
  `mcp_sweep_test.go`, and the rest by `mcp_stream_test.go`, which drives the `subscriptions/listen`
  stream and its ACL filtering, the completions capability, and the tasks extension. What that lane
  cannot drive is a task-augmented mutation reaching the cluster at all — see finding D-12.
- A *successful* Tailscale authentication, in either mode. `tailscale_test.go` covers everything
  `TailscaleProvider` decides before it consults the daemon — the CGNAT/ULA address boundary, and that
  forwarding headers cannot forge a tailnet peer unless a trusted proxy is configured — plus tsnet's
  startup validation. A pass needs a real tailnet (a joined node, an auth key, control-plane access),
  so CapMap group extraction, `acl.TailscaleSource` and the tsnet dual-listener topology stay covered
  by `internal/auth`'s unit tests alone.
- A *successful* CIMD fetch in the MCP OAuth lane: the SSRF guard blocks loopback and private
  addresses, and `CIMDFetcher.AllowLoopback` is a test-only field no configuration exposes. What
  `oauth_test.go` does cover is that an `https://` client_id takes the CIMD path and that the guard is
  live in the shipped binary.
- The ACL case that a digest never names a resource behind a grant.
- ~~The OTLP tracing wiring.~~ Covered by `tracing_test.go`, which drives `CETACEAN_OTEL_ENDPOINT`
  against a stand-in collector. Driving it is what found that the documented endpoint exported
  nowhere: `WithEndpointURL` appends no signal path, so a collector base URL posted to `/` and the
  exporter dropped the 404 — startup logged "distributed tracing enabled" and nothing arrived.
- Two halves of the `If-Match` lane (`precondition_test.go`). `DELETE /plugins/{name}` is driven
  only for the no-representation case: there is no installed plugin to read a validator from, and
  its representation comes from a daemon inspect rather than the cache. `DELETE /nodes/{id}` is
  driven for every refusal but never for the accepted case, which would remove the swarm's only
  node. Both are recorded on the route's own entry, so the gate still counts them.
- ~~SSE 429 and `Retry-After` at the connection cap.~~ Covered by
  `TestSSEConnectionCapRefusesWithRetryAfter` in `sse_acl_test.go`, which opens `sse.MaxClients`
  real subscribers and asserts the next one is refused with 429, `Retry-After: 5` and an RFC 9457
  `SSE001` body. `internal/api/sse/broadcaster_test.go` still substitutes a `noopErrorWriter`
  rather than driving a real client past the cap, so this lane remains the only thing pinning it.

[the-browser-suite]: ../../docs/specs/2026-09-10-e2e-test-harness-design.md#the-browser-suite
[design]: ../../docs/specs/2026-09-10-e2e-test-harness-design.md
[topology]: ../../docs/specs/2026-09-10-e2e-test-harness-design.md#topology
[coverage]: ../../docs/specs/2026-09-10-e2e-test-harness-design.md#coverage
