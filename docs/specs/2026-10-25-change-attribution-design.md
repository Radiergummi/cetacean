# Change attribution — design

**Issue:** [#438](https://github.com/Radiergummi/cetacean/issues/438)

## Problem

A history entry records what changed and when, never who changed it. A scale made through the
dashboard, the API or MCP reads exactly like one made with the Docker CLI, so in an incident
"who did this" has no answer, even though every one of those writes passed an identity check
moments before.

The difficulty is that Cetacean does not write history when it writes to Docker. The entry comes
later, from the Docker event the write causes, through the same watcher path an external change
takes. The write and the entry have to be tied together after the fact.

## Recording

Every write goes through `docker.Client`, from both transports, so that is where it is recorded.
Immediately before the call to the engine, the client registers a *pending attribution*: what it
is about to change, and the actor taken from the request context. A failed call withdraws it. The
actor travels in the context rather than as an argument, so neither the write methods
nor the two writer interfaces change shape.

The actor record is:

| Field | |
|---|---|
| `subject` | The identity's subject |
| `name` | Its display name, for reading; may be empty |
| `provider` | `oidc`, `tailscale`, `cert`, `headers`, `none`, or `oauth` for a token this deployment issued |
| `client` | The OAuth `client_id`, when the credential was a token this deployment issued |
| `transport` | `dashboard`, `rest` or `mcp` |

The email and groups are left out. The subject already identifies the person, and an address is
the one field whose disclosure people mind.

The record is built from an `auth.Identity` by one function in `internal/cluster`, since both
transports apply it. It is a flat struct with only optional additions in mind: the `confirmed`
flag the elicitation design (`2026-10-24-mcp-elicitation-design.md`) promises #438 is one more
boolean field set by the MCP transport, with nothing else to change.

### Telling the dashboard from a REST caller

The auth layer cannot tell them apart in general. A session cookie exists only in `oidc` mode;
Tailscale, mTLS, proxy headers and `none` authenticate a browser and `curl` identically. What does
differ is what the browser itself adds. So, on the REST transport:

1. A request carrying `Authorization: Bearer` is `rest`. The SPA never sends one.
2. Otherwise one carrying `Sec-Fetch-Site: same-origin` is `dashboard`. A browser sets that
   header and a page cannot, and the cross-origin protection already relies on it.
3. Anything else is `rest` — including a browser on another origin the CORS allowlist admits.

The transport is a label, not a credential: a script can send `Sec-Fetch-Site` itself. Nothing
is authorized on it, and the identity next to it is the part that was checked. MCP is `mcp`,
whichever credential it was reached with.

## Matching

A pending attribution names a resource type, a key, and what it expects to see. The cache checks
every non-sync event against the pending set as it appends the history entry, and an event that
matches carries the actor into the entry and consumes the attribution.

- **Updates** (service, node, config and secret specs; a task whose container was removed) are
  keyed by ID and carry the `Version.Index` the write was made against — the version the client
  read, or the one a precondition pinned. The engine refuses a write against a stale version, so
  the first version above that index is the one the write produced. The first event for the
  resource whose version exceeds it matches. When several Cetacean writes to one resource are
  pending, the one with the highest index wins, and every older one is dropped with it: once the
  resource has moved past their index, they can only ever match a later, unrelated change.
- **Removals** are keyed by ID (by name for a volume, which is what the cache keys it on) and
  match the next `remove` event for it. A removed resource has no next version to wait for.
- **Creates** are keyed by name, since the ID does not exist until the engine answers, and match
  the next `create` event of that type and name. Swarm names of configs and secrets are unique,
  so the name is as exact as an ID would be, and registering before the call means the event can
  never beat the registration.

An event nothing expected keeps an empty actor, which is the signal that the change came from
somewhere else. Task events a service update causes are not attributed: the task did not change
because somebody asked it to, and the service's own entry already says who did.

Exact where Docker reports a version, matching has one known imprecision: the watcher can coalesce
two quick changes into one inspect. If an external change lands right behind ours, the one entry
the cache records covers both and carries our actor. The entry is still true about who started
that change; it is not proof that nothing else happened.

Writes with no history behind them — swarm settings, plugins, the unlock key — record nothing,
because there is no entry to attribute. Their audit trail is the log line they already write.

### Bounds

Pending attributions expire after a minute, which is far beyond how long the watcher takes to
observe a change and long enough to cover a task's container stopping. At most 1,024 are held;
past that the oldest is dropped. A write whose event never comes — a no-op the engine did not
version, or one a resync absorbed — therefore costs a slot for a minute and nothing else.

## Output

- **JSON**: an `actor` object on the entry, absent when unattributed. Documented in the OpenAPI
  `HistoryEntry` schema and mapped in the JSON-LD context.
- **CSV**: `actor`, `provider`, `client` and `transport` columns after the existing ones, empty for
  an unattributed entry. The `actor` column is the display name, falling back to the subject.
- **Atom**: an entry-level `atom:author` naming the actor (RFC 4287 §4.2.1); unattributed entries
  inherit the feed's `Cetacean` author as before. The HTML content ends "by alice via MCP".
  **JSON Feed** gets the same as an item-level `authors`.
- **MCP**: the history resource returns the entry as it stands; `get_events` timeline entries
  gain the same `actor`.
- **Dashboard**: the activity feed — the history page and every resource's recent changes — shows
  "by alice via MCP" after the action.

## Persistence

History is not persisted. The ring lives in memory and starts empty with the process; the disk
snapshot holds resources and restart counters only. Attribution therefore survives a restart
exactly as far as history does: not at all. The snapshot format is unchanged, so every existing
snapshot stays readable, and the pending set is deliberately not persisted either — nothing in
it outlives a minute.

Persisting the ring is its own change, with its own retention questions. When it lands, the actor
is a plain JSON field on the entry and goes with it; an entry written before this change reads
back with no actor, which is the right answer for it.

## Privacy

The actor is identity data, so who may see it matters. It is shown to every caller who may read
the entry: the history filter already admits an entry only to someone with a read grant on its
resource, and that is who the record is about. Somebody who may watch a service scale may know who
scaled it; that is what the history is for. The fields are chosen to keep that cheap — no email,
no groups. Secret data clearing is untouched: the actor describes the caller, never the resource.

## Audit log

Left out. The issue asks whether an append-only log, kept longer than the ring, is wanted as well.
It does not fall out of this change: the ring is bounded by entries and lives in memory, and a
durable log needs a storage format, retention, rotation, and an answer for a write whose event
never arrived — none of which attribution needs. Recording the actor on the entry is what both
would share, and it is done here; a log can subscribe to attributed entries later without
touching the matching.

## Decided

1. **Visibility.** Actors are shown to anyone who may read the entry, with no separate grant or
   setting. A grant would hide the one answer the history page exists to give from exactly the
   operators reading it; a deployment that cannot show names can be offered an `acl.*`-scoped
   redaction later without changing the record.
2. **Display name in the record.** Included so an OIDC subject does not show up as an opaque
   string. It is whatever the provider sent at the time of the write.
3. **No audit log in this change**, as argued above.
4. **History stays unpersisted here.** Attribution follows history; persisting both is
   [#487](https://github.com/Radiergummi/cetacean/issues/487).
