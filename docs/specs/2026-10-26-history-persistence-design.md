# Persisting the change history — design

**Issue:** [#487](https://github.com/Radiergummi/cetacean/issues/487)

## Problem

The change history is a ring of 10,000 entries that lives in memory and starts empty with the
process. The snapshot restores resources and restart counters after a restart, but not history, so
a restart empties the history page, every resource's recent changes, the CSV and feeds, MCP's
`get_events`, and the actors #438 attached to entries. The restart that follows an incident is
exactly when someone needs that record.

## Scope

The disk makes the ring survive a restart; it does not become the history. Reads stay in memory,
the ring keeps its size, and the file is read once, at startup. Retention beyond the ring, an audit
log, and queries over more than memory holds are out of scope. If they come, they are a different
store (most likely SQLite), and this file is what it imports once.

## Format

One file, `<data_dir>/history.jsonl`: JSON Lines, UTF-8, one record per line, every line ending in
`\n`. The first line is a header; every other line is a record with a `kind`.

```jsonl
{"format":"cetacean-history","version":1}
{"kind":"entry","id":4711,"at":"2026-10-26T09:14:03.120Z","type":"service","action":"update","resourceId":"k3x…","name":"shop_web","service":"","stack":"shop","serviceId":"","actor":{"subject":"alice","provider":"oidc","transport":"dashboard"}}
{"kind":"close","at":"2026-10-26T09:20:00Z"}
{"kind":"open","at":"2026-10-26T09:31:12Z"}
```

- **`entry`** is one history entry, with every field, the internal ones included. `Service`,
  `Stack` and `ServiceID` are `json:"-"` on `HistoryEntry`, yet replay authorizes a removal with
  them and a service's timeline includes its tasks through them. The line must hold them, so it is
  written from its own record type, not from `HistoryEntry`'s tags. The actor gets its own record
  type too. That keeps the file format out of reach of a rename in the API, as
  `internal/oauth/persist.go` already does for the token file.
- **`open`** marks the moment a process started observing: when the watcher reports ready after
  its first full sync. **`close`** marks a clean shutdown. Together they bound the windows nobody
  was watching; see [Gaps](#gaps).
- **Timestamps** are RFC 3339 in UTC, with nanoseconds where the entry has them.

The file is `0600` in the `0700` data directory, like the snapshot and the token file.

### Versioning

Additive changes, such as a new field on an entry or a new `kind`, keep version 1. A reader
ignores unknown fields and skips lines whose `kind` it doesn't know. This is the rule the token
file follows, and it makes a rollback lose only what the older build can't use. A version bump is
for an incompatible change only. A reader that finds a version it doesn't support logs a warning,
starts with an empty history and overwrites the file, as `LoadFromDisk` does with a snapshot. The
v1 promise (`2026-10-24-v1-compatibility-design.md`) gains `history.jsonl` in its on-disk row: the
next 1.x reads what this one writes.

## Writing

A single writer goroutine owns the file. It does not receive entries from `notify`: a channel there
would either block the cache's write path on the disk or drop entries when full. Instead it tails
the ring. Every second it calls `History.Since(lastWritten)`, appends what comes back with one
`write` and one `fsync`, and advances `lastWritten`. `notify` is untouched, and the cost is one read
lock per second.

- **Crash window.** A crash loses at most the last second of entries. They fall inside the gap the
  next start records, so the record stays honest about them.
- **Overrun.** If more than a ring's worth of changes arrive between two flushes, `Since` answers
  `ok=false`. The writer then writes what the ring still holds from its oldest entry on. The ID
  jump is visible in the file, and reading copes with it (below).
- **Write failures** log a warning and are retried at the next tick from the same `lastWritten`.
  Nothing is lost while the ring still holds the entries. A full disk degrades to today's
  behaviour, never to an error a caller sees.

### Compaction

When the file holds more than twice the ring's size in entries, the writer rewrites it: the header,
the newest ring's worth of entries, and the `open`/`close` records at or after the oldest kept
entry. It writes to `history.jsonl.tmp`, fsyncs it, renames it over the file and fsyncs the
directory, exactly like `WriteToDisk`. The file stays under about two rings of entries, and an
entry that leaves the ring is physically gone from disk by the next compaction. That is the bound
on how long an actor's subject is kept.

One file, not segments. At this size (a few MB) rewriting it every 10,000 entries costs less than
the bookkeeping segments need, and a rename is already the pattern every other file in the data
directory relies on.

### Shutdown

Main closes the writer after the watcher has stopped, inside the same shutdown grace, so the
entries the final events produced are flushed before the `close` record is written.

## Reading

At startup, before the watcher starts and so before any `Append`, the file is read line by line:

- **A torn last line** (no trailing newline, or unparsable) is what a crash mid-write leaves. The
  file is truncated to the last complete line before the writer opens it for appending.
- **A bad line elsewhere** is skipped and counted in one warning.
- **IDs must increase strictly.** A line whose ID doesn't is skipped. Jumps are allowed (see
  *Overrun*).
- **The newest ring's worth of entries** are restored, through a new `History.Restore`. It fills
  the ring in order, rebuilds the per-resource index, and sets the counter to the last entry's ID,
  not to the number of entries. New entries continue from there, so IDs never repeat.
- **`Since` reads the oldest ID from the ring** instead of deriving it as `count - size + 1`. That
  derivation assumes IDs run without jumps from 1, which a restored ring does not promise.

The writer then starts with `lastWritten` set to the restored counter, so restored entries are
never written twice.

## Gaps

Changes made while Cetacean was down produce no events. The first full sync absorbs them into the
cache with a single `sync` event, which history doesn't record. So no burst of misleading
"created" entries happens. The opposite problem does: a restored history followed by new entries
reads as if nothing happened in between.

A gap runs from the last moment the previous process is known to have been watching to the next
`open`. That last moment is its `close` record after a clean shutdown, or its last line after a
crash. The second may be well before the crash in a quiet cluster. A gap that is wider than the
real one is the honest error: it claims less coverage, never more. A process that never became
ready writes no `open`, so its lifetime falls inside the gap too.

`History` keeps the gaps that lie inside the ring's span and drops them as the ring moves past
them.

- **MCP.** `get_events` reports `trackingSince` from the oldest entry. After a restore, that claims
  a window with a hole in it. The result gains a `gaps` array of `{from, to}`, which is an
  addition under the v1 rules, and the tool description says what it means.
- **REST.** The JSON history collection gains a `gaps` member of the same `{from, to}` objects,
  with a JSON-LD term and an OpenAPI schema. A gap is listed on the page whose entries it ends
  among: its `to` is after the page's oldest entry and at or before the entry the `before` cursor
  names. The first page has no upper bound, and the last page (no `next` link) has no lower bound.
  That lists every gap exactly once across a full paging, filtered or not. CSV and the feeds carry
  no gaps; they are lists of changes, and a gap is not one.
- **Dashboard.** The history page and every resource's recent changes draw a divider where a gap
  falls: "Cetacean was not running from … to …; changes in this window were not recorded".

## Replay

The broadcaster's epoch stays per process. A `Last-Event-ID` from before a restart is answered
with a `full_sync`, as today. Continuing it would be wrong: replay would answer `Since` with every
entry after the cursor and report the stream complete, but the changes made during the downtime
are in no entry. A client that trusted it would never learn of them. Since a cursor from this
process is always at or above the restored counter, replay never sends a restored entry, and
restoring needs no change to the broadcaster.

## Settings

Persistence follows `storage.snapshot`. History is cluster state like the snapshot, and an operator
who has turned off writing cluster state to disk has turned off this too. No new setting. The
documentation of `storage.snapshot` and `storage.data_dir` names the new file.

The ring's size stays fixed at 10,000. Making it a setting is independent of persistence, since the
ring still has to fit in memory. `Restore` already truncates to the ring's size and keeps the
newest, so a configurable size could be added later without a format change.

## Secrets and privacy

An entry holds IDs, names, an action and an actor: never a resource's spec, so secret data cannot
reach the file. The actor is identity data at rest, which the data directory already holds in the
token file. Compaction bounds how long it is kept: about two rings of entries, after which it is
gone from disk.

## Where it lives

`internal/cache`, beside `snapshot.go`: `History.Restore`, the `Since` change, the gap list, and a
`historyLog` type for the file and the writer. Main wires it next to the snapshot, gated on the same
setting and on `dataDirReady`. `.claude/ARCHITECTURE.md` gets a line on the trap: the writer tails
the ring and must never be fed from `notify`.

## Decided

1. **JSON Lines, one file, compacted by rewrite.** No new dependency. Segments and an embedded
   database cost more than they buy for a ring of this size.
2. **The writer tails the ring every second.** `notify` neither blocks on the disk nor drops
   entries; a crash loses at most a second, inside a recorded gap.
3. **Own record types on disk**, carrying the fields the API hides.
4. **Gaps are recorded and reported** on MCP, on the REST history collection and on the
   dashboard, so a restored history never claims coverage it doesn't have.
5. **The epoch stays per process.** Pre-restart cursors get `full_sync`, because replaying across
   a gap would report a stream complete that isn't.
6. **No new setting.** Persistence rides on `storage.snapshot`, and the ring's size stays fixed.

## Open

1. **Recording what changed during a gap.** Two ways, both left out of this change:
   - *Comparing the restored snapshot with the first sync.* This sees only net differences, with
     no timestamps and no actors, and misses anything created and removed while Cetacean was
     down. Not worth building.
   - *Replaying the daemon's events.* The engine keeps its last 256 events in memory and serves
     them to a subscription with `since`, so starting the first subscription at the gap's start
     would recover real actions with real timestamps. This covers the common case, Cetacean's own
     restart or redeploy. Its limits:
     - The buffer is shared with container events (health checks among them), so on a busy
       manager it spans minutes.
     - It is lost when the daemon restarts.
     - It is per daemon, and whether another manager logged the same swarm events is unverified.
     - Tasks have no swarm events at all, so their changes stay unrecorded.

     The gap would then narrow, never close. A candidate follow-up issue.
