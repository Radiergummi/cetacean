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
| A panel for every service, stack, or the overview | the operator | the config file |
| A panel for one service | the service's author | that service's labels |
| Pinned services and nodes | the person | the browser |

### Config file panels

```toml
[[metrics.panels]]
id = "http-errors"
title = "5xx rate"
scope = "service"             # overview | stack | service
query = 'sum(rate(http_requests_total{service="$service",code=~"5.."}[5m]))'
unit = "req/s"                # optional, picks the axis formatter
```

`$service`, `$stack` and `$node` are substituted per page, as label values, escaped for a
PromQL string. A panel whose scope has no matching variable is a config error at startup.
Exposed as `panels` on `GET /cluster`, `/stacks/{name}` and `/services/{id}`, already
substituted, so the API and MCP describe the same panels the dashboard draws.

### Label panels

`cetacean.panel.<id>=<promql>`, with `.title` and `.unit`, read like `cetacean.link.<id>`
(`internal/cluster/links.go`): sorted by id, merged into the service's `panels`, and onto
the stack page as the union of its services'. Label panels may use `$service` and `$stack`.

### Promote from the console

The console gains **Add as panel**. It cannot write config or labels, so it opens a dialog
with the query generalised (a literal service or stack name replaced by its variable, when it
appears as a label matcher) and two snippets to copy: the TOML block and the label lines. For
a single-service panel it also links to the service's label editor, which can write the
label directly at operations level 2 and up — the only path that saves without leaving Cetacean.

### Pinning

A pin toggle on service and node detail pages and list rows. Pins sit in a **Pinned** row at
the top of the overview with status and the usage sparkline the list already has. Stored in
`localStorage` via `lib/storage.ts`, so they are personal and per browser; losing them costs
little. They are not exposed by the API.

## Rejected

- **A server-side store under `storage.data_dir`.** Writable from the UI, but makes Cetacean a
  source of record and needs its own ACLs. Revisit only if #487's store lands and people ask
  for UI-authored shared panels.
- **Panels in `localStorage` too.** Panels are the thing people want to share; pins are not.

## Out of scope

Dashboards with layout, alert thresholds on panels (#511), Grafana import.
