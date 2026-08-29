# Changelog

All notable changes to Cetacean will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Fixed
- Live pages, charts, and the connection indicator no longer stop updating for good when the server is momentarily at its connection limit. Resource streams and metrics charts now re-establish themselves after a rejected connection instead of staying silent until the page is reloaded, and reload what they missed while they were down rather than showing frozen data behind a connected indicator; metrics charts additionally recover from ordinary network interruptions, which previously killed them outright
- Live log tail no longer switches itself off without explanation when its connection drops. It now reconnects on its own with a growing delay between attempts, and picks up where it left off — the lines produced while it was disconnected are filled in when it returns, without repeating what was already shown (a very long disconnection may still leave a gap) — and shows what it is doing while it retries. If the server is at its log-streaming connection limit, the viewer says so and counts down to the next attempt instead of showing a cryptic error, and waits as long as the server asks. After several failed attempts it stops and offers to resume.
- Live log tail no longer sits empty behind a pulsing "Live" badge. Asking for a time range as a duration, at whole-second precision, or in a non-UTC time zone — and simply having a computer clock running ahead of the cluster — each silently discarded every line the stream produced, for as long as the viewer stayed open
- Multi-line log output — stack traces, pretty-printed JSON — is no longer repeated in full every time the live tail reconnects, and is now attributed to the task that produced it, so filtering by task keeps it
- Log lines from services running more than one replica are no longer dropped or shown twice around a reconnect. Docker hands them over interleaved, and the viewer resumed from the last line to arrive rather than the newest one
- A live tail whose connection fails the instant it opens now gives up and offers to resume, instead of reconnecting once a second for as long as the page stays open
- Live log tail now sends you to the login page when your session expires, as the rest of the dashboard does
- Charts plotting a single value — node disk and network, task CPU — no longer drop to zero on every live update
- Live metrics charts keep streaming after you switch to another browser tab and back
- Resources created while a list page is open now appear in the list, instead of the page growing by a row that repeats the entry above it and the new resource showing up only after a reload
- AI agents reading service logs over MCP now advance through history correctly. Passing the returned cursor back fetched the same newest lines every time instead of the next page, so an agent paging through a log could loop on the same output
- AI agents reading service logs over MCP now receive at most the number of lines they asked for. A read that resumed from a cursor could return up to ten times as many

## [0.12.0] - 2026-08-28

### Security
- Updated Go, dashboard, and marketing-site dependencies to clear every outstanding security advisory, including a critical advisory in the OpenAPI parser (GHSA-r277-6w6q-xmqw) and high-severity advisories in `react-router`, `undici`, `go-git`, `go-billy`, `grpc`, `postcss`, `js-yaml`, `nanoid`, and `svgo`
- Rejected OIDC bearer tokens no longer reach the server log. An invalid or expired token's contents could previously be written out as part of the authentication-failure message
- A `Request-Id` header forwarded by a proxy is only echoed back when it consists of letters, digits, dashes, underscores, and dots; anything else is replaced with a freshly generated ID

### Changed
- Updated Go and dashboard dependencies to their latest releases, including major upgrades to the TypeScript compiler and the test toolchain. No change to how the dashboard looks or behaves.

### Added
- Embedded Model Context Protocol (MCP) server (opt-in via `CETACEAN_MCP=true`) — exposes cluster state to AI agents over streamable HTTP at `/mcp`, with twelve resources (services, nodes, tasks, stacks, configs, secrets, networks, volumes, plus cluster, recommendations, and history) and twenty-three tools spanning read, operational, configuration, and impactful tiers
- Every MCP tool and resource now advertises a human-readable title, a fuller description of what it does and when to use it, and all four behavioural hints (`readOnlyHint`, `destructiveHint`, `idempotentHint`, `openWorldHint`) so MCP clients can render confirmation UI accurately
- OAuth 2.1 authorization server for MCP clients implementing the MCP 2025-11-25 authorization profile — Dynamic Client Registration (RFC 7591), Client ID Metadata Documents, RFC 8707 resource indicators, PKCE-only flows, and refresh token theft detection
- MCP authorization-server metadata is now also served at the OpenID Connect discovery path (`/.well-known/openid-configuration`), so clients that locate the authorization server via OIDC Discovery 1.0 (per the 2025-11-25 spec) find the same endpoints as via RFC 8414
- MCP endpoint now rejects requests carrying a disallowed `Origin` header with HTTP 403 — a DNS-rebinding defense required by the 2025-11-25 Streamable HTTP transport. The allowlist is `CETACEAN_CORS_ORIGINS` (`*` allows any); non-browser clients, which send no `Origin`, are unaffected
- `CETACEAN_MCP_ISSUER` (and `[mcp].issuer`) for setting the canonical OAuth issuer URL when Cetacean runs behind a reverse proxy
- MCP tools now return structured, machine-readable results (`structuredContent`) alongside the text form, so AI agents can parse tool output without scraping JSON out of a text blob. The `search`, `get_logs`, and `remove_*` tools additionally advertise an output schema, which the server validates results against
- MCP server now advertises usage instructions and a description when a client connects, so AI agents understand the read-mostly model, that writes are gated by operations tier and ACL, and to resolve a resource via `search` before acting on it
- MCP tools and resources now carry icons (per the 2025-11-25 spec), so MCP clients can display a glyph next to each one. Tool icons are grouped by what the tool does (read, search, scale, edit, node, remove) and resource icons reflect the resource type (node, service, stack, config, secret, …); both are served from Cetacean itself
- Open-source licenses page (linked from the footer) listing every Go module and frontend dependency bundled into Cetacean, with search and per-ecosystem filtering. The underlying software bill of materials is available as CycloneDX at `/-/sbom.cdx.json`.

### Fixed
- Marketing-site dependencies could not be installed or updated at all — an unresolvable Astro peer-dependency conflict made every `npm install` in `website/` fail
- MCP OAuth discovery now works when Cetacean runs under a base path: the authorization-server `issuer`, the protected-resource metadata `authorization_servers`, and the access-token `iss` claim now include `CETACEAN_BASE_PATH`, matching where the `.well-known` documents are actually served. Previously a client that derived the metadata URL from the issuer got a 404 whenever a base path was configured (default deployments without a base path were unaffected)
- Service detail page no longer crashes after a task update arrives over the live stream (a stale page left open would occasionally throw "undefined is not an object")
- Unexpected errors now show a clearer recovery screen with reload and try-again actions; technical details are tucked into a collapsible section
- Confirmation dialogs (restart service, rollback, drain node, remove resource, rotate unlock key, …) now dismiss themselves after the action is confirmed instead of staying open
- Service detail page no longer crashes when a service's task template is absent from the response
- Resource pages no longer crash on fields the Docker Engine leaves out of its responses. The dashboard now expects them to be missing wherever Docker can omit them — nodes that have not reported their hostname, platform, resources, or address yet; tmpfs mounts, which carry no source; published ports and network gateways that are unset; and services with no image
- Swarm CA and Raft panels no longer show `undefined` for settings Docker omits when they are left at their default
- Plugin type now displays as `docker.volumedriver/1.0` rather than `[object Object]`
- Metrics charts and the metrics console no longer break on Prometheus responses that carry no samples, and the console now renders scalar and string query results (e.g. `time()`) instead of reporting "No results"
- node-exporter is detected by the presence of its metrics rather than by a Prometheus job literally named `node-exporter`. A deployment whose scrape config names that job anything else reported "node-exporter not detected" and hid the node CPU, memory and disk columns while the exporter was running and healthy. A target that is configured but unreachable now reports as undetected too, rather than reporting as present and then rendering empty panels
- Fixed latent crashes in the disk-usage chart tooltip, the log-driver options editor, and keyboard navigation on empty radio-card groups
- MCP refresh-token store no longer accumulates rotation history indefinitely; theft detection keeps a bounded recent window and grants are cleaned up when tokens expire
- MCP authorization codes are swept on each new issue so abandoned consent flows can no longer fill memory
- MCP `search` tool now rejects empty queries instead of returning every cached resource
- `CETACEAN_MCP_AUTH_BYPASS` now takes effect: when the active Cetacean auth mode is listed (typically `cert`), MCP clients can reach `/mcp` using upstream identity (e.g. mTLS) without an OAuth bearer token
- MCP `update_node_availability` and `update_node_role` tools advertise the `destructiveHint` annotation so MCP-aware clients can gate them behind a confirmation prompt
- MCP `restart_service` and `rollback_service` now carry `destructiveHint: true` (rolling restart interrupts tasks; rollback discards the current spec)
- MCP `search` tool no longer advertises a `types` parameter it doesn't honour — the field was always ignored by the handler
- MCP PKCE verifier comparison uses constant-time equality
- MCP `WWW-Authenticate` header now uses RFC 7230 quoted-string escaping instead of Go-syntax quoting (correct for values containing backticks or non-ASCII characters)
- MCP server's `Close` is now safe under concurrent callers
- MCP Dynamic Client Registration rejects unsupported `grant_types` and `response_types` per RFC 7591 §3.2 instead of silently storing them
- MCP consent error page no longer surfaces raw CIMD fetcher errors (DNS, SSRF block reasons, connection failures); operators still get the details in the server log
- MCP `get_logs` tool now enforces the same read ACL as the service log resource
- MCP `remove_config` / `remove_secret` / `remove_network` ACL checks now key on the resource name from cache rather than the Docker ID, matching REST policy semantics
- MCP `remove_task` ACL delegates to the parent service (`service:<name>`) like REST does instead of keying on `task:<id>`
- MCP CIMD fetcher closes the DNS-rebinding TOCTOU window by resolving and validating IPs inside a custom dial that pins the connection to the validated address
- MCP CIMD validates `redirect_uris` and `logo_uri` from the fetched document so a malicious metadata host cannot inject `javascript:` redirects or non-HTTPS logos
- MCP token endpoint enforces RFC 7636 §4.1 PKCE verifier length (43–128 chars) and unreserved alphabet
- MCP refresh-token grants respect an absolute grant-family lifetime — rotating with a long TTL no longer extends the family past the original 30-day window
- MCP refresh-token resource-indicator mismatch no longer burns the grant family on a client typo; validation runs before token consumption
- MCP `tools/list` hides write tools the caller's identity has no grants for, so the catalog matches the surface the call-time ACL would actually allow
- MCP `notifications/resources/list_changed` skips sessions whose identity can't read any resource of the affected type, removing cross-tenant activity timing leaks
- MCP stack and volume detail reads check the cache before evaluating ACL so `denied` and `not found` are indistinguishable from outside
- MCP `update_service_env` / `update_service_labels` / `update_node_labels` now correctly merge against the current spec (string sets, `null` deletes) instead of replacing the whole map
- MCP `update_service_image` rejects empty / whitespace-only image strings
- MCP logs tool uses Docker's idle-cancel parser, matching the REST log handler and returning promptly when Docker leaves the non-follow stream open
- MCP consent page sets `Cache-Control: no-store` so shared caches and back-button replays can't surface another user's CSRF / state / identity
- MCP DCR endpoint caps request bodies at 64 KiB to block trivial DoS via oversized JSON payloads
- MCP JWT verify rejects tokens whose header `alg` is not `HS256` (defense-in-depth)
- All tier-1/2 MCP write tools now advertise `readOnlyHint:false`; idempotent updates carry `idempotentHint:true` so MCP-aware clients can render correct confirmation prompts
- Service state derivation reports `updating` for rollback-started and rollback-paused states, not just forward updates
- PRM, AS-metadata, and DCR responses marshal before writing so an encoding failure cannot emit a partial body followed by a 500 status
- Stack secret redaction is now centralised in the cache invariant; the redundant inline loop in `GetStackDetail` has been removed
- Env and label patch handlers no longer lose concurrent writes — the merge runs against a fresh Docker inspect inside the writer, so other writers' updates to unrelated keys are preserved
- MCP OAuth DCR rate-limit map is swept on every request; previously every distinct source IP left a bucket behind for the lifetime of the process
- MCP OAuth authorization-server metadata advertises `revocation_endpoint_auth_methods_supported` per RFC 7009
- MCP CIMD fetcher reuses its HTTP client (with the SSRF-aware transport) across fetches instead of rebuilding the transport per request

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
