# Saved PromQL panels and pinning — design

For #510. Status: proposal.

## Problem

A useful console query can only be bookmarked. People want it on the overview, or on every
service's page, and want the services and nodes they watch at the top of the overview.

## Decision: who owns a definition decides where it lives

Cetacean is read-mostly and not a source of record for anything but what the swarm says.
A panel store written from the UI would make it one, with backups, ACLs on the panels
themselves, and a migration story. So nothing new is stored server-side:

| What | Owner | Lives in |
|---|---|---|
| A panel for every service, stack, or the overview | the operator | `prometheus.panels` / `prometheus.panels_file` |
| A panel for one service | the service's author | that service's labels |
| Pinned services and nodes | the person | the browser |

## Decision: panels run through the PromQL proxy, and only for those who may query

A panel is arbitrary PromQL, and PromQL selects whatever series it names. No check on the
panel's definition can bound what it reads, so a panel is executed exactly like a console
query: by the browser, through `GET /metrics`, which `requireReadEverything` already limits to
callers with read on a bare `"*"`. There is no server-side panel execution path.

- **Scoped callers** (`queryable: false` on `GET /metrics/status`) see no panels, the same way
  they see no usage charts and no console today. The `panels` field is omitted for them, so no
  operator-written PromQL reaches a caller who cannot run it.
- **Label panels grant nothing.** Writing one takes `OpUpdateService` (tier 2) and write on that
  service, but it runs only for viewers who could already run any query, so a panel that reads
  another service's series discloses nothing they couldn't read in the console. The remaining
  risks are a misleading title and an expensive query. Each panel therefore shows where it came
  from (config, or the label on service *X*) and its query on demand, and runs under the
  proxy's existing 30 s upstream timeout.
- **MCP gets no panels.** `get_metrics` owns its PromQL so that no caller supplies a selector
  (`internal/mcp/metrics.go`); a panel is a caller-supplied selector by another route.

### No panels for scoped users

Scoped users could get panels if Cetacean rewrote every vector selector in the query to add a
regex matcher on the caller's readable services, the approach of prom-label-proxy. That needs a
PromQL parser, which means depending on `prometheus/prometheus`, and makes Cetacean responsible
for a rewrite that must hold across every PromQL construct. It also needs one label naming the
service on every series, which cAdvisor's have and an application's own metrics may not. This
design ships panels for read-everything callers only; it is revisited if scoped deployments ask
for them, and the same rewrite would then also open the console to scoped users.

## Definitions

### Operator panels

Panels are a list of tables, which no environment variable can hold. They follow the
`acl.policy` / `acl.policy_file` precedent: `prometheus.panels` (env `CETACEAN_PROMETHEUS_PANELS`)
takes the panels inline as JSON, YAML or TOML, and `prometheus.panels_file`
(`CETACEAN_PROMETHEUS_PANELS_FILE`) a path to the same, watched and reloaded on change. Neither
gets a flag, so `prometheus.*` joins the "no command-line flags" sentence in
`docs/configuration.mdx`, which stays the one place they're documented.

```toml
[[panels]]
id = "http-errors"
title = "5xx rate"
scope = "service"             # overview | stack | service
query = 'sum(rate(http_requests_total{service="$service",code=~"5.."}[5m]))'
unit = "req/s"                # optional, picks the axis formatter
```

### Variables

`$service` (scopes `service`) and `$stack` (scopes `stack` and `service`) are the only
variables. A variable in a panel whose scope does not provide it is a config error at load,
and a label panel using `$stack` on a service outside any stack is skipped.

There is no `$node` and no `node` scope. node-exporter names a host by `instance`, which Cetacean
matches only by regex on the node's address (`instanceSelector` in `internal/mcp/metrics.go`),
so a node variable would substitute a regex fragment rather than a label value. Node panels are
out of scope until that mapping is something a panel author can rely on.

### Substitution

A variable may appear only as the entire quoted value of a label matcher —
`label="$service"`, `label!="$service"`, `label=~"$service"`, `label!~"$service"` — and
anywhere else is a config error. That makes the matcher's operator decide the escaping without
a PromQL parser:

- `=` and `!=`: escaped for a PromQL string (`escapePromQLValue`).
- `=~` and `!~`: regex-quoted, then string-escaped (`promQLRegexValue`). PromQL anchors the
  regex, so `api.v2` then matches only `api.v2`.

Both escapers live in `internal/mcp/metrics.go` today. They move to `internal/prom`, beside
`SwarmServiceLabel`, so the panel code and `get_metrics` share them.

Panels are exposed as `panels` on `GET /cluster`, `/stacks/{name}` and `/services/{id}`, already
substituted. Only REST renders them, so resolution lives in `internal/api`, not `internal/cluster`.

### Label panels

`cetacean.panel.<id>=<promql>`, with `.title` and `.unit`, read like `cetacean.link.<id>`
(`internal/cluster/links.go`): sorted by id, merged into the service's `panels`, and onto
the stack page as the union of its services', each carrying the `services` that declare it
as stack links do. Label panels may use `$service` and `$stack`, under the same substitution
rule.

### Promote from the console

The console gains **Add as panel**. It cannot write config or labels, so it opens a dialog
with the query generalised (a literal service or stack name replaced by its variable, when it
appears as a label matcher value) and two snippets to copy: the panels block and the label lines.
For a single-service panel it also links to the service's label editor, which can write the
label directly at operations level 2 and up — the only path that saves without leaving Cetacean.

## Pinning

A pin toggle on service and node detail pages and list rows. Pins sit in a **Pinned** row at
the top of the overview with status and the usage sparkline the list already has. Stored in
`localStorage` via `lib/storage.ts`, so they are personal and per browser; losing them costs
little. They are not exposed by the API.

## Rejected

- **A server-side store under `storage.data_dir`.** Writable from the UI, but makes Cetacean a
  source of record and needs its own ACLs. Revisit only if #487's store lands and people ask
  for UI-authored shared panels.
- **Server-side panel execution.** It would return whatever the query selects to whoever views
  the page, turning every label author into a reader of every series.
- **`[[metrics.panels]]` in the main config file.** It would be the only setting with no
  environment variable.
- **Panels in `localStorage` too.** Panels are the thing people want to share; pins are not.

## Out of scope

Dashboards with layout, alert thresholds on panels (#511), Grafana import, node panels.
