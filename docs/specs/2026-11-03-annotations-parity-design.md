# Annotations: Grafana parity — design

**Issues:** [#592](https://github.com/Radiergummi/cetacean/issues/592) and the rest of #498.
Builds on `2026-11-01-annotations-design.md` and `2026-11-02-annotations-dashboard-write-design.md`,
and does not repeat them.

## Problem

Annotations work end to end, but next to Grafana's they are thin:

- **Querying:** there's one tag filter, no filter by author, and no way to list the tags in use.
- **MCP:** agents can create a note but can't edit or delete one, or read it as a resource.
- **Dashboard:** notes can't be filtered or hidden on charts, a chart marker can't be acted on,
  and the stack page has no notes.
- **Browser:** `/annotations` and `/annotations/{id}` render the SPA's not-found page, so the
  `@id` of every note is a dead link in a browser. Every route must have a useful HTML view.

The stack review also left a list of small issues (#592). This design takes every item on it.

## API

### Filters

`/history`, `/annotations` and their feeds and CSV take:

| Parameter | |
|---|---|
| `tag` | Repeatable. A note matches when it carries **every** given tag |
| `matchAny` | `true`: a note matches when it carries **any** given tag. Grafana's name and meaning |
| `author` | The author's actor subject. Matches the author, not the last editor |
| `kind` | Repeatable on `/history`: `change`, `gap`, `annotation`. Omitted means all |

- A gap still passes `tag` and `author`, as it passes `search`, so a filtered timeline still
  shows where Cetacean wasn't watching.
- `matchAny` without `tag` is ignored, not refused. A `matchAny` that isn't a boolean is `API016`.
- A repeated `kind` lets Recent Activity fetch changes and notes in one request, replacing the
  separate gap fetch whose window one backdated note stretched (#592).

### `GET /annotations/tags`

The tags the caller can see, with how many notes carry each, most-used first.

- `?q=` narrows to tags starting with it; `?limit=` caps the list (default 100, at most 1,000).
- Only notes the caller may read are counted, so a count never reveals a hidden note.
- A `CollectionResponse` of `{"@type": "AnnotationTag", "tag", "count"}`, each `@id`
  `/annotations?tag={tag}`. JSON, CSV and HTML; no feeds, since a tag has no time.
- Grafana's `GET /api/annotations/tags` is the model.

### `/annotations` gets its own semantics

It currently reuses `/history`'s handler and quirks (#592).

- With `storage.timeline` off it answers `ANN003` (`409`), as `POST` does. `/history?kind=annotation`
  keeps answering an empty list, because `/history` exists without annotations.
- It refuses `tasks` and `kind`, which mean nothing for notes.
- Each item has the shape of `GET /annotations/{id}`'s `annotation`: `tags` and `resources`
  are always arrays, and the change fields (`action`, `type`, `name`, `resourceId`) are absent.
  `/history` keeps its own entry shape, since it mixes kinds.
- It gains Atom and JSON Feed, the same entries `/history`'s feeds give a note.

### Resources, timelines and removal

- **A stack's timeline includes notes on its services.** The rule becomes symmetric: a service
  matches notes on its stack, and a stack matches notes on its services. Each note stores its
  service's stack already.
- **A removed service keeps its stack's notes.** The stack is read from the service's removal
  entry in the ring, which records it, and once that is evicted from the `AnnotationResource.Stack`
  of a note that named the service. The live service is gone.
- **A removed resource is marked.** A note's resource carries `"removed": true` when the cache
  no longer holds it. The dashboard renders its name without a link.
  - That changes the representation without a write, so the detail's `ETag` adds a bitmask of
    the removed resources to the version. Preconditions are checked against the same value.

### CSV

Annotation rows in `/history.csv` and `/annotations.csv` fill `id` with the note's ID, and gain a
`resources` column: `type:name` pairs, comma-joined, with `cluster` for a cluster-wide note.

### Signals the dashboard reads

- `/history` and `/annotations` link `/annotations` with `rel="edit"` when the caller may create
  some annotation. This replaces `HEAD /annotations`'s `Allow` as the History page's signal; the
  `Allow` header stays, since every GET reports it.
- The cluster-wide signal is read from the existing `["cluster"]` query, as the step-6 design
  said, instead of `HEAD /cluster`.

### Efficiency and cleanup

- The annotation index keeps notes in a slice ordered by ID, beside the map by ID. `/history`
  binary-searches to `before`, then walks back until `limit` match, instead of copying and
  sorting every note per request.
- `mayAnnotate` drops its `CanWriteEverything` call, which `TypeGrants` already covers.

### Deploy annotations from CI

`docs/api.md` gets a recipe: annotate a deploy from a pipeline with a token and `curl`, tagged
`deploy` and naming the service. It's how most people use Grafana's annotation API.

## MCP

- **`update_annotation`:** takes `id`, any of `text`, `at`, `until`, `tags`, `resources`, and
  the optional `version` the editing tools take. An empty `until` makes a point. Annotations:
  read-only false, destructive false, idempotent true.
- **`delete_annotation`:** takes `id` and the optional `version`. Destructive true, idempotent true.
- Both call `cluster.Annotations`, have no `op`, and are listed at every tier, like
  `add_annotation`. The named exceptions in the tool-catalog tests grow by these two.
- **Resource template `cetacean://annotations/{id}`:** the note, as REST's detail. It can be
  subscribed to, and an edit or delete sends `notifications/resources/updated`.
- **`get_events`** gains `kinds`, `tags`, `matchAny` and `author`, with REST's meanings. A
  `cetacean://annotations/tags` resource lists the tags, as the REST route does.
- **#592:** all three annotation tools are hidden from `tools/list` when `storage.timeline` is
  off. Validation errors name the MCP argument (`at`, not `timestamp`).
- The evaluation set gains a case for each new tool.

## Dashboard

### Pages

Every annotation route renders a page:

- **`/annotations`** lists notes as note cards, newest first, paged like History. Its filters
  are the tag chips, an author control ("Anyone" / "Me", which sends the caller's subject from
  `/profile`), the time window and a text search. The filters live in the URL, so a filtered view
  can be shared. The header carries "Add annotation" when the list links `rel="edit"`, and
  links the feeds and CSV, as History does.
- **`/annotations/{id}`** is the note on its own: the card, the full times, the author and the
  last editor, each resource (linked unless removed), and the edit and delete actions its `Allow`
  permits. "Show on chart" opens the first resource with a metrics panel, with its window set
  around the note.
- **`/annotations/tags`** lists the tags with their counts. Each tag links to
  `/annotations?tag={tag}`.
- The sidebar lists "Annotations" under History.

### History and activity

- History gets a kind control (all, changes, notes) and the tag chips. A tag on any card is a
  link to the filtered list.
- A note card links to its `/annotations/{id}` page, and has "Show on chart".
- URLs in a note's text become links. The text stays plain; nothing else is rendered.

### The form

- Tags autocomplete from `/annotations/tags?q=`. A new tag is still allowed.

### Charts

- **A notes control** in the metrics panel header: show or hide notes, and narrow them by tag.
  Its state is a URL parameter (`?notes=off`, `?noteTag=deploy`), following the widget
  customization plan: view state that belongs in a link is never stored.
- **Clicking a marker's flag** pins a popover with the note card, its menu and "Open". Hovering
  stays a read-only tooltip. This reverses step 6's "chart tooltips stay read-only" for a click
  only.
  - A click elsewhere on the plot isolates the nearest series, so only the flag cap is the hit
    target. A range note's flag sits at its start.
  - A flag that groups several notes lists their cards in the popover, newest first.
  - A click on a flag doesn't isolate. In annotate mode a click still marks a point, flag or not.

### Stack page

- A Recent Activity section with the stack's changes and notes, and "Add annotation" when the
  stack detail links `rel="edit"`.

### #592

- An `annotation` event invalidates the history and timeline queries only, not the cluster
  snapshot, Topology, Search or resource details.
- `useSwarmQuery` refetches lists on an allowlist of resource event types, so an unknown kind
  refetches nothing.
- The capability checks use the signals above; `HEAD /cluster` and `HEAD /annotations` go.
- One menu component in `components/ui` holds the Base UI menu classes that `AnnotationMenu`
  and `SegmentedControl` each spell out.

### Demo mode

The demo handlers serve the three pages' endpoints, the tag list and the new filters.

## Not in this design

- **Notes generated from PromQL** (Grafana's annotation queries) and **notes from alert state.**
  Both mean stored configuration, which the saved panels design (#510) avoided. They need a
  design of their own.
- Bulk delete, `PUT`, and colour per note or per tag. A merge patch already covers `PUT`.

## Steps

Each step is one PR with its CHANGELOG entry.

1. **Server:** the filters, `/annotations/tags`, `/annotations`'s semantics and feeds, the
   timeline and removal rules, CSV, the signals, the index and the cleanup. OpenAPI, the
   AsyncAPI copy of the resource schema, spec claims, `docs/api.md` with the CI recipe. The
   JSON-LD context's `@vocab` already covers `AnnotationTag`.
2. **MCP:** the two tools, the resources, the `get_events` arguments, the #592 items, the
   evaluation set.
3. **Dashboard groundwork:** the #592 items.
4. **Dashboard features:** the three pages, the History filters, autocomplete, links in text,
   the chart notes control, the marker popover, the stack page section, the demo.

## Decisions

Settled on 2026-11-03:

1. **Stack and service timelines** each include the other's notes.
2. **A clicked chart marker** opens an actionable popover; hovering stays read-only.
3. **Notes from PromQL and alerts** wait for their own design.
4. **Every annotation route renders a page** in the browser, as every other route does.
