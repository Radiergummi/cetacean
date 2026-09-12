# Architecture

Two things only: a map, so you know which package to open, and the traps that
cost someone a day. What a package holds and what its functions are called is in
the code, which cannot go stale; a decision and its reasoning belongs in
`docs/specs/<date>-<feature>-design.md`, which describes a moment and so stays
true. Anything written here in the present tense about a specific function is a
liability — it is the only part of the previous version of this file that rotted.

## Map

### Backend (`internal/`)

| | |
|---|---|
| `auth/` | Five pluggable providers behind one `Provider` interface: none, OIDC, Tailscale, cert, headers |
| `acl/` | Grant-based RBAC. `(resources, audience, permissions)` tuples, additive only, hot-reloaded from file |
| `config/` | Env/flag/TOML parsing. Everything optional |
| `cache/` | The in-memory swarm state, its change-history ring, the restart counter, and the disk snapshot |
| `docker/` | Engine API client and the watcher that keeps the cache current |
| `logs/` | Docker log-frame parsing shared by the REST and MCP transports |
| `api/` | The REST transport: router, handlers, content negotiation, SSE, feeds, CSV, problem details |
| `api/sse/` | The broadcaster behind every stream |
| `api/jgf/`, `api/dot/`, `api/graphml/`, `api/atom/`, `api/jsonfeed/`, `api/linkset/` | Pure serialization, no HTTP |
| `cluster/` | The domain layer both transports share — see the boundary rule below |
| `mcp/` | The embedded MCP server: tools, resources, prompts, widgets, OAuth, tracing |
| `prometheus/`, `prom/` | Query client and proxy; `prom/` is the three result types alone |
| `recommendations/` | The checkers and the engine that schedules them |
| `filter/`, `integrations/`, `metrics/`, `version/` | expr-lang filtering, third-party label detection, our own instrumentation, build stamps |

### Frontend (`frontend/src/`)

| | |
|---|---|
| `api/` | Fetch wrapper and the types mirroring the Go JSON |
| `hooks/` | `useSwarmQuery`, `useListPage`, `useDetailResource`, `useResourceStream`, `useAuth` |
| `components/` | `DataTable`, `log/`, `metrics/`, `search/`, and the shared primitives |
| `widgets/` | MCP Apps bundles, a second Vite target — one self-contained HTML file per widget |
| `lib/` | Chart palette, tooltips, shared constants |
| `pages/` | List and detail per resource type, plus search, swarm, topology |

## Boundaries

- A rule both transports must apply lives in `cluster/`. A projection only one of
  them renders does not — most of `cluster/` is MCP's alone and is there because
  MCP wants it below its transport, not because REST calls it. Putting a
  one-sided projection there invites the next reader to assume a sharing that
  does not exist, which is how the topology builders drifted.
- Dashboard affordances gate on the server-computed `Allow` header, never on a
  client-side reading of the policy.
- Widgets read data only through MCP tools, never Cetacean's HTTP API, so every
  read stays on the ACL-checked path.

## Traps

Each of these looks like a simplification and is not.

- **Proxy trust is decided once, at the edge**, on the address the connection
  actually came from, and carried in the request context. Re-deriving it
  downstream reads `RemoteAddr` *after* it has been rewritten to whatever the
  proxy named — which broke headers mode behind every proxy. The composed
  behaviour is covered in `internal/api/auth_proxy_test.go`, not in the provider
  tests, which is where it hid.
- **A path ending `.json` never reaches the mux.** `negotiate` strips a known extension and
  rewrites the path before routing, so the SPA fallback answers it. That is why the JWK Set
  is served at `/oauth/jwks` rather than the conventional `/.well-known/jwks.json`.
- **Docker ignores the `since` option for service logs.** Every caller offering a
  cursor must filter after parsing, which is why one function owns that and no
  caller does it itself.
- **A nil `*Client` in a non-nil interface is not nil.** The Prometheus client is
  optional, so `main.go` guards the assignment rather than assigning and
  nil-checking later.
- **An empty Prometheus result cannot distinguish an idle service from a cluster
  with no cAdvisor.** Metrics answers probe for the exporter before reporting
  zero, because a recommendation that stops on "no usage" would otherwise fire on
  a missing exporter.
- **MCP tools never take raw PromQL.** The label selector would be the caller's,
  so the call could not be ACL-checked at all.
- **mcp-go does not recover panics on the path our converging tools take.** That
  goroutine runs after the HTTP response is written, past the API's own recovery
  middleware, so the server option that installs recovery is the only thing
  between a panic in a mutation and a dead process.
- **mcp-go only cleans up a task when the client supplied a TTL**, so a hook
  fills one in; without it every mutation leaks a result for the process
  lifetime.
- **The task wait detaches the context**, because mcp-go runs the task on a
  goroutine holding the already-cancelled request context. A cancel therefore
  cannot interrupt the wait; the timeout is the real bound.
- **A tool has one output schema whatever its arguments.** A `raw` mode must add
  records *beside* the normal result, never replace it, or a strict client
  rejects the very call that asked for them.
- **Hosts disagree about where UI metadata lives**, so both the flat and the
  nested `_meta` keys are written.
- **The browser retries an SSE transport error itself but gives up forever on a
  non-2xx** — which is what every capped stream answers with. Reopening is
  therefore conditional on the connection being closed, not on any error. The log
  tail does not use `EventSource` at all, because it cannot see a 429's status or
  `Retry-After`.
- **One widget per Vite invocation.** Single-file output turns off code
  splitting, and Rolldown rejects that for a build with more than one input.
- **A widget must create exactly one host object and share it** — each one runs
  its own handshake. The tool arguments arrive as a notification that must be
  subscribed to *before* the handshake, or it is missed.
- **`DataTable` auto-loads the next page** from an observer sentinel whenever
  more remain, so a collection's `total` and its loaded items must never drift
  apart.
- **In widget tests use vitest's `vi.waitFor`, not React Testing Library's** —
  RTL cannot see fake timers and hangs until the test times out. React Flow draws
  an edge only once both endpoints are measured, which jsdom never does, so edge
  assertions belong in the layout unit test.

## Details that bite

- Config data comes back base64-encoded from the Docker SDK; the frontend decodes it.
- SSE connection caps answer 429 with `Retry-After`, not 503.
- `?limit=0` on search returns up to 1000 per type; the default is 3.
- pprof is opt-in and registered without a method prefix, so `go tool pprof` can POST.

## Supply chain

The SBOM and attribution files are committed and embedded in the binary. CI
regenerates them on every pull request and commits the result back to the branch,
then **dispatches the CI workflow explicitly** — a commit authored with
`GITHUB_TOKEN` raises no event, so the run that would put the branch's required
checks on the new head never starts, and the PR sits blocked on checks that
passed one commit earlier and can never appear on this one.

Two kinds of PR are reported on rather than written to. A fork's token is
read-only. A Dependabot PR could be written to but must not: Dependabot stops
updating any branch carrying a commit it did not author, and the frontend groups
share one lockfile, so a rebase is exactly what they need in any week both have
updates. Those merge with the SBOM stale, and the push-to-`main` run opens a
repair PR. So `main` may carry a stale SBOM briefly, which is why a release tag
is gated on the committed SBOM matching its manifests.
