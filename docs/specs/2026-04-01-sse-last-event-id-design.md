# SSE Last-Event-ID Replay

## Problem

When a browser's SSE connection drops and reconnects, all events that fired during the disconnect window are silently lost. The client has no way to know what changed while it was offline. Currently, the only recovery mechanism is the periodic 5-minute Docker resync (`EventSync`), which triggers a full refetch on all connected clients. Brief disconnects (seconds to minutes) leave the UI stale until the next sync.

The SSE spec defines a `Last-Event-ID` mechanism for exactly this: the browser sends the last received event ID on reconnect, and the server replays missed events. The infrastructure is mostly in place — the history ring buffer already assigns global monotonic IDs — but the SSE `id:` field currently uses a meaningless per-connection counter.

## Design

### Wire format change

The SSE `id:` field switches from a per-connection counter to the global `HistoryEntry.ID`:

- **Normal event batches**: `id:` = highest `HistoryEntry.ID` in the batch.
- **Sync events** (not recorded in history): `id:` = current `History.Count()` value (the next ID that would be assigned). This means "I've seen everything up to here."
- **Mixed batches** (real events + sync in the same batch window): use the sync semantics (history counter), since a sync supersedes incremental events.

To make history IDs available at write time, `cache.Event` gains a `HistoryID uint64` field, populated by `cache.notify()` after `history.Append()`. For sync events (which skip history), `HistoryID` is set to `history.Count()`.

### History API addition

Two new methods on `History`:

```go
func (h *History) Since(afterID uint64) (entries []HistoryEntry, ok bool)
```

- Returns all entries with `ID > afterID` in chronological order.
- `ok == false` if `afterID` has been overwritten (`afterID < h.count - ringSize`) or is a future ID (`afterID > h.count`). Caller cannot trust completeness.
- `ok == true` with empty slice if `afterID == h.count` (fully caught up).
- Acquires the read lock internally, returns a copy.

```go
func (h *History) Count() uint64
```

Returns the ID of the most recently appended entry (0 if empty).

No changes to the existing `List` / `HistoryQuery` API.

### Replay logic in ServeSSE

On connection, before entering the normal event loop:

1. **Parse `Last-Event-ID`** from the request header as `uint64`. If absent or unparseable, skip replay (fresh connection, behave as today).

2. **Determine replay eligibility.** `ServeSSE` gains an additional `replayType cache.EventType` parameter (zero value means replay-ineligible). Callers that register type-level streams (`TypeMatcher`) pass the event type; callers that register detail-level or stack streams pass zero. Only connections with a non-zero `replayType` attempt replay — the rest fall back to sync. This keeps the eligibility decision at the call site (where the matcher is constructed) rather than introspecting the matcher function.

3. **Subscribe to live events first** — register the `sseClient` with the broadcaster so no events are missed from this point forward.

4. **Query `history.Since(lastEventID)`:**
   - `ok == false`: ID is too old or invalid. Send a single sync event with `id:` set to `history.Count()`. Client refetches. Done.
   - `ok == true`: Proceed to step 5.

5. **Filter and send replay entries.** Filter history entries by `entry.Type` matching the stream's type. Construct lightweight `cache.Event` values from `HistoryEntry` fields (type, action, resource ID, name). These events carry no `Resource` payload — they're change notifications only. Send as a batch with `id:` set to the highest replayed history ID.

6. **Drain live channel with deduplication.** Events buffered in the live channel during steps 4-5 may overlap with replayed entries. Drain them and skip any with `HistoryID <= lastReplayedID`.

7. **Enter normal event loop.**

### Replay events without resource payload

Replayed events are reconstructed from `HistoryEntry`, which stores type, action, resource ID, and name — but not the full resource. This means replayed events have no `Resource` field.

This is acceptable because:

- **Detail pages** (`useDetailResource`): already refetch on every event regardless of payload.
- **List pages** (`useSwarmResource`): the optimistic update path currently expects a resource payload for upserts. When the payload is absent, the handler falls back to a full list refetch. This fallback needs to be verified and added if missing.

### Frontend changes

Minimal. The browser's native `EventSource` sends `Last-Event-ID` automatically on reconnect — no application code needed.

The only change: verify that `useSwarmResource`'s SSE event handler gracefully handles events without a `Resource` field by falling back to a full refetch. Add this fallback if missing.

### Out of scope

- **Metrics stream** (`metricsstream.go`): no `id:` fields, no replay. The initial range query on connect covers the full window.
- **Log stream** (`log_handlers.go`): already has its own `Last-Event-ID` implementation using timestamps.
- **Configurable ring size**: stays at 10,000 entries.
- **Precise cross-resource replay**: detail and stack streams get `sync` on reconnect, not filtered replay.

## Testing

- **`History.Since`**: unit tests for chronological order, `ok == false` on overwritten IDs, `ok == false` on future IDs, `ok == true` with empty result when caught up, correct filtering after ring wraps.
- **`History.Count`**: returns 0 when empty, correct value after appends.
- **Wire format**: verify SSE frames carry `HistoryEntry.ID` not local counter. Verify sync events carry `history.Count()`.
- **Replay flow**: integration test with a mock broadcaster — connect, receive events, disconnect, reconnect with `Last-Event-ID`, verify replayed events match what was missed.
- **Deduplication**: verify no duplicate events when replay and live stream overlap.
- **Fallback to sync**: verify detail-level streams send sync on reconnect. Verify type-level streams send sync when ID is too old.
- **Frontend**: verify `useSwarmResource` handles events without resource payload (refetch fallback).
