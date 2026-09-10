# End-to-end test harness: design

## Problem

Cetacean's test suite is thorough about functions and thin about wiring. Every
package tests its own logic well, and nothing tests the binary: `main.go`
assembles config, TLS, auth, the watcher, the API and MCP into a running
process, and no test has ever started that process.

The gap is not theoretical. Cert mode behind a TLS-terminating proxy — the
configuration the [authentication docs][authentication] tell operators to
write — could not start at all: `LoadAuth` hard-required `auth.cert.ca`, and
`main.go` parsed the CA to build a `tls.Config` for a listener that never
existed. Both the provider
and the config package were green throughout. The composed behaviour was what
was broken, and nothing composed it.

The features most exposed are the ones nobody runs in production here. mTLS,
proxy-forwarded client certificates, OIDC, ACL hot reload and the MCP transport
are all configuration paths that a developer never exercises by accident. They
are covered by unit tests written by the same reasoning that produced the code,
which is exactly the coverage that fails to catch a wiring mistake.

What is missing is a way to start the real binary against a real Docker Swarm,
in each of its deployment shapes, and assert on what it actually serves.

## Decision

A local-only end-to-end harness: an ephemeral Docker-in-Docker Swarm, the real
`./cetacean` binary run as a subprocess in a per-case environment, and Go tests
asserting over HTTP and MCP.

Three constraints shape everything below.

**Not for CI.** The suite may take minutes, require a privileged container, and
depend on a locally built binary and fixture image. Freed from a CI budget, it
can afford to be faithful.

**Reproducible.** Every run starts from an empty cluster and an explicit
environment. No dependency on the developer's swarm, leftover stacks, or
`CETACEAN_*` variables in their shell.

**Debuggable by hand.** When a case fails against a real engine, the fastest
answer is usually to bring the environment up and look at it. The environment
is therefore a compose file a human can start, not a topology that exists only
inside a Go process.

Scope is the HTTP API, SSE and MCP. The browser stays out of the Go suite — the
SPA is not where the distrusted code lives — but it does not go untested: a
Playwright suite already exists and gets the environment it has always lacked.
See [the browser suite][the-browser-suite]. Prometheus-backed metrics are
deferred; see [phase two][phase-two].

## Topology

`test/e2e/compose.e2e.yaml` stands up the static cast on a dedicated network,
and nothing else:

| Service | Role |
|---|---|
| `dind` | `docker:28-dind`, privileged. The engine under observation; the harness runs `swarm init` against it through the SDK |
| `cert-init` | The openssl one-shot from `compose.dev-auth.yaml`, writing CA, server and client PEMs to a volume that is also bind-mounted to a host path |
| `dex` | OIDC issuer, config lifted from `compose.dev-auth.yaml` |
| `caddy` | mTLS termination, forwarding `Client-Cert` per [rfc9440] |

A second Caddy site injecting headers for `headers` mode, and an `nginx` service
reproducing the realistic `X-Forwarded-For`-only misconfiguration, were planned
here but not built in phase one; the `headers` lane's hostile-input cases run
against the in-process reverse proxy (`test/e2e/proxy/`) instead — see
[Coverage](#coverage) and the [README's Deferred section][deferred] for what
that leaves untested.

The certificate volume is bind-mounted to a host path because the Go side needs
the same chain the containers use: cert-mode cases dial the SUT's own TLS
listener with a client certificate, and they must present one the SUT's CA
accepts.

Two joints are load-bearing and worth stating plainly.

**The SUT is a host process; the proxies are containers.** They reach it via
`extra_hosts: host.docker.internal:host-gateway`, the mechanism
`compose.dev-auth.yaml` already relies on. This is the one platform-sensitive
part of the design.

**Proxy configuration is static, so SUT ports are fixed per lane.** Caddy cannot
discover a port chosen at runtime, so each auth mode reserves one (19001–19009,
mirroring dev-auth's scheme) and the harness starts the binary on the port for
the lane under test. Cases within a lane therefore run serially; lanes run in
parallel with each other.

## Layout

Everything is behind `//go:build e2e`, so `go test ./...` and `make check` are
untouched. `make test-stack` depends on `build`, so the binary under test is
never stale. (`make test-e2e` already names the Playwright suite.)

```
test/e2e/
  compose.e2e.yaml
  harness/    compose lifecycle, swarm init, SDK client, host-path cert loading
  sut/        binary supervisor: env, exec, readiness, log capture, stop
  fixtures/   baseline cluster and per-case throwaway stacks
  proxy/      in-process reverse proxy for hostile and malformed traffic
  *_test.go   the cases
```

## The SUT supervisor

`sut.Start(t, cfg)` returns a handle carrying a base URL, a preconfigured
`*http.Client` and a `t.Cleanup`-registered stop. Four decisions inside it:

**The environment is explicit and total.** The child receives exactly the
variables the case names, never the parent's. A stray `CETACEAN_*` in a
developer's shell silently changing a result is the failure mode that makes a
suite untrustworthy, and it is ruled out at the `exec.Cmd.Env` line.

**Failing to start is an outcome, not a harness error.** `StartExpectingExit`
returns exit code and captured stderr. This is what lets the suite assert on
startup validation at all: that cert mode with a trusted proxy and no CA now
comes up, and that cert mode terminating TLS without a CA still refuses. A
harness able only to express "it started and answered" cannot test a third of
what `LoadAuth` and `LoadTLS` do.

**Readiness is `GET /-/ready`; a timeout dumps the log tail into the failure.**
The supervisor buffers both streams. Because Cetacean logs structured JSON to
stderr, that buffer is also an assertion target — whether a deprecated setting
warned, whether a reconnect was logged.

**The hostile proxy needs no container.** It listens on loopback and the SUT
runs with `CETACEAN_TRUSTED_PROXIES=127.0.0.1/32`, so an in-process
`httputil.ReverseProxy` is trusted by construction and can emit anything:
duplicate `Client-Cert`, `Forwarded: for=unknown`, `::ffff:10.0.0.2` hops, an
XFF chain with no `Forwarded`. Every trust-decision edge is reachable against
the real binary.

`CETACEAN_DOCKER_HOST` passes a `tcp://` URL through to `client.WithHost`, so
pointing the SUT at DinD needs no production change.

## Fixtures

**The baseline cluster** is deployed once per run and shared by every read-only
case. Its contents are chosen so the derived and cross-referenced surface has
something to bite on: two stacks; a replicated, a global and a single-replica
service; a service mounting both a config and a secret; an overlay network and
a volume; a deliberately crash-looping service, so failed tasks, the restart
counter and the flaky-service recommendation have real input; and an orphan
config, secret, network and volume used by nothing, so the "used by services"
cross-references have an empty case. Labels carry
`com.docker.stack.namespace` plus Traefik, Shepherd, Diun and swarm-cronjob
markers — the only way `internal/integrations` is exercised at all.

**Images come from a locally built fixture image**, `docker save`-piped into
DinD at startup. One `FROM busybox` image serves every fixture service. Pulling
from a registry inside DinD on every run would make the suite both slow and
network-dependent.

**Convergence reuses `cluster.ServiceConverged`**, the same rule the MCP tasks
extension applies, so the harness and the product cannot disagree about what
"settled" means.

**Mutation cases** deploy a stack named for the test and the run, assert only on
their own resources, and remove it in cleanup. Read-only cases never mutate the
baseline, which is what keeps the suite in minutes rather than tens of minutes
without a restore step that can silently poison later cases.

## Coverage

A case earns a place here only if a unit test cannot honestly assert it.

The table states what the lane is meant to prove; where phase one shipped less
than that, the cell says so. See the [README's Deferred section][deferred] for
the full list, including the SSE 429/`Retry-After` case, which no test —
end-to-end or unit — currently pins.

| Lane | What only end-to-end can prove |
|---|---|
| `none` — API surface | Content negotiation through the real router, ETag and 304 round-trips, pagination `Link` headers, `Allow` reflecting the real ops level, and the three `/topology` renderings agreeing on one live graph |
| SSE | Subscribe to `/services`, then deploy, scale and remove a stack, asserting events arrive carrying full resources. The watcher-to-cache-to-broadcaster chain cannot be tested in-process without reimplementing it. (429 and `Retry-After` at the connection cap are deferred — not built here.) |
| `headers` | The hostile proxy for malformed input (duplicate/absent `Forwarded`, XFF fallback), against the real trust decision at the real edge. (Caddy injecting real headers and an nginx-shaped XFF-only misconfiguration are deferred — not built here.) |
| `cert` | Direct mTLS and Caddy-forwarded `Client-Cert` producing the same identity; duplicate `Client-Cert` rejected (against the positive case of a single, accepted header); an untrusted peer's `Client-Cert` ignored; both startup outcomes |
| `oidc` | The authorization-code flow against Dex — redirect, login form, callback, session cookie round-trip — then Bearer validation |
| ACL | Policy hot reload: rewrite the file mid-run and assert the verdict changes with no restart. `fsnotify` has no other honest test. Plus per-persona filtering and `Allow`. (That a digest never names a resource behind a grant is deferred — not built here.) |
| Writes and ops level | Scale and restart against a real engine. (Image, rollback, drain, task removal, and the 409 that needs a genuinely concurrent update to produce are deferred — not built here.) |
| MCP | `tools/list` filtered by tier, and `find` and `describe` agreeing on one live resource. (Grant-based filtering, a task-augmented mutation returning a converged result, and notifications firing from real cache events are deferred — not built here.) |

Metrics get one case in phase one: with Prometheus unconfigured, the nil-receiver
paths report 503 rather than charting nothing.

## The browser suite

`frontend/e2e/` already holds 24 Playwright specs, run by `make test-e2e`. What
they lack is not coverage but an environment: `playwright.config.ts` resolves
`baseURL` from `CETACEAN_E2E_URL` with a `localhost:9000` fallback and has no
`webServer` block, so the suite assumes Cetacean is already running against
whatever cluster the developer happens to have. It adapts to what it finds,
probing `/metrics/status` to decide whether metrics specs can run and gating
mutations behind `CETACEAN_E2E_WRITE`. That is the same reproducibility problem
this design exists to solve, one layer up.

The harness answers it without touching a single spec. Because the environment
is a compose file rather than a Go-internal topology, it can be brought up and
held: `make e2e-up` starts DinD, deploys the baseline fixtures and runs a
`none`-mode SUT on a fixed port; `make e2e-down` tears it down. Pointing
`CETACEAN_E2E_URL` at that port makes the existing suite reproducible.

The two suites stay decoupled. Playwright does not learn about the Go harness,
and the Go harness does not run Playwright. What they share is an environment
and a documented URL.

Naming, because `test-e2e` is taken: the Go suite runs under `make test-stack`,
with `e2e-up` and `e2e-down` managing the shared environment.

## Build order

Harness, supervisor and the `none` lane first — that combination proves the whole
machine works and is the cheapest thing to debug when it does not. Then `cert`,
which is the motivating risk; then ACL; then writes; then SSE; then MCP. `oidc`
last: driving a login form is the fiddliest lane and the least likely to reveal
a wiring bug the others miss.

## Phase two

Prometheus with a seeded TSDB. The sizing checker looks back 168 hours, so there
is no live-scraping path to usable data; the faithful option is generating an
OpenMetrics file with timestamps relative to run time and converting it with
`promtool tsdb create-blocks-from openmetrics` into blocks mounted in the
container. That gives real PromQL against real Prometheus, which matters because
`internal/mcp/metrics.go` duplicates the dashboard's queries by design and
nothing currently checks that either copy parses.

The cost is hand-authoring cAdvisor-shaped series carrying
`container_label_com_docker_swarm_service_name` and node-exporter series keyed by
`instance` — without them the probe in `requireExporter` reports no exporter and
every metrics assertion tests the wrong branch. That is a meaningful slice of
work aimed at the least-suspected code, which is why it is deferred rather than
dropped.

A browser lane over the SPA is a separate decision, not a later phase of this
one. It would test a different risk and needs its own design.

[authentication]: ../authentication.md
[phase-two]: #phase-two
[the-browser-suite]: #the-browser-suite
[rfc9440]: https://www.rfc-editor.org/rfc/rfc9440.html
[deferred]: ../../test/e2e/README.md#deferred
