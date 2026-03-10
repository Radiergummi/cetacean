# LogViewer Redesign

## Goal

Make the LogViewer faster, more reliable, more flexible, and easier to maintain. Fix known reliability issues, add the highest-value UX features, restructure the component for maintainability, and add cursor-based pagination for scrolling back through history.

## Scope

Frontend only — no backend changes needed. The existing `?after=`, `?before=`, `?limit=`, `?stream=` params and SSE streaming already support everything below.

---

## Part 1: Reliability & Performance

### 1a. Abort fetch on unmount

Store the `AbortController` from `fetchLogs` in `abortRef`. Add a cleanup function to the fetch `useEffect` that calls `abort()`. Prevents dangling 15s connections when navigating away.

### 1b. Batch live SSE updates

Buffer incoming SSE lines in a `ref` array. Flush to state via `requestAnimationFrame` — one `setLines` call per frame instead of one per message. Reduces renders from hundreds/sec to ~60/sec and replaces N array copies with one per frame. Apply the 10k line cap during the flush.

### 1c. Stable array construction

During the rAF flush, concat the entire buffer in one operation rather than spreading per-line.

---

## Part 2: UX Features

### 2a. Level filter dropdown

A `<select>` dropdown in the toolbar with options: All levels, Error, Warn, Info, Debug. Filters within the existing `filtered` memo alongside stream filter and search. Placed next to the stream toggle.

### 2b. Search match navigation

When search is active, show a match count indicator (`3/47 matches`) replacing the current `{filtered.length}/{lines.length}` text. Add up/down arrow buttons (and Enter/Shift+Enter keyboard shortcuts) to jump between matching rows. Implementation: track `currentMatchIndex` in state, scroll the virtualizer to the target row on navigation.

### 2c. Keyboard shortcuts

- `Enter` / `Shift+Enter` in search input: next/prev match
- `Escape` in search input: clear search and blur

### 2d. URL-persisted time range

Follow the MetricsPanel pattern. Store time range in URL search params:
- Presets: `?logRange=5m`, `?logRange=1h`, etc.
- Custom ranges: `?logSince=<ISO>&logUntil=<ISO>`
- On mount, read from URL. On change, update URL via `useSearchParams`.

---

## Part 3: Cursor-based pagination

### Load older logs

When the user scrolls near the top of the log table (within 100px), trigger a fetch with `?before={oldest}&limit=500`. Prepend results to existing lines. Use an intersection observer or scroll position check — no explicit "load more" button.

Show a "Loading older logs..." spinner row at the top while fetching. Show "Beginning of logs" when a fetch returns empty.

### Scroll position preservation

Measure `scrollHeight` before the state update, then after React commits the prepend, adjust `scrollTop` by the delta (`newScrollHeight - oldScrollHeight`). This keeps the user's viewport stable while content is inserted above.

### Deduplication

Skip prepended lines whose timestamp matches an existing line at the boundary. The `oldest` timestamp from the current set becomes the `before` cursor for the next fetch.

### Load newer (non-live only)

When live mode is off and the user scrolls to the bottom, fetch with `?after={newest}&limit=500` and append. When live mode is on, SSE handles new content — no pagination needed at the bottom.

---

## Part 4: Component restructuring

Split the 1026-line `LogViewer.tsx` into:

| File | Contents | ~Lines |
|------|----------|--------|
| `LogViewer.tsx` | Main component: state, effects, pagination logic, layout | 350 |
| `LogTable.tsx` | LogTable, VirtualLogBody, LogRow | 200 |
| `LogToolbar.tsx` | Toolbar, TimeRangeSelector, StreamFilterToggle, LevelFilter, ToolbarButton | 250 |
| `LogMessage.tsx` | LogMessage, HighlightedText | 80 |
| `log-utils.ts` | detectLevel, formatTime, isJSON, prettyJSON, constants, types | 100 |

All files under `frontend/src/components/`. Exports are internal — only `LogViewer.tsx` is the public entry point.

---

## Implementation order

1. Restructure into separate files (no behavior changes — pure move)
2. Reliability fixes (abort on unmount, batch live updates)
3. Level filter dropdown
4. Search match navigation + keyboard shortcuts
5. URL-persisted time range
6. Cursor-based pagination (load older + load newer)

Each step is independently shippable and testable.
