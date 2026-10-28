# Timeline store — design

**Issues:** [#487](https://github.com/Radiergummi/cetacean/issues/487) (persist the history),
shaped by [#496](https://github.com/Radiergummi/cetacean/issues/496) (time windows),
[#497](https://github.com/Radiergummi/cetacean/issues/497) (chart markers) and
[#498](https://github.com/Radiergummi/cetacean/issues/498) (annotations)

## Problem

The change history is a ring of 10,000 entries that lives in memory and starts empty with the
process. A restart empties the history page, every resource's recent changes, the CSV and feeds,
MCP's `get_events`, and the actors #438 attached to entries. The restart that follows an incident
is exactly when someone needs that record.

Persisting the ring alone would fix that. But the features queued behind it all add things that
happened at a time, about some resources:

- the windows Cetacean wasn't watching
- annotations people and agents write (#498)
- later, a tag moving to a new digest, a crash loop starting, or a container the agent sees
  outside Swarm

So this change persists a **timeline**, of which Docker-reported changes are the first kind. It
comes with one ID sequence, one store and one query surface, so each later kind is a row type,
not another file.

It is not the task timeline of #546, which the dashboard derives from the tasks Swarm still
retains and which neither stores nor adds anything.

## Scope

In:
- the timeline model
- a SQLite store for it, behind an interface
- restoring the ring at startup
- retention
- gaps as timeline items
- `storage.history_entries` and `storage.timeline`
- the constraints that keep a shared database possible

Out, each with its own issue:
- the annotation API and interface (#498), beyond the room this design leaves for it
- time windows on REST (#496)
- reading further back than the ring holds

Unchanged: `snapshot.json` is current state, not a record of events, and the cluster can't be
rebuilt from the timeline. `oauth-tokens.json` holds credentials. Neither moves.

## The timeline

An item is a `HistoryEntry` with a **kind**:

| Kind | What it is | Where it comes from |
|---|---|---|
| `change` | A change Docker reported, with its actor when made through Cetacean | the cache's `notify`, as today |
| `gap` | A window nobody was watching, from `timestamp` to `until` | recorded when the watcher becomes ready; see [Gaps](#gaps) |
| `annotation` | Reserved for #498 | — |

All kinds share one ID sequence, so paging by `before`, `Last-Event-ID` replay, the SSE stream and
the feeds carry every kind without a mechanism of their own. `HistoryEntry` gains `Kind` and
`Until`; every other field keeps its meaning, and a gap leaves the resource fields empty.

**One vocabulary.** MCP's `projection.TimelineEntry` already has a `kind` of `"change"` or
`"log"`, where `log` is reserved for container log lines merged in for an agent. The kinds above are the
same strings, declared once as constants beside `HistoryEntry` in `internal/cache`. `log` stays
the projection's own: a log line is read from Docker on demand, never recorded, so it never
reaches the store or `/history`. `get_events` copies `HistoryEntry.Kind` instead of writing
`"change"`.

### On the wire

`/history` stays the route and becomes the timeline. Every item carries `kind`, and a gap has
`@type: "Gap"` and `until`. A `kind` query parameter narrows the list; without it every kind is
returned. A gap names no resource, so it is included in a list filtered by `resourceId` as well:
it was a gap for that resource too.

This is breaking for a client that assumes every item is a change. It ships before 1.0, with a
**Breaking** changelog entry naming `?kind=change` as the way to keep the old list. After 1.0 a new
kind is an addition, and clients must ignore kinds they don't know; `docs/api.md` says so.

- **CSV** gains `kind` and `until` columns at the end. A gap is a row: "nothing recorded from …
  to …" is part of what the log says.
- **Atom and JSON Feed** carry a gap as an entry, titled for what it is.
- **MCP** `get_events` returns `projection.TimelineEntry`. A gap is `kind: "gap"`, and
  `TimelineEntry` gains `until`, in the same fixed-width format as `at`.
- **SSE** gains a `gap` event, which the AsyncAPI document lists. The server accepts connections
  before the watcher is ready, so a client connected during startup receives it like any other
  event.
- **The dashboard** draws a gap as a divider on the history page and in every resource's recent
  changes, and later as a shaded band on the charts (#497).

## Storage

One SQLite database, `<data_dir>/cetacean.db`, through `database/sql` and
[`ncruces/go-sqlite3`](https://github.com/ncruces/go-sqlite3). The driver is pure Go: SQLite
translated to Go ahead of time, with no CGO, no WebAssembly runtime and no compile step at
startup. Measured on a minimal static build, it adds about 6 MB to a binary of 63–66 MB and three
modules, and opens a database in milliseconds. `modernc.org/sqlite` was the alternative: 1 MB
smaller, but with twice the modules.

The connection is opened with:
- `journal_mode=WAL` and `synchronous=FULL`, so a committed batch survives a power loss
- `locking_mode=EXCLUSIVE`, so SQLite keeps no shared-memory file, which makes a data directory
  on NFS work. A second process on the same volume can't use the database: it logs a warning
  and runs without persistence instead of corrupting it.
- a single connection; at this rate, one is enough

The file and its WAL are `0600` in the `0700` data directory.

### Schema

```sql
CREATE TABLE timeline (
    id               BIGINT PRIMARY KEY,  -- assigned by the recorder, never by the database
    kind             TEXT    NOT NULL,
    at               BIGINT  NOT NULL,    -- Unix nanoseconds, UTC
    until            BIGINT,              -- a range's end; NULL for a point
    type             TEXT,
    action           TEXT,
    resource_id      TEXT,
    name             TEXT,
    summary          TEXT,
    service          TEXT,
    stack            TEXT,
    service_id       TEXT,
    actor_subject    TEXT,
    actor_name       TEXT,
    actor_provider   TEXT,
    actor_client     TEXT,
    actor_transport  TEXT
);
CREATE INDEX timeline_resource ON timeline (resource_id, id);
CREATE INDEX timeline_service  ON timeline (service_id, id);
CREATE INDEX timeline_at       ON timeline (at);

CREATE TABLE coverage (
    id          BIGINT PRIMARY KEY,
    started_at  BIGINT NOT NULL,  -- the watcher reported ready
    seen_at     BIGINT NOT NULL   -- last heartbeat
);

CREATE TABLE schema_migrations (version BIGINT PRIMARY KEY, applied_at BIGINT NOT NULL);
```

- `service`, `stack` and `service_id` are fields the API never returns, yet replay authorizes a
  removal with them and a service's timeline includes its tasks through them. A row holds every
  field the ring does.
- **The actor is in columns, not a JSON blob.** That way "everything alice did" stays a query,
  which an audit log will want.
- **`id` is explicit** and never the database's own row counter. The sequence belongs to the
  recorder, which is what makes a shared database possible (see
  [Multi-instance](#multi-instance)).

### Migrations

Migrations are numbered SQL files, embedded and applied in order at startup, each in its own
transaction. Within 1.x they only add: tables, nullable columns and indexes. Every statement names
its columns, so an older binary opening a newer database keeps reading and writing it and leaves
the new columns empty. This is the SQL form of "ignore unknown fields". The v1 promise
(`2026-10-24-v1-compatibility-design.md`) gains `cetacean.db` in its on-disk row.

## Writing

A single writer goroutine owns the connection. It does not receive entries from `notify`: a
channel there would either block the cache on the disk or drop entries when full. Instead it
follows the ring. Every second it:

1. calls `History.Since(lastWritten)`
2. inserts what comes back, updates this process's `coverage.seen_at`, and, when it inserted
   anything, applies retention, all in one transaction
3. advances `lastWritten`

`notify` is untouched, and the cost is one read lock and one commit per second.

- **Crash window.** A crash loses at most the last second, and the next gap starts at the last
  `seen_at`. So the record is honest about what it lost, to within a second, even when the cluster
  was quiet.
- **Overrun.** More than a ring's worth of changes between two ticks makes `Since` answer
  `ok=false`. The writer then inserts what the ring still holds. The entries it missed were also
  evicted from memory, so this is retention that came early, not a gap; the IDs simply jump.
- **Write failures** log a warning and are retried from the same `lastWritten` at the next tick.
  Nothing is lost while the ring still holds the entries. A full disk degrades to today's
  behaviour, never to an error a caller sees.

### Retention

A transaction that inserted items keeps the newest `storage.history_entries` items of the kinds
the ring holds, and deletes the rest. A tick that inserted nothing can't have pushed anything out,
so on a quiet cluster the commit is the heartbeat alone; the first tick after startup always
retains, since the size may have shrunk. Retention also drops coverage rows that end before the
oldest kept item. SQLite reuses the freed pages, so the file settles at a steady size without
`VACUUM`. Annotations (#498) are outside this rule; their retention is #498's to decide.

### Shutdown

Main closes the writer after the watcher has stopped, inside the same shutdown grace. The entries
the final events produced are committed with a last `seen_at`.

## Reading

At startup, before the watcher starts and so before any `Append`:

1. Open and migrate the database.
2. Select the newest `storage.history_entries` items, oldest first, into the ring through a new
   `History.Restore`. It rebuilds the per-resource index and sets the counter to the largest ID,
   not the row count, so new items continue the sequence. The size used is the one configured
   now. A smaller size than the database was written under keeps the newest items, and the next
   retention pass deletes the rest.
3. Start the writer with `lastWritten` at that counter.

`Since` reads the oldest ID from the ring instead of deriving it as `count - size + 1`. That
derivation assumes IDs run without jumps, which an overrun breaks.

Reads stay in memory, as today. The store serves startup and nothing else in this change. Reading
further back than the ring (#496 over long windows, an audit log) is a later change that needs no
new format.

## Gaps

Changes made while nobody watched produce no events. The first full sync absorbs them with one
`sync` event, which history doesn't record, so there is no burst of misleading "created" entries.
The opposite problem remains: a restored history followed by new items reads as if nothing
happened in between.

When the watcher reports ready, Cetacean opens this process's coverage row and, if an earlier row
exists, records a `gap` item through the cache. The gap runs from that row's `seen_at` to now. It
goes through `notify` like any change, so the ring, the writer, SSE and every listing pick it up
with no path of their own. When the startup sync succeeds, which is the usual case, nothing is
recorded before it, so the gap is the first item after the restored ones. If Docker was
unreachable at first, changes the event stream delivered before the first successful sync come
first. The gap then ends later than it had to: it claims less coverage, never more.

A process that never became ready opens no coverage row, so its lifetime falls inside the next
gap. That is the right answer: it watched nothing.

## Replay

The broadcaster's epoch stays per process, and a `Last-Event-ID` from before a restart gets
`full_sync`, as today. Continuing it would be wrong: the changes made during the downtime are in no
item, and replay would report the stream complete without them. A cursor from this process is
always at or above the restored counter, so replay never sends a restored item, and the
broadcaster needs no change.

## Settings

- **`storage.timeline`** (`CETACEAN_TIMELINE`, `-timeline`, default `true`) turns persistence of
  the timeline on and off. It is not folded into `storage.snapshot`: the snapshot is current state
  served while the live sync catches up, the timeline is a record, and turning off one says
  nothing about wanting the other. The data directory is created when either needs it, and its
  documentation names `cetacean.db`.
- **`storage.history_entries`** (`CETACEAN_HISTORY_ENTRIES`, `-history-entries`) sizes the ring
  and the retention on disk. The default is 10,000; it accepts 1,000 to 100,000, and startup
  refuses anything outside that range, as it does for the `sizing.*` ranges. It applies with or
  without persistence. The upper bound exists because a search with no match walks the whole ring
  under the read lock every `Append` waits on. The size also bounds how far `Last-Event-ID` replay
  reaches and what an unpaginated CSV download returns.

Retention by age, once reads can go past the ring, is a later setting.

## Annotations

#498 adds the `annotation` kind. This design leaves it the following:

- **The same sequence.** Creating, editing and deleting an annotation are timeline items, so
  replay, SSE and the feeds carry them.
- **Its own table** for each annotation's current state, outside the ring's retention. An
  annotation must outlive task churn.
- **The `until` column,** since an annotation can be a range.
- **An open question for #498:** an annotation added at 15:00 about 14:00 sorts by its ID in a
  listing, but sits at 14:00 on a chart. Whether listings sort it by the time it describes is
  #498's to decide.

## Multi-instance

Nothing here builds a shared database, but nothing may rule one out. The rules:

- **One interface.** Callers use a `timeline.Store`: restore, append a batch, retain, open
  coverage, heartbeat, close. They never see SQL. A Postgres implementation would write its own
  SQL; one conformance test suite runs against every implementation.
- **One recorder.** IDs come from whichever instance records. With several instances, a lease row
  chooses it, and the others only read. The time between a recorder failing and another taking
  over is a gap, recorded the same way: a gap means "nobody was watching", not "Cetacean was
  down".
- **Replay across instances** needs the epoch to belong to a stretch of coverage and be stored,
  instead of being random per process.
- **Reads behind the interface.** A single instance serves them from the ring; instances sharing
  a database would read from it.

What multi-instance needs beyond the timeline, recorded so it isn't mistaken for done:

- **Pending attributions** live in the process that made the write, which may not be the one that
  records the event.
- **The OAuth state** is a local file, and an auto-generated signing key differs per process.
- **The secret-entry handoff** behind `/secrets/new` is an in-memory map.
- **Connection caps per owner** apply per process.
- **The key that signs MCP round-trip state** is random per process, so a retried call that lands
  on another instance is refused. MCP itself already runs without sessions; the key is the only
  part of it that would need sharing.

An external database also pays off with a single instance: `storage.data_dir` is a local volume,
so a Cetacean that Swarm reschedules onto another manager leaves its timeline behind.

## Where it lives

- **`internal/timeline`:** the `Store` interface, the SQLite implementation, the migrations and the
  conformance suite.
- **`internal/cache`:**
  - `Kind` and `Until` on `HistoryEntry`
  - `History.Restore` and the `Since` change
  - recording a gap
  - the writer that follows the ring

`run()` in `main.go` opens the store beside the snapshot load, before `docker.NewWatcher`, gated on
`storage.timeline` and on `dataDirReady`; `storage.timeline` joins the condition that creates the
data directory.
`.claude/ARCHITECTURE.md` gets a line on the trap: the writer follows the ring and must never be
fed from `notify`.

## Decided

1. **A timeline, not a history file.** Kinds share one sequence, one store and one query surface.
   The snapshot and the OAuth state stay out.
2. **SQLite through `ncruces/go-sqlite3`**, behind `timeline.Store`. JSON Lines would have meant
   writing compaction, tombstones and crash recovery by hand, and every kind with its own lifetime
   would have added a rule.
3. **The writer follows the ring every second.** `notify` neither blocks on the disk nor drops
   entries.
4. **Gaps are timeline items,** recorded through `notify`, with their end tracked to the second by
   a heartbeat.
5. **`/history` becomes the timeline before 1.0,** with `?kind=change` for the old list.
6. **The epoch stays per process.** Pre-restart cursors get `full_sync`, because replaying across
   a gap would report a stream complete that isn't.
7. **Additive migrations only** within 1.x, with columns always named, so rollbacks keep working.
8. **`storage.history_entries`**, 10,000 by default, bounded to 1,000–100,000.
9. **`storage.timeline`** switches persistence, separate from `storage.snapshot`.

## Open

1. **Recording what changed during a gap.** Comparing the snapshot with the first sync is ruled
   out: it sees net differences only, with no times and no actors. Replaying the daemon's events
   from the gap's start would recover real actions for a short gap. The engine keeps its last 256
   in memory and serves them to a subscription with `since`. Its limits:
   - the buffer is shared with container events, so on a busy manager it spans minutes
   - it is lost when the daemon restarts
   - it is per daemon
   - tasks have no swarm events

   A gap would narrow, never close. A candidate follow-up.
