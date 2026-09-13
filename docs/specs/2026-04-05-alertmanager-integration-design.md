# Alertmanager Integration Design

## Overview

Add Alertmanager integration to Cetacean: a read-only alert viewer with silence management and cross-referencing to existing Swarm resources. Alerts are pushed to Cetacean via Alertmanager webhooks and streamed to browsers via SSE. Silences are managed through Cetacean's UI, proxied to Alertmanager, and gated by existing ACL write permissions. Additionally, ship a predefined Prometheus rules file with recording rules (pre-computing repeated Cetacean queries) and alerting rules (sensible Swarm defaults).

## Configuration & Discovery

- New env var `CETACEAN_ALERTMANAGER_URL` (optional explicit URL).
- New TOML section `[alertmanager]` with `url` field.
- On startup, if no explicit URL is set and Prometheus is configured, query Prometheus's `/api/v1/alertmanagers` to discover active Alertmanager instances. Use the first active one.
- If neither discovery nor explicit URL yields a result, alerting features are disabled (warn and move on).
- New env var `CETACEAN_ALERTMANAGER_WEBHOOK_SECRET` for authenticating incoming webhook payloads.
- Extend `/metrics/status` response with `alertmanagerConfigured`, `alertmanagerReachable`, and `alertmanagerURL` fields.

## Backend: Alertmanager Client

New package `internal/api/alertmanager/` (or `internal/alertmanager/`), following the same pattern as `internal/api/prometheus/`.

**Client (`client.go`):**
- Thin HTTP client wrapping Alertmanager's v2 API.
- Methods: `GetAlerts()`, `GetAlertGroups()`, `GetSilences()`, `CreateSilence()`, `ExpireSilence()`, `GetStatus()`.
- 10-second timeout, structured error handling.

## Backend: Cache Integration

- New `alerts` field in the cache storing current alert state (firing/pending/resolved).
- Cross-referenced to services/nodes/stacks by matching labels:
  - `container_label_com_docker_swarm_service_name` -> service
  - `container_label_com_docker_stack_namespace` -> stack
  - `instance` / `node` -> node (resolved via hostname/IP mapping in cache)
- Lookup methods: `AlertsForService(id)`, `AlertsForNode(id)`, `AlertsForStack(name)`, `AlertCount()`.
- Alerts that don't match any known Swarm resource are stored and displayed as unlinked.
- Silences are NOT cached — fetched on-demand from Alertmanager when requested.

## Backend: Webhook Receiver

- `POST /-/webhooks/alertmanager` receives Alertmanager webhook payloads.
- Updates cache, fires `OnChangeFunc` -> SSE broadcaster.
- Event type: `alert` with `status: firing|resolved` in payload.
- Exempt from auth middleware (under `/-/`) but validated by shared secret (`CETACEAN_ALERTMANAGER_WEBHOOK_SECRET`) via HTTP basic auth or custom header.
- On startup, full fetch of all current alerts from Alertmanager API to populate cache. Webhooks handle ongoing state changes.

## API Endpoints

All alerting endpoints are nested under `/alerting/`.

### Read endpoints (level 0)

- `GET /alerting/alerts` — List all alerts. Supports `?search=`, `?filter=`, `?sort=`, `?dir=` like other list endpoints. Returns `CollectionResponse` with JSON-LD metadata. Content-negotiated: JSON, SSE, HTML (SPA), Atom.
- `GET /alerting/alerts/{fingerprint}` — Alert detail by Alertmanager fingerprint. JSON-LD wrapped response with alert + cross-referenced resource links.
- `GET /alerting/alerts/groups` — Alert groups as Alertmanager organizes them (by routing tree).

### Silence endpoints (level 1, ACL-gated)

- `GET /alerting/silences` — List silences, fetched on-demand from Alertmanager. Content-negotiated.
- `POST /alerting/silences` — Create a silence. Body: matchers, start/end time, author, comment. Requires `write` ACL on matched resources.
- `DELETE /alerting/silences/{id}` — Expire a silence. Requires `write` ACL on matched resources.

### Cross-reference extensions

- Service, node, and stack detail responses gain an `alerts` field listing firing/pending alerts for that resource.
- `GET /search` includes alerts in results when alert names or labels match the query.

### Operations level

- Read endpoints at level 0 (read-only).
- Silence create/delete at level 1 (operational).

## SSE & Real-time Updates

- `GET /alerting/alerts` with `Accept: text/event-stream` opens an SSE connection scoped to alert changes.
- `GET /alerting/alerts/{fingerprint}` with SSE streams changes for a single alert.
- Events use the same shape as other resources: `event: alert` with the full alert object in the `resource` field for optimistic client-side updates.
- No SSE for silences — fetched on-demand, refetched after create/expire actions.
- Service/node/stack detail SSE streams include alert count updates when alert changes cross-reference that resource.

## Frontend

### New pages

- `/alerting/alerts` — Alert list. `DataTable` with columns: alert name, state (firing/pending/resolved), severity, affected resource (clickable link), started at, duration. Filterable by state and severity. SSE-driven updates via `useSwarmResource` pattern.
- `/alerting/alerts/{fingerprint}` — Alert detail. Labels, annotations, generatorURL (link to Prometheus rule), linked resource, state history. `useDetailResource` pattern.
- `/alerting/silences` — Silence list. Active/pending/expired silences with matchers, author, comment, time range. On-demand fetch (no SSE), manual refresh button.
- Silence creation form dialog. Matchers pre-filled when creating from alert or resource context ("Silence this alert" on alert detail, "Silence alerts" on service detail).

### Alert badges on existing pages

- Service, node, and stack detail pages show a badge next to the page header when there are firing alerts (e.g. "2 firing"). Clicking navigates to `/alerting/alerts?filter=...` scoped to that resource.
- Service and node list pages gain an optional alert indicator column.

### Nav bar

- New "Alerts" nav item with firing alert count badge (total firing count, hidden when zero). Streamed via SSE.

### Monitoring status

- `MonitoringStatus` component extended with Alertmanager tier: unconfigured, unreachable, healthy. Same progressive detection pattern as Prometheus/cAdvisor/node-exporter.

## Predefined Prometheus Rules File

Shipped as `rules/cetacean.rules.yml` in the repo. Users copy it into their Prometheus `rule_files` directory.

### Recording rules (pre-compute repeated Cetacean queries)

- `cetacean:service_cpu:rate5m` — CPU rate by service name
- `cetacean:service_memory:bytes` — Memory usage by service name
- `cetacean:node_cpu:percent` — Node CPU utilization percentage
- `cetacean:node_memory:percent` — Node memory utilization percentage
- `cetacean:node_disk:percent` — Node filesystem usage percentage

### Alerting rules (sensible Swarm defaults)

- `ServiceDown` — Service has 0 running tasks
- `ServiceRestartLoop` — High restart rate over 15 minutes
- `ServiceDegraded` — Running tasks < desired replicas for sustained period
- `NodeDiskPressure` — Filesystem usage > 85%
- `NodeMemoryPressure` — Memory usage > 90%
- `NodeDown` — Node unreachable / not reporting
- `NodeDrained` — Node availability set to drain (informational)

Exact queries finalized during implementation by auditing existing PromQL in `useServiceMetrics.ts`, `useNodeMetrics.ts`, and the recommendations engine.

### Frontend integration

- On startup, Cetacean probes whether its recording rules are available (quick test query).
- If available, metrics queries use pre-computed series instead of raw queries.
- Transparent fallback to raw queries if recording rules are not installed.

## ACL Integration

- Alert reads follow the existing ACL read model. With ACL active, users only see alerts for resources they have `read` permission on. Unlinked alerts (no Swarm resource match) are visible to all authenticated users.
- Silence create/expire requires `write` permission on the matched resource(s). E.g., silencing an alert on service `myapp` requires write access to the `myapp` service.
- New ACL resource type: `alert` for controlling access to unlinked alerts (optional, default: visible to all authenticated users).
