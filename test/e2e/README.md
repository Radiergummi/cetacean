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

(That's `CETACEAN_E2E_URL`, matching `playwright.config.ts`'s `baseURL` resolution.) Specs gated on
Prometheus (via `/metrics/status`) skip, since this environment has none — see
[Deferred](#deferred) below. Specs gated behind `CETACEAN_E2E_WRITE` also skip unless you set that
variable; the baseline fixtures are shared and idempotent, so mutating them isn't the default.

### Known spec failures against this environment

The first real run of `frontend/e2e` against a reproducible cluster (105 passed, 30 skipped, 16
failed at the time of writing) turned up genuine mismatches between the suite and the current
dashboard — not fixture gaps, and none fixed here, per this task's brief: don't edit specs to make
them pass. Recorded so nobody re-diagnoses them from scratch:

- **`DataTable`'s ARIA role.** The accessibility pass in `6fed71d5` gave `DataTable` an explicit
  `role="grid"` (with `aria-activedescendant` keyboard navigation), which overrides the `<table>`
  element's implicit `table`/`row`/`cell` roles. Every spec still locating rows with
  `page.getByRole("table")` / `getByRole("cell")` no longer finds them —
  `nodes.spec.ts:4,32`, `services.spec.ts:4,22,42`, `sse.spec.ts:16`, `stacks.spec.ts:4`,
  `tasks.spec.ts:4`. (Locators built on `page.locator("table tbody tr")`, a plain CSS selector, are
  unaffected — that's why "row click navigates to detail" tests still pass.)
- **`SearchPalette`'s markup.** The same pass moved grouped results to an ARIA
  listbox/option/group pattern; the group label is a `<span>`, not a `<section><header>`.
  `global-shell.spec.ts:149` still looks for `section header` and finds nothing.
- **A real bug**: `ErrorIndex.tsx` passes the whole `/api/errors` response — the JSON-LD
  `CollectionResponse` envelope (`{"@context", "@type", "items": [...]}`) — straight to
  `setErrors`, then does `for (const definition of errors)` over it. That throws (`e is not
  iterable`), and the page's `ErrorBoundary` catches it, so `/api/errors` shows the generic error
  screen instead of the reference table. `errors.spec.ts:21,29` fail on it. The fix is
  `.then((body) => setErrors(body.items))`; nothing in this task's brief covers touching frontend
  source, so it's left as found.
- **Row clicks landing on a nested link.** `.click()` on a `<tr>` clicks its bounding-box center,
  which — on the tasks table's current column layout — lands on the `Node` column's hostname link
  rather than triggering the row's own navigation, so the browser follows the link to
  `/nodes/<id>` instead. `tasks.spec.ts:16,31,37` and `log-viewer.spec.ts:82` (which all navigate
  to a task via a row click) land on a node page and time out waiting for a `/tasks/.+` URL.
- **An ambiguous accessible name.** `services.spec.ts:110` asks for
  `getByRole("button", { name: /^Labels$/i })` and gets a strict-mode violation: four buttons on a
  service detail page currently share the name "Labels".

None of these are specific to the harness or the fixture cluster's data — they reproduce against
any real cluster once one is up, which is exactly why they were invisible before this task: the
suite had never had one.

One more thing, unconfirmed: a `--workers=1` diagnostic run against the same long-lived `cetacean`
process, after several minutes of continuous prior load from repeated Playwright runs, showed
failures cascading and getting faster near the end — including plain HTTP-only specs — suggesting
the SUT itself may degrade under sustained load (a connection or goroutine leak is a plausible
suspect, though unconfirmed). This did not reproduce in the canonical single `make e2e-up` →
`make test-e2e` → `make e2e-down` sequence above, so it wasn't chased further, but it's worth
watching for if the suite is run repeatedly against one long-lived environment.

## Constraints

- **`-p 1` is required** when running more than one package (`./test/e2e/...`). Packages share the
  same fixed lane ports (see the table above), so parallel packages fight over them.
- **No `t.Parallel()`.** Same reason, at the test level.
- **The formatting gate is `golangci-lint fmt`, not `gofmt`.** CI (and this suite's own checks) run
  `golangci-lint fmt --diff ./... | diff /dev/null -`, which fails on any output. Run
  `golangci-lint fmt ./...` before committing.
- Every file here — and `test/e2e/cmd/e2eenv/main.go` — carries `//go:build e2e`, so `go test ./...`
  and `make check` never touch it.

## Deferred

Prometheus-backed metrics are not seeded here. The one metrics case this suite covers is that an
unconfigured Prometheus reports 503 rather than an empty series; a seeded TSDB with realistic
cAdvisor/node-exporter series is phase two — see the design doc's [Phase two][phase-two] section
for what that would take and why it's deferred rather than dropped.

Beyond that, this pass shipped less than the design doc's [Coverage][coverage] table describes.
Nothing below is exercised by this suite, and — except where noted — nothing else pins it either:

- The `headers` lane's Caddy half (real header injection) and its nginx half (an XFF-only
  misconfiguration, no `Forwarded`) — see [Reserved ports](#reserved-ports) above. The lane's
  hostile-input cases (malformed/duplicate headers) still run, against the in-process proxy.
- Write-lane coverage beyond scale and restart: image update, rollback, drain, task removal, and
  the 409 stale-version conflict.
- MCP grant-based `tools/list` filtering (only tier gating is covered), a task-augmented mutation
  polled to convergence, and cache-event notifications.
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
- ~~SSE 429 and `Retry-After` at the connection cap.~~ Covered by
  `TestSSEConnectionCapRefusesWithRetryAfter` in `sse_acl_test.go`, which opens `sse.MaxClients`
  real subscribers and asserts the next one is refused with 429, `Retry-After: 5` and an RFC 9457
  `SSE001` body. `internal/api/sse/broadcaster_test.go` still substitutes a `noopErrorWriter`
  rather than driving a real client past the cap, so this lane remains the only thing pinning it.

[the-browser-suite]: ../../docs/specs/2026-09-10-e2e-test-harness-design.md#the-browser-suite
[design]: ../../docs/specs/2026-09-10-e2e-test-harness-design.md
[topology]: ../../docs/specs/2026-09-10-e2e-test-harness-design.md#topology
[coverage]: ../../docs/specs/2026-09-10-e2e-test-harness-design.md#coverage
[phase-two]: ../../docs/specs/2026-09-10-e2e-test-harness-design.md#phase-two
