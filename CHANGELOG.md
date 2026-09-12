# Changelog

All notable changes to Cetacean will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Any list can be downloaded as CSV — add `.csv` to the URL or ask for `text/csv`. Search, filters and sorting apply; a download that asks for no page gets every row
- The dashboard is installable as an app, with icons and a theme colour that follows its own background
- The cluster can be searched from the browser's address bar, via the OpenSearch description at `/opensearch.xml`
- `/.well-known/api-catalog` (RFC 9727) lists the APIs this process serves; every response links to it
- Write operations accept an optional `If-Match` header and refuse with 412 if the resource changed since you read it
- `Prefer: wait=30` holds a service write open until the cluster settles, answering `202 Accepted` with the rollout's progress if it runs out. `Prefer: respond-async` acknowledges immediately
- JSON, Atom feeds and topology exports above a kilobyte are served as zstd or gzip when the client accepts one
- Cross-site request forgery protection covers every write. An origin must be in `server.cors.origins`; `server.public_url` is trusted automatically. A wildcard cannot grant writes, so a read-everything deployment must now name its writers
- Client certificate authentication works behind a TLS-terminating proxy that forwards the certificate in `Client-Cert` (RFC 9440)
- `Forwarded` (RFC 7239) is read alongside `X-Forwarded-For` when resolving the client address behind a trusted proxy
- The MCP authorization server publishes the public key that verifies its access tokens, as a JWK Set at `/oauth/jwks`. Anything checking a token Cetacean issued no longer needs a key that could issue one
- The documentation site is navigable by an agent: every page has a Markdown version, `/llms.txt` lists the site, and `/openapi.json` describes what it serves

### Changed
- The dashboard's first load is about a third of its former size, and hashed assets are cached permanently
- The API reference at `/api` is six months newer, and now follows Scalar releases automatically
- MCP access tokens follow the RFC 9068 `at+jwt` profile. Clients holding an older token refresh automatically
- The Cetacean API description moved to `/api/openapi.yaml`; `/openapi.json` now describes the documentation site itself
- **Breaking:** `CETACEAN_MCP_SIGNING_KEY` is now a root secret both keys derive from, and must be 32 bytes of hex or base64 — generate one with `openssl rand -hex 32`. Leaving it unset still generates a key at startup
- MCP access tokens are signed with ES256 rather than HMAC. Clients refresh once on upgrade; stop every replica before starting the new version
- An endpoint with only one representation no longer answers 406 to an `Accept` header it does not recognise

### Fixed
- Everything that does not describe the cluster keeps working while the Docker daemon is unreachable — the dashboard's own icons and manifest, the API catalogue, the OpenSearch description and `/profile`
- Header-based authentication works behind a reverse proxy again; it was answering 401 to every request
- Asking an endpoint for a format it does not serve now says so, instead of answering with JSON
- Relabelling a node needs operations level 2 over the API, matching MCP. It was gated with draining and demoting
- An Atom feed identifies itself by the same host its links use. Set `server.public_url` to keep that identity stable across proxies, since nothing derived from a request can be
- Feed alternate links carry only the parameters the feed they point at reads
- The API documentation, the playground script, the JSON-LD context and the attribution documents are cacheable, and compressed once rather than on every request
- Documentation pages no longer advertise a `.html` canonical URL that nothing links to

## [0.14.0] - 2026-09-10

### Added
- `server.public_url` sets the canonical external URL once, instead of configuring the MCP OAuth issuer and the OIDC redirect separately
- The documentation site carries an error reference: every code the API can return, its status, meaning and resolution

### Changed
- Deploying as a stack no longer needs an overlay network created first — `docker stack deploy -c compose.yaml cetacean` stands alone. Add `-c compose.prometheus.yaml` to connect the bundled Prometheus
- Status colours are consistent across the dashboard and adapt to dark mode, drawing from one palette of five meanings
- The service and stack lists head their rollout column "Rollout" rather than "Status", and no longer colour a settled rollout green — it reports Docker's update state, not whether the service is running
- MCP tools advertise the values each argument accepts, so a client can offer them. `get_logs` gained the `fatal` level
- The MCP tool reference gives every tool its own linkable entry rather than a table per operations level
- The topology view draws its graph immediately instead of waiting for the layout engine to download
- A dropped connection or server restart no longer leaves a page stuck on an error; transport failures are retried briefly

### Fixed
- Metrics charts are live again — none of them ever opened the streaming connection, so they only moved on the refresh timer
- Keyboard navigation in resource tables no longer loses its place when an update arrives over the live connection
- Searching a live log keeps your place; stepping through matches was pulled back to the first hit on every new line
- Service cards in the topology view show how many replicas are actually running, and global services report tasks up rather than 0/0
- The topology view no longer labels every service "Updating…" — Docker leaves a finished rollout on a service indefinitely
- Stacks in the topology view can be told apart by colour again
- The dashboard has a favicon, and dark mode no longer flashes white before the page appears
- The dashboard loads in a browser configured to block site data, falling back to defaults instead of a blank page
- Resource tables can be sorted from the keyboard, and screen readers are told which column is sorted and which way
- Form labels across the integration panels, mount editor and swarm settings are tied to their fields, so a screen reader announces both label and hint
- The search palette and shortcuts overlay hold keyboard focus while open and return it on close
- Service cards and edge-tooltip network links in the topology view can be reached and opened with the keyboard
- Pressing Enter submits the create-config and create-secret dialogs
- The keyboard shortcuts overlay shows that its list continues below
- Alternating row shading no longer inverts while scrolling a long table or log
- The confirmation button in the command palette is legible again
- The documentation site's "Edit this page on GitHub" link points at the file a page was written from, and diagram pan and zoom controls are announced to screen readers
- Startup no longer stops for every authenticated deployment over an unreachable MCP OAuth issuer — only when MCP sign-in is actually in use
- Atom and JSON Feed links are built from `server.public_url` when it is set, rather than from request headers
- `make build` works from a fresh clone; it previously failed because the binary embeds a frontend that had never been built
- The example config offered `acl_claim`, `acl_capability` and `acl` keys that do not exist, so anyone uncommenting one got no grants and no warning. The working keys are `oidc_claim`, `tailscale_capability` and `headers_acl` under `[acl]`

## [0.13.0] - 2026-09-07

### Added
- MCP clients that support interactive apps render results as widgets: a sortable table, a topology graph, a live log tail, a metrics chart, and recommendations grouped by severity
- AI agents can ask what is using the most CPU, memory or network, cluster-wide or on one node
- AI agents can ask what draining a node would move, where it would go, and which services have nowhere to go
- AI agents can ask whether the cluster is healthy and be told what is wrong — degraded services and nodes, rollouts in flight, capacity reserved
- AI agents can ask what changed and when, narrowed to a time range, a resource type or one resource
- AI agents can read and search logs across a stack or the whole cluster in one call, each line naming its service
- AI agents can wait for a service to finish deploying, and are told how far the rollout got if it never settles
- AI agents can change a service's mounts, secrets, configs, health check and command, and create secrets and configs — completing the rotation sequence. A secret's value is never returned by any tool
- AI agents can chart CPU, memory or network for a service or node over the last hour, six hours, day or week. Requires Prometheus
- AI agents can ask for Cetacean's recommendations directly, optionally only the critical ones
- AI agents can list a whole resource type, paged, rather than searching one name at a time
- MCP clients are offered named investigations — diagnose an unhealthy service, explain why one will not schedule, review capacity, roll back, right-size, drain a node — filtered to what the caller could carry out end to end
- MCP clients can complete a resource or prompt argument, offering names rather than IDs and only resources the caller may see
- Resources can be addressed by name as well as by ID — `cetacean://services/web`. An ambiguous name is refused, naming the candidates
- `find` and `describe` return links to the resources they are about
- Approving an MCP client is remembered for 90 days instead of being asked on every expiry. `CETACEAN_MCP_CONSENT_TTL=0` always asks
- Cetacean exports traces to an OpenTelemetry collector via `CETACEAN_OTEL_ENDPOINT`, joining a calling agent's trace where there is one
- Cetacean identifies itself to MCP clients with a display name, website and icon
- `CETACEAN_MCP_SIGNING_KEY_FILE` reads the signing key from a file, so it can be supplied as a Docker secret
- MCP list and read responses carry cache freshness hints, so agents poll less
- The licenses page shows the full text and NOTICE of every bundled dependency, filterable by license and ecosystem, with the whole attribution document as a download

### Changed
- **Breaking:** the MCP server speaks protocol revision 2026-07-28 only. Older revisions are refused with an error naming the version to use — update your client if it cannot negotiate it
- **Breaking:** AI agents receive a compact description of a resource rather than raw Docker output. Listing and searching are now one `find` tool, with a new `describe` for detail; raw records are still available on request
- **Breaking:** the eleven MCP tools that edited a service or node specification are now `update_service` and `update_node`, naming the part they change in a `section` argument. `update_node_labels` stays separate because it needs a lower operations level
- **Breaking:** `update_service` no longer accepts `secrets`, `configs` or `mounts` as a `section`. These reported success and changed nothing; each now has its own tool
- **Breaking:** the MCP tools that edit a specification return a compact summary of the section they changed rather than the whole Docker object, and every tool now publishes an output schema
- **Breaking:** the MCP tools that scale, update the image of, roll back or restart a service return where the service ended up, not its full specification
- **Breaking:** the MCP `search` tool returns `results`/`counts`/`total`, matching the HTTP search response
- **Breaking:** `/topology/networks` and `/topology/placement` are removed. `/topology` has replaced them since 0.12.0 and both paths answer `410 Gone` naming it
- **Breaking:** `CETACEAN_MCP_SESSION_IDLE_TTL` and `CETACEAN_MCP_MAX_SESSIONS` are removed — the protocol has no sessions. Both are ignored if still set
- **Breaking:** an MCP signing key shorter than 32 bytes is refused at startup rather than used. Leaving it unset still generates a random one
- MCP authorization responses identify the issuer (RFC 9207), so a client cannot be tricked into redeeming a code at the wrong one
- Client ID Metadata Documents are now the recommended way for MCP clients to identify themselves, and are advertised as such. Dynamic Client Registration still works
- MCP clients registering dynamically declare whether they are a native or web application, and are held to the redirect URIs that implies
- A cross-type MCP search reports how many matches each resource type holds alongside the total
- The documentation has been rewritten end to end, and the MCP page split into a setup guide and a tool reference

### Fixed
- **Security:** the MCP tools that edit a service's specification no longer return its environment variable values or log-driver options — an agent raising a CPU limit was handed the service's credentials
- An MCP client whose permissions match no grant no longer receives change notifications, which leaked the timing of changes it cannot see
- Listing tasks over MCP no longer names a service or node the caller has no permission to read
- Per-service charts and every sizing recommendation were silently empty on the bundled monitoring stack. cAdvisor reads container metadata through containerd, whose socket is now mounted
- The bundled monitoring stack applies its own Prometheus configuration; it was mounted where Prometheus does not read it, so no exporters were ever discovered
- Node charts show data when Prometheus scrapes the exporters over an overlay network
- The monitoring stack and Cetacean's stack share the overlay network they are documented to share, and `docker stack deploy -c compose.yaml cetacean` works on a fresh swarm
- Cetacean no longer recommends shrinking services it has no measurements for; missing figures were read as a measured zero
- Right-sizing recommendations state current, configured and suggested CPU in the same unit — a suggestion could look like a thousandfold change
- Replica counts are correct for services that restart frequently. Tasks Swarm had replaced were counted as running until the next re-sync, so a crash-looping service could report thirteen replicas against a desired one
- A service restarting in a loop is reported as failing rather than healthy, and appears in the cluster's list of what is wrong
- Waiting for a deploy no longer reports failure on a rollout that succeeded, nor success the instant it starts
- The topology view includes services published with DNS round-robin, which have no virtual IP and so appeared connected to nothing
- The physical topology view no longer counts a replaced replica twice
- `/topology` returns a stable ETag, so a conditional request is answered "not modified" instead of re-sending the graph
- Live pages, charts and the connection indicator recover when the server is momentarily at its connection limit, instead of staying silent until reload
- Live log tail reconnects on its own, resumes where it left off, and shows what it is doing; at the server's connection limit it counts down rather than showing a cryptic error
- Live log tail no longer sits empty behind a "Live" badge when the time range is a duration, at whole-second precision, in a non-UTC zone, or the local clock runs ahead
- Multi-line log output is no longer repeated in full on every reconnect, and is attributed to the task that produced it
- Log lines from services with several replicas are no longer dropped or shown twice around a reconnect
- Live log tail sends you to the login page when your session expires
- Log reads say when they could not reach the start of the requested window, or when a search ran out of budget before the older lines
- Searching within a live log, and the change timeline, now report how far back they can answer for
- Browsers no longer reconnect in lockstep after a restart, colliding and synchronizing harder each round
- Charts plotting a single value no longer drop to zero on every live update, and keep streaming after a tab switch
- Resources created while a list page is open appear in the list, instead of the page growing by a repeated row
- Lists hold a consistent order, so returning to one after visiting a resource no longer means hunting for the row you clicked
- The Tasks page no longer flips between the list and a "range start is beyond the total number of items" error
- Breadcrumbs on a resource in a stack lead through the stack, and a resource that belongs to none no longer shows a stack prefix that is not there
- Plugins on the Swarm page show their type instead of "undefined.undefined/undefined", and scoped npm packages on the licenses page show their full name
- The keyboard shortcuts overlay lists every shortcut, and describes `/` as opening the command palette
- Editing a service's ports by reading them and writing them back no longer destroys the published port. Any section given a field it does not have is now refused, naming it
- The change history for a single resource no longer stops after 64 entries while reporting itself complete
- Label keys containing a hyphen are accepted when editing a node's labels, as Docker allows
- A service running an image from a private registry on a port no longer links to a Docker Hub page that does not exist
- AI agents are told the truth about a change they just made. Scaling reported the service as it was *before* the call, and returned a version the next change would have rejected
- Asking an agent for metrics now says when nothing is collecting them, rather than returning an empty chart indistinguishable from an idle service
- An agent can tell that a service is stuck in a restart loop; a description now carries how often its replicas have died in the last hour and week
- Recent change history over MCP identifies a task by its service and slot instead of repeating the task's own ID
- Asking for a dead replica's logs over MCP explains that the output is gone for good and names the service to read instead
- **Breaking (MCP):** a service description reports its entrypoint as `command` and its arguments as `args`, the split Docker itself makes. The entrypoint was previously hidden entirely
- **Breaking (MCP):** the cluster overview states every figure in a named unit and gives both CPU numbers in cores. Reserved CPU was in nanoCPUs, so an agent comparing the two was wrong by a factor of a billion
- **Breaking (MCP):** a recommendation that can be acted on names the tool that does it, rather than an HTTP route an agent cannot call
- A service description reports a health check's probe command, timeout, start period and retry count, not just its interval
- Asking an MCP tool for a raw Docker record no longer breaks the shape it promised; raw records come back alongside the compact answer rather than in place of it
- Addressing a task by name over MCP works for services whose name contains a dot
- Reading a service's logs over MCP by name works on deployments that use an authorization policy
- AI agents reading service logs over MCP advance through history correctly, and receive at most the number of lines they asked for
- Stack-scoped permissions reveal the MCP tools and investigations they cover, and the change notifications for services inside the stack
- An MCP client whose permissions match no grant no longer sees tools it cannot use
- AI agents on the new protocol revision receive live cluster updates; subscribing succeeded and then delivered nothing
- MCP clients running in a browser can reach the server — cross-origin preflight rejected every header the transport requires
- MCP clients no longer re-authorize on every restart; refresh tokens are kept in the data directory. Set `CETACEAN_MCP_SIGNING_KEY` to keep access tokens valid too
- An agent asking for a service change as a task no longer grows Cetacean's memory indefinitely. A default expiry applies when none is asked for (`CETACEAN_MCP_TASK_TTL`, 15 minutes), capped by `CETACEAN_MCP_MAX_TASK_TTL`
- `CETACEAN_MCP_CONSENT_TTL=0` works as documented; setting it stopped Cetacean from starting
- Turning off `CETACEAN_MCP_CIMD_ENABLED` takes effect; the setting was ignored
- The example Compose file gives Cetacean a volume for its state, and names the published image rather than a local tag
- The Tailscale tsnet example sets the state directory it mounts a volume for, so the node no longer re-authenticates on every restart
- The API documentation no longer describes MCP tools that do not exist, and documented task states, media types and feed formats match what the server accepts
- The configuration reference documents every setting; the MCP, authorization and sizing settings were absent
- The documentation site publishes again — one page without the required metadata had been failing every build
- Corrected several documentation claims: the log viewer does not tail until turned on, the logical topology draws one edge per pair of services, Swarm ignores Compose's `depends_on`, and the default operations level permits operational writes rather than everything
- Release binaries carry build provenance, as the container image already did
- The SBOM no longer ships without the integrity hash of every npm dependency, which the frontend build strips from the installed tree

## [0.12.0] - 2026-08-28

### Security
- Updated Go, dashboard and website dependencies to clear every outstanding advisory, including a critical one in the OpenAPI parser (GHSA-r277-6w6q-xmqw)
- Rejected OIDC bearer tokens no longer reach the server log
- A `Request-Id` header forwarded by a proxy is only echoed back when it is letters, digits, dashes, underscores and dots; anything else is replaced

### Added
- Embedded Model Context Protocol server, opt-in via `CETACEAN_MCP=true`, exposing cluster state to AI agents at `/mcp` — twelve resources and twenty-three tools across the read, operational, configuration and impactful tiers
- OAuth 2.1 authorization server for MCP clients: Dynamic Client Registration, Client ID Metadata Documents, resource indicators, PKCE-only flows and refresh token theft detection
- `CETACEAN_MCP_ISSUER` sets the canonical OAuth issuer URL when Cetacean runs behind a reverse proxy
- MCP authorization metadata is also served at the OpenID Connect discovery path, for clients that look there
- The MCP endpoint rejects a request carrying a disallowed `Origin` with 403, a DNS-rebinding defence. The allowlist is `CETACEAN_CORS_ORIGINS`; non-browser clients send no `Origin` and are unaffected
- MCP tools return machine-readable results alongside the text form, so an agent need not scrape JSON out of a blob
- Every MCP tool and resource carries a title, a description of when to use it, behavioural hints and an icon, so clients can render confirmation prompts accurately
- The MCP server describes itself on connect: the read-mostly model, that writes are gated by operations tier and ACL, and to resolve a resource before acting on it
- Open-source licenses page, linked from the footer, listing every bundled Go module and frontend dependency with search and per-ecosystem filtering. The bill of materials is at `/-/sbom.cdx.json`

### Fixed
- Resource pages no longer crash on fields the Docker Engine omits — nodes that have not reported a hostname, platform, resources or address; tmpfs mounts; unset ports and gateways; services with no image
- The service detail page no longer crashes on a task update arriving over the live stream, or when a service's task template is absent
- Swarm CA and Raft panels no longer show `undefined` for settings Docker omits at their default
- Plugin type displays as `docker.volumedriver/1.0` rather than `[object Object]`
- node-exporter is detected by its metrics, not a Prometheus job literally named `node-exporter` — any other naming hid the node CPU, memory and disk columns. An unreachable target reports as undetected rather than rendering empty panels
- Metrics charts and the console no longer break on Prometheus responses carrying no samples, and the console renders scalar and string results such as `time()`
- Unexpected errors show a recovery screen with reload and try-again, with technical detail collapsed
- Confirmation dialogs dismiss themselves once the action is confirmed instead of staying open
- Fixed latent crashes in the disk-usage chart tooltip, the log-driver options editor, and keyboard navigation on empty radio-card groups
- Editing a service's environment variables or labels merges against the current spec instead of replacing the whole map, and concurrent writes to unrelated keys are no longer lost
- MCP OAuth discovery works under a base path; a client deriving the metadata URL from the issuer previously got a 404
- `CETACEAN_MCP_AUTH_BYPASS` takes effect, so MCP clients can reach `/mcp` on upstream identity such as mTLS without a bearer token
- MCP `tools/list` hides write tools the caller has no grants for, and change notifications skip clients that cannot read the affected type
- MCP ACL checks for logs, task removal and config, secret and network removal now match the REST policy semantics they diverged from
- Stack and volume detail reads over MCP make "denied" and "not found" indistinguishable from outside
- MCP `search` rejects an empty query instead of returning every cached resource, and no longer advertises a `types` parameter it ignored
- `update_service_image` rejects an empty image string
- Destructive MCP tools — restart, rollback, node availability and role — advertise it, so clients can gate them behind a prompt
- MCP client metadata documents are validated and fetched over a connection pinned to a checked address, so a malicious host cannot inject a `javascript:` redirect or reach an internal one
- MCP PKCE verifiers are compared in constant time and held to the length and alphabet the spec requires
- MCP refresh-token history, abandoned authorization codes and registration rate-limit buckets are bounded, so a long-running server no longer accumulates them
- A refresh-token grant family respects its absolute lifetime, and a resource-indicator typo no longer burns the family
- MCP registration rejects unsupported `grant_types` and `response_types` rather than storing them, and caps request bodies
- The MCP consent page no longer surfaces raw fetcher errors, and is not cached, so a back-button replay cannot surface another user's session
- Reading logs over MCP returns promptly when Docker leaves the stream open
- A service is reported as `updating` while a rollback is in progress, not only a forward update
- Website dependencies can be installed again; an unresolvable Astro peer conflict made every `npm install` fail

## [0.11.2] - 2026-05-20

### Security
- Bumped `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` to v1.43.0 to pick up the fix for unbounded HTTP response bodies (GHSA-w8rr-5gcm-pp58)
- Pinned `fast-uri` ≥ 3.1.2 via npm overrides in the dashboard, clearing the path-traversal and host-confusion advisories (GHSA-q3j6-qgpj-74h6, GHSA-v39h-62p7-jpjc)
- Pinned `hono` ≥ 4.12.18, `ip-address` ≥ 10.1.1, `postcss` ≥ 8.5.10 in the dashboard's transitive (dev-only) deps
- Updated the marketing site's deps via `npm audit fix`, clearing the critical `protobufjs` arbitrary-code-execution advisory (GHSA-xq3m-2v4x-88gg) and the bundled `dompurify`/`astro` advisories

### Fixed
- Production frontend build no longer pulls test files through `tsc`, so test-only Node imports stop breaking the release pipeline

## [0.11.1] - 2026-05-20

### Added
- Atom feed support on resource endpoints — request via `Accept: application/atom+xml` header or `.atom` URL suffix
- Feed icon button in page headers for pages with Atom feeds
- `Link: rel="alternate"` header on JSON responses advertising the Atom feed URL
- API reference shows operations-level badges on write endpoints, an experimental marker on the recommendations endpoint, and human-readable descriptions for enum values (recommendation categories, node availability/role, service mode, auth providers)
- Manual resync button next to the live-connection indicator and `POST /-/resync` endpoint for forcing a full re-fetch when the cache appears stale

### Fixed
- Service resource charts now scale reservation/limit threshold lines by replica count so they line up with the service-wide usage curve (a 3-replica service with a 1 GB/task limit no longer appears to exceed its limit at 1.5 GB usage)
- Node and task detail pages no longer crash when Docker briefly returns a payload with null Description/Status/Spec fields (the previous `c.Resources is undefined` error)
- Cache no longer drifts after rapid stack deploys: transient inspect failures retry with backoff instead of being silently dropped
- Task SSE events now also fire when a task's status message changes — previously the stream silently coalesced these
- Log viewer follow-mode keeps up with high-volume streams instead of disabling itself a few lines after Jump to Bottom
- Loading older log pages no longer jumps the viewport to a wrong position
- Live log indices are re-stamped after the in-memory buffer trims so search highlight and pinning stay correct
- Late-arriving log fetches can no longer overwrite newer results during rapid filter/time-range changes
- Container-registry icons are bundled inline instead of loaded from `github.com`, `hub.docker.com`, etc. — the previous URLs were blocked by the dashboard's CSP
- Tightened the `img-src` CSP directive now that no external images are referenced
- Filter expressions like `exit_code != "0"` no longer match running tasks (Docker reports `-1` mid-run)
- Task detail CPU/memory gauges fall back to the host node's capacity when the service has no per-task limit set, instead of rendering empty
- Exit code is no longer shown on running tasks (Docker often reports `-1` while a container is alive)
- Node list no longer crashes when a node briefly arrives without a Description payload
- SSE drops caused by slow clients are now surfaced as the `cetacean_sse_client_events_dropped_total` metric so silent drift can be detected
- Corrected OpenAPI spec examples to match actual API responses
- Documented missing OpenAPI endpoints and parameters: `POST /swarm/unlock`, `GET /topology`, `GET /services/{id}/mode`, `GET /services/{id}/endpoint-mode`, `?force` on node and volume deletion, and `application/merge-patch+json` support on env and label PATCH endpoints
- Removed dead `GET /swarm/plugins` alias route
- Empty cross-reference and collection fields now serialize as `[]` instead of `null` on config/secret/network/volume/stack detail, service sub-resources (configs, secrets, networks, mounts), stack listings, and the recommendations endpoint
- Flaky-service warnings now report actual task failures instead of inflated counts derived from a misused Prometheus metric — restarts are tracked from swarm events and persist across restarts via the snapshot. Flaky-service detection no longer requires Prometheus.

## [0.11.0] - 2026-04-02

### Added
- General trusted proxies setting (`CETACEAN_TRUSTED_PROXIES`) for real client IP resolution behind reverse proxies — replaces the headers-auth-specific setting, which is now deprecated
- Client IP in structured request logs when trusted proxies are configured
- CLI flags for all settings that were previously env-var-only: `-operations-level`, `-sse-batch-interval`, `-cors-origins`, `-snapshot`, `-data-dir`, `-trusted-proxies`
- Gzip compression for snapshot files (existing plain JSON snapshots are read transparently)
- Tailscale auth mode comparison table in the authentication docs
- Configurable CORS support for cross-origin API access (`CETACEAN_CORS_ORIGINS`)
- Grant-based RBAC authorization with per-resource access control
- `Allow` response header indicating available methods per resource — the dashboard uses this to show or hide write controls per resource and per user
- `Accept-Patch` response header advertising supported patch formats per resource (RFC 5789)
- `Prefer: return=minimal` support on all write endpoints (RFC 7240)
- `Last-Modified` / `If-Modified-Since` conditional requests on resource detail endpoints
- `Strict-Transport-Security` (HSTS) header when TLS is enabled
- Structured error responses (RFC 9457) for all authentication and OIDC callback errors
- SSE keepalive comments on idle connections to prevent proxy timeouts
- Footer with version info, GitHub, docs, and API links
- Table/grid view toggle on the tasks page with tasks grouped by service in card view
- Arrow-key navigation for radio card groups
- Series labels in the Prometheus query result table
- Per-stack CPU and memory usage charts on the node detail page with drill-down to individual services
- HTTP Range Request pagination on all list API endpoints (`Range: items 0-49` returns `206 Partial Content` with `Content-Range`)
- Infinite scroll on all resource list pages — items load automatically as you scroll down

### Deprecated
- `CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES` — use `CETACEAN_TRUSTED_PROXIES` instead; will be removed in v1

### Security
- Service tasks, service logs, task logs, and node tasks endpoints now enforce ACL read checks — previously accessible to any authenticated user regardless of grants
- Topology endpoints now filter services and nodes by per-resource ACL grants instead of showing the entire cluster to any authenticated user
- History endpoint now filters events by per-resource ACL read permission instead of showing all resource changes
- SSE event replay on reconnect now applies ACL filtering (previously replayed events bypassed ACL)
- Policy validation rejects malformed glob patterns at load time instead of silently creating dead grants
- Monitoring status, Prometheus label names, and label values endpoints moved from unauthenticated `/-/` prefix to authenticated `/metrics/` — previously exposed cluster node count and Prometheus label data without authentication
- Task-to-service-to-stack ACL inheritance chain now resolves correctly (previously stopped at service level)

### Changed
- `X-Request-ID` header renamed to `Request-Id` per RFC 6648 (deprecation of `X-` prefix)

### Fixed
- Layout shift in the header when the live connection timer changes width
- Pressing Escape in a combobox dropdown closing the parent editor panel
- Node metrics table columns, gauges, and chart tooltips when the Prometheus instance label uses a hostname instead of an IP
- Sizing recommendations comparing aggregate usage across all tasks to per-task limits, producing incorrect percentages and suggestions for multi-replica services

## [0.10.0] - 2026-03-31

### Added
- Self-metrics endpoint (`/-/metrics`) documented in monitoring guide with full metric reference
- `CETACEAN_SELF_METRICS` toggle to disable the self-metrics endpoint (enabled by default)
- `CETACEAN_RECOMMENDATIONS` toggle to disable the recommendation engine (enabled by default)
- Task state filter on the tasks page — filter by running, failed, or any other state via segmented control
- "Failed Tasks" card on the cluster overview now links directly to failed tasks
- Recommendation cards expand to show why each recommendation matters
- Monitoring status banner shows the actual error when Prometheus is unreachable

### Fixed
- Relative timestamps ("5 minutes ago") never updating while the page stays open
- Chart data briefly corrupted when switching time ranges on metrics panels
- Task state not updating in real time when replicas finish starting, requiring a 5-minute wait for the periodic re-sync
- Prometheus proxy returning raw 404/502 responses instead of structured errors when the configured URL is wrong
- Prometheus query client ignoring HTTP error status codes, producing misleading "parse error" messages
- Base path detection using page URL instead of `<base>` tag, causing broken API requests when navigating in the dev server
- API requests hanging indefinitely when Prometheus or Docker is unresponsive (now timeout after 30 seconds)
- Empty state messages not announced by screen readers

## [0.9.1] - 2026-03-30

### Fixed
- Assets not loading when deployed under a base path

## [0.9.0] - 2026-03-30

### Added
- Sub-path deployment: serve Cetacean under a configurable URL prefix (e.g., `/cetacean/`) via `CETACEAN_BASE_PATH` environment variable, `--base-path` flag, or `base_path` TOML config
- Recommendation engine: `/recommendations` page with cluster health checks across resource sizing, config hygiene, operational health, and cluster topology — with one-click fixes for auto-fixable items
- Integration detection: Traefik, Shepherd, Swarm Cronjob, and Diun labels shown as structured panels on service detail pages, with inline editing

## [0.8.2] - 2026-03-27

### Fixed
- Release Docker image running as unprivileged user, preventing Docker socket access
- Release Docker image healthcheck not detecting Docker connectivity issues
- Minimal scratch base image for release builds (was alpine)

## [0.8.1] - 2026-03-27

### Fixed
- Resource pages showing empty states instead of errors when Docker socket is unreachable
- Container running as unprivileged user unable to access Docker socket
- Watcher reconnect loop spamming API with sync events on persistent Docker failures

## [0.8.0] - 2026-03-26

### Added
- Label editing for configs and secrets
- Config and secret creation from the dashboard
- Plugin management: browsable list and detail pages, install/upgrade with privilege review, enable/disable/remove/configure
- Swarm configuration editors: per-section editing for raft, CA, orchestration, and task defaults, with token rotation and unlock key retrieval
- Structured error codes across all API error responses with browsable error reference at `/api/errors`
- Toast notifications for mutation errors with contextual suggestions from the error code registry
- Config, secret, network, and volume removal from detail pages with confirmation dialogs
- Node role change (promote/demote) with radio card selector and quorum impact warnings
- Node removal with type-to-confirm hostname dialog (only available for down nodes)
- Stack removal with type-to-confirm dialog (removes all services, configs, secrets, and networks)
- Service mounts editor with card-based UI supporting all Docker mount types (bind, volume, tmpfs, npipe, cluster, image)
- Service config, secret, and network attachment editors (add/remove references with searchable dropdowns)
- Container configuration editors: command, runtime options, capabilities, extra hosts, DNS settings
- Browsable pages for service sub-resource endpoints (env, placement, ports, policies, log driver, mounts, attachments)
- Docker documentation help links on all service editor fields
- Deployment diff and recent activity shown side by side on wide screens

### Improved
- Detail pages use SSE event payloads for instant sub-resource updates instead of refetching (10 → 2 HTTP requests per event on service pages)
- Healthcheck timeline is keyboard-accessible and scrollable on small screens
- Combobox dropdowns show resource IDs as subtitles for disambiguation
- Editor sections show configured values only, with empty states and edit hints

### Fixed
- Config and secret editors auto-filling incorrect target paths for stack-namespaced resources
- Escape key in combobox dropdowns closing the parent editor instead of just the dropdown

## [0.7.0] - 2026-03-21

### Added
- Operations level setting to restrict write operations by danger tier
- Scale, update image, rollback, and restart actions on the service detail page
- Drain, activate, and pause node availability controls on the node detail page
- Force Remove button on task detail page to kill a task and its backing container
- Last deployment diff on service detail page showing what changed in the most recent service update
- Inline environment variable editor on service detail page (add, edit, remove variables)
- Inline node label editor on node detail page (add, edit, remove labels)
- Inline service label editor on service detail page
- Service resource limits editor on service detail page (CPU and memory limits/reservations)
- Service mode switching (replicated/global) and endpoint mode editing (VIP/DNS-RR)
- Write actions accessible via the command palette (Cmd+K)
- Cluster capacity API endpoint for resource slider bounds
- PromQL metrics query console with autocompletion for metric names, functions, and label values
- Segment-prefix fuzzy matching in global search and PromQL autocompletion
- Copy-to-clipboard buttons on container configuration and healthcheck values
- Docker Swarm template expressions (e.g. `{{.Node.Hostname}}`) rendered as human-readable badges
- Resizable log viewer (drag bottom edge, double-click to reset)
- Fullscreen log viewer via browser Fullscreen API
- Service placement, ports, update/rollback policy, and log driver read and write endpoints
- Healthcheck editor on service detail page (view and edit healthcheck configuration)
- Interactive healthcheck timeline on task detail page
- Delete service button on service detail page
- Config file auto-discovery from standard locations
- Mobile-responsive layout (navigation, topology, log viewer, search palette, charts)
- Replica doughnut chart on service detail page

### Improved
- All mutation forms use polished shadcn/ui components (popovers, confirmation dialogs, styled dropdowns)
- Resource limits editor shows sliders with cluster-aware bounds and accepts memory in megabytes
- Scale replica input has increment/decrement buttons
- Destructive actions use styled confirmation dialogs instead of browser-native confirms
- Native title attributes replaced with proper tooltips across service detail page
- Activity feed on detail pages no longer shows redundant resource type badge
- Activity feed uses stack-prefix rendering for resource names

### Fixed
- Log viewer appearing empty on service and task detail pages until user interaction
- Node metrics showing when node-exporter targets are not available
- Duplicate labels section on service detail page
- Long values overflowing in last deployment section
- Command palette showing write actions above the configured operations level

## [0.6.0] - 2026-03-16

### Added
- Pluggable authentication with five providers: anonymous, OIDC, Tailscale, mTLS client certificates, and trusted proxy headers
- Identity display in the navigation bar when authentication is enabled
- `_FILE` suffix support for secret environment variables (for Docker Swarm secrets)

### Security
- Session cookies use `__Host-` prefix for browser-enforced security constraints

## [0.5.0] - 2026-03-16

### Added
- Tasks list page (the "Failed Tasks" card on the homepage no longer 404s)
- Click-to-isolate on chart legend items in stack drill-down charts
- Keyboard shortcut tooltips on navigation links (hover to discover shortcuts)

### Changed
- Lazy-load all pages and split heavy vendor libraries (Chart.js, topology) into separate chunks — initial load reduced from 2.3 MB to ~360 KB

### Fixed
- Log viewer "has more" indicator inaccurate when filtering by stream (stdout/stderr)
- List page item counts not updating on real-time add/remove events
- Race condition when multiple metrics streams connect simultaneously
- Stale fetch requests not cancelled when navigating away from detail/search pages
- Ghost stacks appearing after all services removed but configs/secrets remain
- Metrics charts not reconnecting SSE stream after tab becomes visible again

## [0.4.0] - 2026-03-16

### Added
- Live-updating charts via SSE streaming (no more manual refresh for recent time ranges)
- Range picker with quick presets and custom date-time selection

### Changed
- Replace auto-refresh toggle with streaming play/pause control

### Fixed
- Click-to-isolate and double-click drill-down racing on chart interactions
- Doughnut chart sizing on homepage

## [0.3.0] - 2026-03-16

### Added
- TOML config file and CLI flags as alternative to environment variables
- `healthcheck` subcommand for container health checks

### Fixed
- Simplify disk usage chart to single ring (two-ring layout was confusing; reclaimable info moved to tooltip)

## [0.2.0] - 2026-03-15

### Added
- Migrate from uPlot to Chart.js for all charting
- CVD-safe color palette with theme integration
- Interactive charts: linked crosshairs, click-to-isolate, brush-to-zoom
- Custom date-time range picker with quick presets
- Stacked area toggle on time series charts
- Stack-based drill-down on cluster overview
- Resource allocation bar chart on service detail
- Mock chart data in dev mode when Prometheus is unavailable

### Fixed
- Search input no longer loses focus on refetch
- List pages no longer flash loading skeleton on search/sort

## [0.1.11] - 2026-03-14

### Fixed
- Log fetches for multi-replica services reduced from ~10s to ~2s (idle timeout on stream parser)

## [0.1.10] - 2026-03-14

### Fixed
- Frontend test failures after monitoring status hook was added

## [0.1.9] - 2026-03-14

### Fixed
- Stale browser-cached responses (added Cache-Control headers)
- Data race in cross-reference lookups under concurrent reads
- Dockerfile healthcheck path
- Docker Compose external network name for monitoring stack

### Security
- Bump undici

## [0.1.8] - 2026-03-14

### Added
- Per-task CPU/memory sparklines on service, node, and task pages
- Metrics panels on cluster overview, node list, and service list
- Leader badge, availability, and address columns on node table
- Ports column on service table

### Fixed
- Resource gauges crash when no resource limit is set
- Page titles all showed "frontend" instead of the resource name
- Tables clip on small viewports

## [0.1.7] - 2026-03-13

### Fixed
- Double borders on deploy config detail panels
- Ghost stacks appearing from orphaned volumes

## [0.1.6] - 2026-03-13

### Fixed
- Duplicate edges in logical topology view
- Overlapping nodes in physical topology view
- Stack health incorrectly counting historical task failures

## [0.1.5] - 2026-03-13

### Fixed
- Node metrics showing wrong values in overlay network deployments (now resolves by hostname instead of IP)

## [0.1.4] - 2026-03-13

### Fixed
- Service replica counts inflated by shutdown tasks
- Log viewer making unnecessary polling requests
- Spurious task change events in activity feed
- Running tasks not sorting first in task lists
- Task detail service link not clickable

## [0.1.3] - 2026-03-13

### Fixed
- Dockerfile naming and base image updates for release builds

## [0.1.2] - 2026-03-13

### Fixed
- SSE test data race under `-race` flag
- CI compatibility with Go 1.26 and latest GitHub Actions

### Security
- Bump hono to 4.12.7 (prototype pollution CVE)

## [0.1.1] - 2026-03-13

### Fixed
- CI and dependency fixes

## [0.1.0] - 2026-03-12

### Added
- Per-resource SSE streaming on all list and detail endpoints
- OpenAPI 3.1 spec with Scalar API playground at `/api`
- JSON-LD metadata and RFC 9457 error responses
- Content negotiation via `Accept` header or `.json`/`.html` extension
- ETag conditional caching with 304 Not Modified
- Global cross-resource search with `Cmd+K` command palette
- Network topology view (logical and physical)
- Stack detail pages with member resources
- Log viewer with live streaming, regex search, JSON formatting
- Monitoring auto-detection (Prometheus, cAdvisor, node-exporter)
- Node resource gauges and service/stack metrics panels
- Disk snapshot persistence for instant dashboard on restart
- Expression-based filtering on all list endpoints
- Virtual scrolling for large tables
- Activity feed with recent resource changes
- Multi-platform Docker images (amd64, arm64) with SBOM and provenance

### Security
- Secret values never exposed in API responses
- Prometheus proxy restricted to query endpoints
- Connection limits: 256 SSE clients, 128 concurrent log streams
