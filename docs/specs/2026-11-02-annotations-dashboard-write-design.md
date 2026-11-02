# Annotations: dashboard write — design

Step 6 of `docs/specs/2026-11-01-annotations-design.md`. The dashboard can show annotations as of
step 5 (#586). This step lets people write them: create from a chart or a form, then edit and
delete. It refines the parent spec's "Dashboard" section and does not repeat it.

## Why it needs more than the parent spec

The parent spec says "cluster-wide is offered when the caller may create it" and "edit and delete
when the annotation's `Allow` permits them". The dashboard can't learn either of those today:

- No response says whether the caller may create a cluster-wide annotation (`write` on `*`).
  `/profile` lists grants, but matching ACL patterns in the browser would copy server rules into
  the client.
- `/history` items carry no per-item permissions. Only `GET /annotations/{id}` reports them.

A resource the caller may annotate is already signalled: service, stack and node details link
`/annotations` with `rel="edit"`.

## Two PRs

Both are stacked on #586.

- **6a, create:** the capability signals, annotate mode, the create form and its entry points.
- **6b, edit and delete:** the card menu, the edit form, delete, and the minors deferred from
  step 5.

## 6a: creating

### Capability signals (server)

- **Cluster-wide:** `GET /cluster` links `/annotations` with `rel="edit"` when annotations are
  enabled and the caller has `write` on `*`. The cluster is the resource a cluster-wide note is
  about, so this is the same rule the resource details follow. Copy `linkAnnotations`. Pages that
  offer "Cluster-wide" read it through the existing `["cluster"]` query.
- **Any annotation:** `GET` and `HEAD /annotations` list `POST` in `Allow` when annotations are
  enabled and the caller may create at least one annotation: `write` on `*` or on any service,
  stack or node. `acl.Evaluator.TypeGrants` already answers the second part.
- `docs/api.md` (Annotations) and `api/openapi.yaml` document both. Each gets a spec claim if
  the registry covers it.
- **The client** gets a helper that reads whether a response links `/annotations` with
  `rel="edit"`. The existing `editableOf` only collects sub-paths of the page, so it can't see
  this link. `useDetailResource` and the cluster query expose it as `annotatable: boolean`.

### Annotate mode (charts)

- An **Annotate** toggle button sits in the metrics panel header. It shows only on a panel whose
  charts draw annotations, for a resource the caller may annotate. A note made there then appears
  where it was drawn. In practice that means:
  - the service page's panel and the node page's "Metrics" panel, when the detail carries the
    link
  - not the "Resource Usage by Stack" panels (overview and node page), which draw no timeline;
    their drill-down clicks are therefore never in conflict
  - not task pages (a note can't name a task), list pages, or the stack page (no panel)
- Turning it on switches every synced chart in the panel:
  - a click marks a point
  - a drag marks a range, and the zoom plugin's drag is off while the mode is on
  - on touch, two taps make a range
  - `Esc`, the toggle, or a submitted form leaves the mode
  - click-to-isolate a series is off while the mode is on

  The cursor and the toggle's pressed state show that the mode is on.
- The gesture opens the **create form** in a popover anchored at the chart, with the times
  prefilled.

### The create form

One component, used in the chart popover and in a dialog for the buttons below.

- **Text:** required, up to 1,000 characters, multi-line.
- **Tags:** chips. Typing a space, a comma or Enter commits a tag. A tag must match
  `[a-z0-9._-]{1,40}`, and there are at most 10. Invalid input is refused inline, before
  submitting.
- **Times:** start, plus an optional end, in local time. From a chart they are prefilled and
  still editable; from a button they default to now and no end. The end must be after the start.
- **Scope:** one resource or cluster-wide, never several. Naming more stays possible over REST
  and MCP.
  - On a service or node page: that resource, with "Cluster-wide" as the alternative when
    allowed.
  - On the History page: a resource picker (services, stacks and nodes, through the existing
    search) plus "Cluster-wide" when allowed.
- **Submit:** `POST /annotations`. On success the form closes and the new note appears through
  the `annotation` event; no optimistic insert is made. A problem response shows its `detail`
  inline and keeps the form open with its input, so a `403` for a resource picked on History
  reads as a refusal, not a crash.

### Entry points besides the chart

- **Recent Activity** on a service or node page gets an "Add annotation" button when the detail
  carries the link. The section shows when the caller may annotate, even with no activity yet.
  Node pages render it through `ActivitySection`; service pages render it inline.
- **Stack notes** are made from the History page's picker. The stack page has no metrics panel
  or activity section, and adding them is a separate change.
- **The overview** offers no way to create a note. Its only panel draws no annotations; notes
  already show in its Recent Activity.
- **The History page** gets an "Add annotation" button in its header when `Allow` on
  `HEAD /annotations` includes `POST`.

## 6b: editing and deleting

- Each note card gets a "…" menu button. Opening it requests `GET /annotations/{id}`:
  - `Allow` decides which of **Edit** and **Delete** are offered. With neither, the menu says
    "You can't change this note".
  - The `ETag` is kept for `If-Match`.

  That's one request per click, not one per card.
- **Edit** opens the form in a dialog with text, tags and times filled in. The scope is shown
  read-only: changing it needs the permissions creating it would, and it is rarely wanted.
  Submitting sends `PATCH` with `application/merge-patch+json` and only the changed fields.
  Clearing the end sends `until: null`.
- **Delete** asks for confirmation in an alert dialog, then sends `DELETE` with `If-Match`.
- **A `412`** reads "This note changed since you opened it". The form reloads the note and keeps
  the user's edits on screen so they can reapply them.
- **Chart tooltips** stay read-only. Editing starts from a card.
- **Fixes deferred from step 5:**
  - "1 more note" is pluralised
  - the tooltip text breaks long words
  - the demo history filter applies the stack and `tag` rules
  - tag chips are keyed by position and tag

## Demo mode

The demo handlers accept `POST`, `PATCH` and `DELETE` on `/annotations` against an in-memory
list, send the `annotation` event, and advertise every capability. This lets the public demo show
the whole flow.

## Not in this step

- Several resources on one note from the dashboard.
- A stack page metrics panel or activity section, and a timeline on the overview's panel.
- Changing a note's scope from the dashboard.
- Editing from the chart tooltip.
- Edit and delete over MCP (the parent spec keeps them REST-only).

## Left to the builder

- Layout and styling of the toggle, popover and dialogs, following the existing write dialogs
  (for example the service scale and labels editors).
- How annotate mode reaches every synced chart: through `ChartSyncProvider` or the panel's
  context, whichever already reaches them all.
- The time inputs. A native `datetime-local` is fine.
