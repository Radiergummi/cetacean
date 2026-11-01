# Annotations — design

**Issue:** [#498](https://github.com/Radiergummi/cetacean/issues/498), building on #487
(`2026-10-26-timeline-store-design.md`), #496 (time windows) and #497 (chart markers).

## Problem

The timeline only knows what Docker reported. What an incident review needs is often missing
from it: "load test started", "upstream DB failover", "rolled back because of the 502s". People
and agents need to write that down at the moment it happened, and find it later wherever the
history is shown.

This makes Cetacean a source of record for something besides the swarm, which the saved panels
design (#510) avoided. Here that can't be avoided: no other owner exists for the note.

## The annotation

| Field | |
|---|---|
| `id` | The sequence ID of the item that created it. Stable across edits |
| `timestamp`, `until` | The moment it describes, or the range; `until` absent for a point |
| `text` | Plain text, at most 1,000 characters, no markup |
| `tags` | Up to 10, each `[a-z0-9._-]{1,40}`. Grafana's `tags` |
| `resources` | Up to 20 `{type, id, name}`, type `service`, `stack` or `node`; empty means cluster-wide |
| `actor` | The author's actor record from #484: "by alice via dashboard" |
| `updated`, `editor` | When it was last edited and by whom; absent until it is |

`timestamp` and `until` take any time, including the future: a planned maintenance window is a
legitimate range. `until` must be after `timestamp`. Each resource also stores its service's name
and stack at creation, the way a removal's history entry does, so authorization still works
after the resource is gone (`cluster.CanReadEvent`).

## Storage

A migration adds to `cetacean.db`:

```sql
CREATE TABLE annotations (
    id INTEGER PRIMARY KEY,  -- creation item's sequence ID
    version INTEGER NOT NULL, -- sequence ID of the last write, the ETag
    at INTEGER NOT NULL, until INTEGER, text TEXT NOT NULL,
    created_at INTEGER NOT NULL, updated_at INTEGER,
    actor_subject TEXT, actor_name TEXT, actor_provider TEXT, actor_client TEXT, actor_transport TEXT,
    editor_subject TEXT, editor_name TEXT, editor_provider TEXT, editor_client TEXT, editor_transport TEXT
);
CREATE TABLE annotation_tags (annotation_id INTEGER NOT NULL, tag TEXT NOT NULL,
    PRIMARY KEY (annotation_id, tag));
CREATE TABLE annotation_resources (annotation_id INTEGER NOT NULL, type TEXT NOT NULL,
    resource_id TEXT NOT NULL, name TEXT, service TEXT, stack TEXT,
    PRIMARY KEY (annotation_id, type, resource_id));
```

Tags and resources are rows, not JSON, for the same reason the actor is in columns: "every
annotation on service X" stays a query.

- **Writes are synchronous.** The caller is told `201`, so the row must be committed first. The
  write goes to the writer goroutine, which owns the one connection, and waits for it.
- **The commit happens under `notifyMu`.** The sequence ID is taken under it and held across the
  commit and the ring append, so no item with a lower ID lands after it. A failed commit appends
  nothing and answers `503`. The cost: a slow disk stalls event recording for the commit's
  duration. Annotations come at the rate people type and a commit takes milliseconds, so step 1
  measures it. If it matters, `History` gains ID reservation (reserved under its lock, appended
  in ID order), so the commit can move outside; today `Append` assigns the ID itself.
- **Events and state are separate.** Creating, editing and deleting each append a
  `kind: "annotation"` item to the ring, with `action` `create`, `update` or `remove`. These
  items carry SSE and `Last-Event-ID` replay, and the writer skips them, because the table holds
  the state. `History.Restore` sets the counter from the largest of `timeline.id` and
  `annotations.version`.
- **Reads stay in memory.** At startup every annotation is loaded into an index in
  `internal/cache`, beside the ring. Lists read it, and the ring's annotation items are never
  listed.
- **Retention:** none by age, and ring churn never evicts an annotation. The 10,001st live
  annotation is refused (`ANN002`, `409`) rather than evicting someone's note.
- **Persistence follows `storage.timeline`**, not `storage.snapshot`, since annotations live in
  `cetacean.db`. With the timeline off, annotation writes answer `ANN003` (`409`, "annotations
  need `storage.timeline`") and the dashboard hides the controls. An annotation that would be
  lost on restart breaks the first acceptance criterion.

## Authorization

The rules live in `internal/cluster/annotations.go`, as a `cluster.Annotations` use case shared
by REST and MCP: validate, authorize, record.

- **Read:** a caller sees an annotation only if they may read **every** resource it names. With
  "any", one readable reference would reveal text written about a resource the caller can't
  read. A cluster-wide annotation is visible to every caller with any grant, the way a gap is.
- **Create:** `write` on every named resource. A cluster-wide annotation needs `write` on `*`,
  through a new `acl.CanWriteEverything` beside `CanReadEverything`.
- **Edit and delete:** the author (same subject and provider), as long as they can still read
  it, or anyone who could create it. An edit that changes `resources` must also pass the create
  rule for the new set.
- **No operations tier.** An annotation doesn't touch Docker, so it isn't a `cluster.Operation`
  and works at `server.operations_level` 0, the default. `docs/configuration.mdx` says so beside
  the tier table.
- A refusal on an annotation the caller can't read answers `404`, as `hiddenTarget` does.

## REST

| Route | |
|---|---|
| `POST /annotations` | Create. `201`, `Location`, `ETag` |
| `GET /annotations/{id}` | One annotation. `Allow` lists `PATCH, DELETE` when the caller may |
| `PATCH /annotations/{id}` | `application/merge-patch+json` (RFC 7396); `If-Match` honoured |
| `DELETE /annotations/{id}` | `204`; `If-Match` honoured |
| `GET /annotations` | The same handler as `/history?kind=annotation`, so the collection exists |

The ETag is `version`. Preconditions follow the existing RFC 9110 handling, and each new
requirement is claimed in `internal/spec/registry`. Errors are problem details:

- `ANN001`: `400`, the body is invalid, naming the field
- `ANN002`: the cap is reached
- `ANN003`: the timeline is off

The JSON-LD `@type` is `Annotation`, and `@id` is `/annotations/{id}` wherever the annotation
appears.

The service, stack and node detail responses carry a `rel="edit"` link to `/annotations` when the
caller may annotate that resource. The dashboard gates on that link, never on its own reading of
the policy.

## On the timeline

- **`/history`** merges the annotation index with the ring by ID. An annotation sits at its
  creation ID with its current fields, so an edit changes what it says without moving it. A
  delete removes it. Edits and deletes take sequence IDs, so `History.Count` advances and the
  derived ETag changes.
  - `?kind=annotation` narrows to annotations.
  - The window matches a range that overlaps it.
  - `type`/`resourceId` match annotations that name the resource. A service also matches
    annotations on its stack. A cluster-wide annotation matches every resource filter, as a gap
    does.
  - `search` matches `text`, and a new `tag` filter matches a tag.
- **SSE:** an `annotation` event on `/events` and on the stream of each resource the annotation
  names, carrying the annotation (or its `id` for `remove`). Listed in the AsyncAPI document.
- **Atom and JSON Feed:** one entry per annotation, with its entry ID fixed and `updated` /
  `date_modified` set to the last edit. Feed readers then show an edit as an update. The text is
  escaped.
- **CSV:** `text` and `tags` columns at the end. A cell starting with `= + - @` is prefixed with
  `'` against formula injection.
- **Grafana:** fields are named `text` and `tags`, so Infinity maps them as they are. The docs
  section adds `until` → `timeEnd`. No duplicate `time` field.
- **MCP:**
  - `get_events` returns annotations as `kind: "annotation"`, and `projection.TimelineEntry`
    gains `text` and `tags`. It already has `until`.
  - A new tool, `add_annotation`, takes `text`, an optional `at`/`until` (default now), `tags`
    and `resources` as ids or names resolved like `find`. It is attributed with transport `mcp`.
    Its annotations: read-only false, destructive false, idempotent false. It is registered at
    every tier and has no `op`. Its handler calls `cluster.Annotations`, which checks the ACL.
    Edit and delete stay REST-only until an agent needs them.

## Dashboard

- **`TimelineItem`** gains `{shape: "point", kind: "annotation"}` and `{shape: "range", kind:
  "annotation"}`, carrying the annotation.
- **Rendering** keeps the three kinds apart at a glance:
  - Changes stay thin lines with caps.
  - Gaps stay hatched.
  - A point annotation is a solid line in a new `annotation` semantic colour, with a flag cap
    at the top.
  - A range annotation is a translucent, unhatched band in the same colour.
  - Annotations never cluster with changes. The tooltip shows the text, the tags and
    "by alice via dashboard", plus "edited" when it has been.
- **Creating on a chart.** Drag zooms and click isolates a series, so neither gesture can be
  taken. An **Annotate** toggle in the metrics panel header switches every synced chart into
  annotate mode:
  - a click marks a point, and a drag selects a range (zoom's drag is off in this mode)
  - `Esc` or the toggle leaves the mode
  - on touch, two taps make a range

  A popover then asks for the text and tags. The scope starts as the page's resource, and
  "cluster-wide" is offered when the caller may create it.
- **The History page** and each resource's recent changes get an **Add annotation** button,
  with the same form and editable times. Annotation rows show as a note card: the text, the
  described time ("about 14:00–14:30"), the author, and edit and delete when the annotation's
  `Allow` permits them.
- **Demo mode** seeds one point and one range annotation.

## Steps

Each step is one PR with its CHANGELOG entry.

1. **Store and index:** the migration, `timeline.Store` methods with conformance tests, the
   `cache` index, the restore counter, and an `ARCHITECTURE.md` trap: the ring's annotation
   items are never listed or persisted.
2. **`cluster.Annotations` and REST CRUD:** OpenAPI, the JSON-LD context, problem codes, spec
   claims, and a section in `docs/api.md`.
3. **The timeline surfaces:** the `/history` merge and filters, SSE, AsyncAPI, the feeds, CSV,
   and the Grafana docs.
4. **MCP:** `get_events` and `add_annotation`, the catalog drift check, and an evaluation-set
   case.
5. **Dashboard read:** the timeline kinds, the chart layer, the activity rows, and demo data.
6. **Dashboard write:** annotate mode, the forms, and edit and delete.

## Decisions

Settled on 2026-11-01:

1. **ACL permission:** `write` on every named resource. A separate `annotate` permission can come
   later; it would only widen what is allowed, so starting narrow blocks nothing.
2. **Operations tier:** none. Annotations work at `server.operations_level` 0: the tiers bound
   what Cetacean does to Docker, and a read-only deployment is where incident notes matter most.
   This is the one write that skips `requireLevel`; `docs/configuration.mdx` says so.
3. **Listing order:** by creation ID, with the chart placing an annotation at the time it
   describes. The `before` cursor and replay are defined over IDs.
4. **Retention:** no expiry by age; a cap of 10,000 live annotations, refusing the next one. A
   refusal is visible, an eviction is not.
5. **Cluster-wide:** creating one needs `write` on `*`; every caller with a grant can read it, as
   with gaps.
6. **Chart gesture:** an Annotate mode toggle. A modifier key can't be discovered and doesn't
   exist on touch.
