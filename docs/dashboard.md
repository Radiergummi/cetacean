---
title: Dashboard
description: Navigation, keyboard shortcuts, the command palette, list and detail pages, charts, logs, and topology.
category: guide
tags: [dashboard, ui, keyboard-shortcuts, search, charts, logs]
---

# Dashboard

The dashboard updates itself: a scaled service or a downed node appears without a refresh. The connection
indicator in the nav bar shows when the last update arrived, and reconnects on its own if the connection drops.
It also carries a resync button that makes Cetacean re-read the whole cluster from Docker.

## Navigation

The nav bar links every resource type: nodes, stacks, services, tasks, configs, secrets, networks, volumes, plus
the swarm info, topology and metrics pages. On narrow screens they collapse behind a menu button.

To the right of the search box sit the shortcut help button, a [recommendations][recommendations] indicator badged
with the current finding count, the theme toggle (light, dark, system), and, in every
[authentication][authentication] mode except `none`, the identity badge linking to your profile. The footer carries
the running version and commit, the licenses page, and a link to the API playground at `/api`.

## Keyboard shortcuts

Press `?` for the full list. Shortcuts are ignored while focus is in a text input.

| Shortcut         | Action                                       |
|------------------|----------------------------------------------|
| `⌘ K` / `Ctrl K` | Toggle the command palette                   |
| `/`              | Open the command palette                     |
| `?`              | Toggle the shortcut list                     |
| `Esc`            | Close the overlay, or go back                |
| `g` `h`          | Cluster overview                             |
| `g` `n`          | Nodes                                        |
| `g` `k`          | Stacks                                       |
| `g` `s`          | Services                                     |
| `g` `a`          | Tasks                                        |
| `g` `c`          | Configs                                      |
| `g` `x`          | Secrets                                      |
| `g` `w`          | Networks                                     |
| `g` `v`          | Volumes                                      |
| `g` `i`          | Swarm info                                   |
| `g` `t`          | Topology                                     |
| `g` `m`          | Metrics console                              |
| `g` `r`          | Recommendations                              |
| `j` / `↓`        | Next row in a table                          |
| `k` / `↑`        | Previous row                                 |
| `Enter`          | Open the selected row                        |

The `g` shortcuts are chords: press `g`, release, then the second key within one second. Hovering a nav link shows
its chord.

## Command palette

`⌘ K` (`Ctrl K` on Linux and Windows) opens the palette. Type to search names, images and labels across every
resource type. Results are grouped by type in a fixed order, with a state indicator per row, and refresh every two
seconds while the palette is open so a converging service updates in place. Move with the arrow keys and open with
`Enter`.

Typing an action name instead runs that action as a guided sequence, one prompt per argument, with a breadcrumb of
what you have chosen so far:

| Action              | Type                                                    |
|---------------------|---------------------------------------------------------|
| Scale Service       | Pick a service, then a replica count                    |
| Update Image        | Pick a service, then an image reference                 |
| Rollback Service    | Pick a service                                          |
| Restart Service     | Pick a service                                          |
| Drain Node          | Pick a node                                             |
| Pause Node          | Pick a node                                             |
| Activate Node       | Pick a node                                             |
| Promote Node        | Pick a node                                             |
| Demote Node         | Pick a node                                             |
| Force Remove Task   | Pick a task                                             |
| Remove …            | Pick a service, node, stack, config, secret, network or volume |

Destructive actions ask for confirmation before they run. An action your [operations level][operations-level]
or your [grants][authorization] do not allow is refused with a permission message, not a server error.

## List pages

Every resource type has a list page with a search box, sortable columns, and a table or grid toggle. Search and
sort are held in the URL (`?q=`, `?sort=`, `?dir=`), so a filtered list is a shareable link. Your choice of table
or grid is remembered per resource type; narrow screens always use the grid.

Lists load more as you scroll, and stay current as resources come and go.

The API accepts expression filters through `?filter=` that the dashboard's search box does not build. See the
[API guide][filter-fields-by-resource] for the fields available per resource type.

## Detail pages

A detail page shows the resource, its cross-references (the services using a config, the tasks of a service, the
stack a resource belongs to), and its recent change history. Cross-references are links, so you can walk from a
secret to the services mounting it to the nodes their tasks run on.

Where the operations level and your grants allow it, detail pages carry actions: scale, update image, rollback and
restart on a service, plus inline editors for environment variables, resource reservations and limits, placement,
ports, update and rollback policy, and the log driver; availability and labels on a node; force removal on a task.
Actions hidden by permissions are not rendered.

## Charts

Charts appear on the cluster overview and on node, service and task detail pages, and require
[monitoring][monitoring]. Node and service list pages carry sparklines and gauges from the same data.

The panel header holds a `1H` / `6H` / `24H` / `7D` selector, a custom date-time range picker, a refresh button, a
pause control for the live stream, and a line/area toggle. The selected range is stored as `?range=`, and a custom
one as `?from=` and `?to=`.

- Click a series name or line to isolate it. Everything else dims; click again to restore.
- Drag horizontally to zoom into a time window. The URL updates, so the window is shareable.
- Hover one chart to get a crosshair and matching values on every other chart in the same panel.
- Double-click a stack in the per-stack charts on the cluster overview or a node page to drill into its services.

The preset ranges update live as new points arrive. A custom range is a snapshot; use the refresh button to
bring it up to date.

## Metrics console

The metrics page runs PromQL directly against the configured Prometheus, with completion for metric names. A query
returns a result table and a range chart over the same `1H` / `6H` / `24H` / `7D` selector. The query and range are
held in the URL as `?q=` and `?range=`.

## Log viewer

Service and task detail pages carry a log viewer. It can tail live once you turn the tail on and follows the bottom of the output
until you scroll up, which pauses following; the live toggle in the toolbar stops and starts the stream.

- Time range: presets from the last 5 minutes upwards, or a custom since/until pair
- Filters: by level (parsed from the line, including JSON and `log/slog` numeric levels) and by stream
- Search: substring or regular expression, with match navigation and highlighting
- Rendering: JSON payloads pretty-printed, levels colour-barred
- Download: saves the lines currently loaded as a `.log` file

## Topology

The topology page has two views.

- Logical: one card per service, grouped into its stack, with an edge between any two services sharing an overlay network, listing the shared networks.
  Hovering a card dims everything it is not connected to, and the legend maps colours to stacks.
- Physical: one card per cluster node, listing the tasks placed on it.

Both views pan, zoom and let you drag cards, and clicking a service opens its detail page.

## Atom feeds

Every resource list and detail page shows a feed icon in the page header. Click it to open that page's Atom feed,
or copy the URL into a feed reader. The history, search and recommendations pages have feeds too. See the
[API guide][feeds] for the supported endpoints and pagination.

## Licenses

The licenses page, linked from the footer, lists every open-source dependency bundled into Cetacean, both Go
modules and frontend packages. Search by name, or filter by ecosystem and license. Clicking a license badge opens
the full text for that dependency, with its NOTICE file if it ships one. The header links to the complete
attribution document.

[authentication]: authentication
[authorization]: authorization
[feeds]: api#feeds
[filter-fields-by-resource]: api#filter-fields-by-resource
[monitoring]: monitoring
[operations-level]: configuration#operations-level
[recommendations]: recommendations
