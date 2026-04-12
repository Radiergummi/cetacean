---
title: Dashboard Guide
description: Keyboard shortcuts, command palette, chart interactions, log viewer, and topology views.
category: guide
tags: [dashboard, ui, keyboard-shortcuts, search, charts, logs]
---

# Dashboard Guide

Cetacean's UI updates in real time via SSE—when a service scales or a node goes down, the dashboard reflects it
without refreshing. The connection indicator in the nav bar shows stream health; if it drops, Cetacean reconnects
automatically.

## Keyboard Shortcuts

Press `?` to see all shortcuts. The highlights:

| Shortcut                 | Action                                    |
|--------------------------|-------------------------------------------|
| `⌘ K` / `Ctrl K`         | Command palette (search + actions)        |
| `g` then `s`/`n`/`k`/... | Navigate to services, nodes, stacks, etc. |
| `j`/`k` or `↓`/`↑`       | Move through table rows                   |
| `Enter`                  | Open selected resource                    |

The `g` shortcuts are chords: Press `g`, release, then the second key.

## Command Palette

`⌘ K` opens the command palette. Type to search across all resource types, or type an action name (`scale`,
`restart`, `drain`, `rollback`) to trigger write operations with guided steps and confirmation. Actions respect
[operations level](configuration.md#operations-level) and [authorization](authorization.md) permissions.

## Filtering

List pages support search and [expr-lang](https://expr-lang.org/) filter expressions via the API's `?filter=` parameter:

```js
role == "manager" && state == "ready"       # nodes
name contains "web" && mode == "replicated" # services
state == "failed" || error != ""            # tasks
```

See the [API reference](api.md) for available filter fields per resource type.

## Charts

Charts appear on the cluster overview, node, service, task, and stack detail pages. They 
require [monitoring](monitoring.md).

- **Click to isolate** a series by clicking its name or line, and everything else dims. Click again to restore.
- **Brush to zoom** by dragging horizontally. The URL updates so you can share the time window.
- **Linked crosshairs** synchronize across all charts in the same panel: hover on one to see values on all siblings.
- **Stacked area toggle** switches between line and stacked area views in the chart header.
- **Stack drill-down** on the cluster overview: double-click a stack to see its individual services.

## Log Viewer

The log viewer on service and task detail pages supports live tailing, time range selection (presets or custom), stream
and level filtering, substring/regex search with match navigation, JSON pretty-printing, and log download.

## Atom Feeds

Every resource list and detail page shows a feed icon in the page header. Click it to open the Atom feed for that page
in a new tab, or copy the URL to subscribe in a feed reader. Feeds include history, search results, and
recommendations pages. See the [API reference](api.md#atom-feeds) for the full list of supported endpoints and
pagination details.

## Topology

The topology page offers two views: **logical** (services grouped by stack, connected by networks) and **physical**
(tasks grouped by node). Both are interactive—click to navigate to detail pages.
