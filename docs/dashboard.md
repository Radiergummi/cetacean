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

The indicator reads **Live**, **Reconnecting**, or **Stale**. Stale means your browser is connected to Cetacean but
Cetacean has lost Docker, so everything on screen is the state it last read, not the cluster as it is now. The
indicator switches to a warning color and shows how long ago that was; the dashboard catches up on its own once
Docker answers.
The same condition is available to alerting—see [Monitoring][monitoring].

## Navigation

The nav bar links every resource type: nodes, stacks, services, tasks, configs, secrets, networks, volumes,
plugins, plus the swarm info, topology, and metrics pages. On narrow screens they collapse behind a menu button.

To the right of the search box sit the shortcut help button, a [recommendations][recommendations] indicator badged
with the current finding count, the theme toggle (light, dark, system), and, in every
[authentication][authentication] mode except `none`, the identity badge linking to your profile. The footer carries
the running version and commit, the licenses page, and a link to the API playground at `/api`.

## Keyboard shortcuts

Press `?` for the full list. Shortcuts are ignored while focus is in a text input.

| Shortcut         | Action                        |
|------------------|-------------------------------|
| `⌘ K` / `Ctrl K` | Toggle the command palette    |
| `/`              | Open the command palette      |
| `?`              | Toggle the shortcut list      |
| `Esc`            | Close the overlay, or go back |
| `g` `h`          | Cluster overview              |
| `g` `n`          | Nodes                         |
| `g` `k`          | Stacks                        |
| `g` `s`          | Services                      |
| `g` `a`          | Tasks                         |
| `g` `c`          | Configs                       |
| `g` `x`          | Secrets                       |
| `g` `w`          | Networks                      |
| `g` `o`          | Published ports               |
| `g` `v`          | Volumes                       |
| `g` `p`          | Plugins                       |
| `g` `i`          | Swarm info                    |
| `g` `t`          | Topology                      |
| `g` `m`          | Metrics console               |
| `g` `r`          | Recommendations               |
| `j` / `↓`        | Next row in a table           |
| `k` / `↑`        | Previous row                  |
| `Enter`          | Open the selected row         |

The `g` shortcuts are chords: press `g`, release, then the second key within one second. Hovering a nav link shows
its chord.

## Command palette

`⌘ K` (`Ctrl K` on Linux and Windows) opens the palette. Type to search names, images, and labels across every
resource type. Results are grouped by type in a fixed order, with a state indicator per row, and refresh every two
seconds while the palette is open so a converging service updates in place. Move with the arrow keys and open with
`Enter`.

Typing an action name instead runs that action as a guided sequence, one prompt per argument, with a breadcrumb of
what you have chosen so far:

| Action            | Type                                                            |
|-------------------|-----------------------------------------------------------------|
| Scale Service     | Pick a service, then a replica count                            |
| Update Image      | Pick a service, then an image reference                         |
| Rollback Service  | Pick a service                                                  |
| Restart Service   | Pick a service                                                  |
| Drain Node        | Pick a node                                                     |
| Pause Node        | Pick a node                                                     |
| Activate Node     | Pick a node                                                     |
| Promote Node      | Pick a node                                                     |
| Demote Node       | Pick a node                                                     |
| Force Remove Task | Pick a task                                                     |
| Remove …          | Pick a service, node, stack, config, secret, network, or volume |

Actions that remove or disrupt something ask for confirmation before they run. An action your [operations level][operations-level]
or your [grants][authorization] don't allow is refused with a permission message, not a server error.

## List pages

Every resource type has a list page with a search box, sortable columns, and a table or grid toggle. Search and
sort are held in the URL (`?q=`, `?sort=`, `?dir=`), so a filtered list is a shareable link. Your choice of table
or grid is remembered per resource type; narrow screens always use the grid.

Lists load more as you scroll, and stay current as resources come and go.

Where your grants allow creating one, the config and secret lists carry a create form: a name, plus the content
pasted in or read from a file. A secret's value can't be read back afterward.

The API accepts expression filters through `?filter=` that the dashboard's search box doesn't build. See the
[API guide][filter-fields-by-resource] for the fields available per resource type.

The published ports page at `/ports` lists every port the services publish, one row per port: the published and
target port, protocol, publish mode, and the service and stack behind it. An ingress port is bound on every node; a
host-mode port shows how many nodes bind it. Search matches the service name. It has no entry in the navigation
bar; open it with `g` `o`, or with the **Published ports** button on the networks list.

## Detail pages

A detail page shows the resource, its cross-references (the services using a config, the tasks of a service, the
stack a resource belongs to), and its recent change history. Cross-references are links, so you can walk from a
secret to the services mounting it to the nodes their tasks run on.

A stack's page opens with **Topology**: the networks its services attach to and the configs, secrets, and volumes
they mount, drawn as a graph above the tables listing the same resources. Every node links to the resource it
stands for, and a service shows its running tasks against the number wanted. A network the stack doesn't own is
drawn dashed; a resource nothing references is dimmed. Hovering or focusing a node dims everything more than one
edge away and names it in the URL.

Where the operations level and your grants allow it, detail pages carry actions: scale, update image, rollback and
restart on a service, plus inline editors for environment variables, resource reservations and limits, placement,
ports, update and rollback policy, and the log driver; availability and labels on a node; force removal on a task.
Actions hidden by permissions aren't rendered.

Configs, secrets, networks, volumes, services, stacks, nodes, and tasks can also be removed from their detail page,
behind a confirmation. A config, secret, network, or volume that a service still uses can't be removed, and its
button says why.

Service and stack pages end with a collapsed **Compose file** section. Expanding it renders the resource as a
compose document, with buttons to copy it or download it as a `.yaml` file. It redeploys to the state the cluster
is running now, so it isn't the file the stack was first deployed from, and it references secrets and configs
rather than exporting their contents.

## Rotating a secret or config

Swarm can't change a secret or a config once it exists, so rotating one means replacing it:

1. On the **Secrets** list, create the replacement under a new name, such as `db_password_v2`. This needs
   [operations level][operations-level] 2.
2. On the replacement's page, add the label `cetacean.version-of=db_password` so it
   [groups with the old one](#versions). If the old one carries a `com.docker.stack.namespace` label, add that too.
3. Open the old secret. Its page lists every service that mounts it. On each service, edit **Secrets**: add the
   replacement with the same **Target** the old one had, so the container reads the same file, and remove the old
   one. Saving rolls the service out once. This also needs level 2.
4. Once the old secret's page lists no services, remove it. This needs level 3, and Docker refuses while any service
   still mounts it.

Configs follow the same steps from the **Configs** list and each service's **Configs** editor. A service deployed
from a Compose file gets the old reference back on the next `docker stack deploy`, so change the file as well.

Over the API, the same sequence is `POST /secrets`, then `PATCH /services/{id}/secrets` for each service named in
`GET /secrets/{id}`, then `DELETE /secrets/{id}`; see the [API guide][api].

### Versions

Rotation leaves a trail of versions behind, so the config and secret lists show only the newest version of each
group; **+N older** loads the others beneath it. The **Used by** column counts the services using each one, and an
older version none of the services you can see uses says so.

Versions group only within one stack, by their `com.docker.stack.namespace` label, so a replacement must carry the
same stack label as the version it replaces. Within a stack, a config or secret labeled `cetacean.version-of=<name>`
is a version of `<name>`. Without that label, Cetacean matches names instead. Either way it drops a `<stack>_` or
`<stack>-` prefix and a version suffix, which is `_` or `-` followed by either eight or more hex digits
(`app_env_0a1b2c3d4e5f`, as `docker-stack-deploy` and SwarmCD write) or `v` and a number (`db_password_v3`). A suffix
like `_1` isn't one. So `myapp_db_password` groups with `myapp_db_password_v2`, and with a replacement labeled
`db_password` in the same stack. A group matched by name says **inferred from names**, because a name can match by
accident; label your versions to make it certain.

The newest version is the one created last. Each older version that no service uses is flagged as an
`unused-version` [recommendation][recommendations], so you know what's safe to remove. Both the groups and the counts
cover only the configs, secrets, and services you may read.

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

Service and task detail pages carry a log viewer. The live toggle in the toolbar starts and stops the stream; while
it runs the view follows the bottom of the output, and scrolling up pauses that until you scroll back down.

- Time range: presets from the last 5 minutes upwards, or a custom since/until pair
- Filters: by level (parsed from the line, including JSON and `log/slog` numeric levels) and by stream
- Search: substring or regular expression, with match navigation and highlighting
- Rendering: JSON payloads pretty-printed, levels color-barred
- Download: saves the lines currently loaded as a `.log` file

## Topology

The topology page has two views.

- Logical: one card per service, grouped into its stack, with an edge between any two services sharing an overlay
  network, listing the shared networks. Hovering a card dims everything it isn't connected to, and the legend maps
  colors to stacks.
- Physical: one card per cluster node, listing the tasks placed on it.

Both views pan, zoom and let you drag cards, and clicking a service opens its detail page.

## Swarm page

The swarm info page shows the cluster's join tokens and its Raft, orchestration, dispatcher, CA, and encryption
settings, each editable in place where the [operations level][operations-level] and your grants allow it. Rotating
a join token asks for confirmation.

Below the settings, a **Plugins** section lists the installed Docker plugins and links each to its own page, where
you can enable, disable, configure, upgrade, or remove it. An **Install Plugin** button appears when your grants
allow installing. It takes a remote reference and shows the privileges the plugin requests before you confirm.

## Profile

In every [authentication][authentication] mode except `none`, the identity badge opens a profile page. It shows
how you're signed in, with your subject, email, and groups where the provider supplies them, and the certificate
issuer and expiry in `cert` mode. Under `oidc` it carries the **Sign out** button.

## Installing the dashboard

Cetacean serves a web app manifest, so a browser that supports it offers to install the dashboard as an app with
its own window and icon. It's the same dashboard—it still needs a connection to Cetacean, and nothing is cached
for offline use.

## History

The history page at `/history`, linked from the recent activity on the cluster overview, lists the latest 200
changes Cetacean has seen. Filter it by resource type, or by name with the search box. The download button saves
the whole log Cetacean still holds as CSV.

A change made through Cetacean names who made it and whether they used the dashboard, the API, or MCP. A change
with no name came from somewhere else, such as the Docker CLI. The history lives in memory, so it starts empty
after a restart.

## Atom feeds

Every resource list and detail page shows a feed icon in the page header. Click it to open that page's Atom feed,
or copy the URL into a feed reader. The history, search and recommendations pages have feeds too. See the
[API guide][feeds] for the supported endpoints and pagination.

## Licenses

The licenses page, linked from the footer, lists every open-source dependency bundled into Cetacean, both Go
modules and frontend packages. Search by name, or filter by ecosystem and license. Clicking a license badge opens
the full text for that dependency, with its NOTICE file if it ships one. The header links to the complete
attribution document.

[api]: api
[authentication]: authentication
[authorization]: authorization
[feeds]: api#feeds
[filter-fields-by-resource]: api#filter-fields-by-resource
[monitoring]: monitoring
[operations-level]: configuration#operations-level
[recommendations]: recommendations
