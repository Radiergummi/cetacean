# LogViewer Redesign Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the LogViewer faster, more reliable, more flexible, and easier to maintain.

**Architecture:** Split the 1026-line LogViewer.tsx into 5 focused files. Fix reliability bugs (fetch abort, live batching). Add level filtering, search navigation, URL-persisted time range, and cursor-based pagination for loading older/newer logs.

**Tech Stack:** React 19, TypeScript, @tanstack/react-virtual, react-router-dom useSearchParams, Vitest

---

### Task 1: Extract log-utils.ts

Pure extraction — move types, constants, and utility functions out of LogViewer.tsx.

**Files:**
- Create: `frontend/src/components/log-utils.ts`
- Modify: `frontend/src/components/LogViewer.tsx`

**Step 1: Create `log-utils.ts`**

Move these items from LogViewer.tsx into log-utils.ts:
- `Level` type (line 34)
- `LogLine` interface (lines 29-32) — the frontend-enriched version that extends `ApiLogLine`
- `TimeRange` interface (lines 36-40)
- `LIMIT_OPTIONS` constant (line 42)
- `MAX_LIVE_LINES` constant (line 43)
- `LOG_ROW_HEIGHT_ESTIMATE` constant (line 44)
- `LOG_VIRTUAL_THRESHOLD` constant (line 45)
- `PRESETS` array (lines 47-91)
- `LEVEL_BAR` record (lines 93-99)
- `classifyLevel` function (lines 101-108)
- `LEVEL_KEYS` array (line 110)
- `detectLevelFromJSON` function (lines 112-135)
- `detectLevel` function (lines 137-151)
- `toLogLine` function (lines 153-155)
- `formatTime` function (lines 157-169)
- `isJSON` function (lines 171-174)
- `prettyJSON` function (lines 176-182)
- `toLocalInput` function (lines 184-189)
- `formatShortDate` function (lines 715-719)

Export all of them. In LogViewer.tsx, replace the removed code with imports from `./log-utils`.

**Step 2: Verify build**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: no errors

**Step 3: Run tests**

Run: `cd frontend && npx vitest run src/components/LogViewer.test.tsx`
Expected: all tests pass (no behavior change)

**Step 4: Commit**

```
feat: extract log utility functions into log-utils.ts
```

---

### Task 2: Extract LogMessage.tsx

**Files:**
- Create: `frontend/src/components/LogMessage.tsx`
- Modify: `frontend/src/components/LogViewer.tsx`

**Step 1: Create `LogMessage.tsx`**

Move from LogViewer.tsx:
- `LogMessage` component (lines 964-995)
- `HighlightedText` component (lines 997-1025)

Import `isJSON`, `prettyJSON` from `./log-utils`. Import `Level` type. Export both components.

**Step 2: Update LogViewer.tsx**

Replace removed components with `import { LogMessage, HighlightedText } from "./LogMessage"`.

**Step 3: Verify build and tests**

Run: `cd frontend && npx tsc -b --noEmit && npx vitest run src/components/LogViewer.test.tsx`
Expected: all pass

**Step 4: Commit**

```
refactor: extract LogMessage and HighlightedText components
```

---

### Task 3: Extract LogTable.tsx

**Files:**
- Create: `frontend/src/components/LogTable.tsx`
- Modify: `frontend/src/components/LogViewer.tsx`

**Step 1: Create `LogTable.tsx`**

Move from LogViewer.tsx:
- `LogTableProps` interface (lines 776-784)
- `LogRow` component (lines 786-844)
- `LogTable` component (lines 846-896)
- `VirtualLogBody` component (lines 898-962)

Import from `./log-utils`: `LogLine`, `LEVEL_BAR`, `LOG_ROW_HEIGHT_ESTIMATE`, `LOG_VIRTUAL_THRESHOLD`, `formatTime`.
Import from `./LogMessage`: `LogMessage`.
Import `useVirtualizer` from `@tanstack/react-virtual`.
Import `useState`, `useCallback` from `react`.

Export `LogTable` (default or named) and `LogRow`.

**Step 2: Update LogViewer.tsx**

Replace removed components with import. Remove the `useVirtualizer` import from LogViewer.tsx if no longer needed there.

**Step 3: Verify build and tests**

Run: `cd frontend && npx tsc -b --noEmit && npx vitest run src/components/LogViewer.test.tsx`
Expected: all pass

**Step 4: Commit**

```
refactor: extract LogTable, LogRow, and VirtualLogBody components
```

---

### Task 4: Extract LogToolbar.tsx

**Files:**
- Create: `frontend/src/components/LogToolbar.tsx`
- Modify: `frontend/src/components/LogViewer.tsx`

**Step 1: Create `LogToolbar.tsx`**

Move from LogViewer.tsx:
- `TimeRangeSelector` component (lines 570-713)
- `StreamFilterToggle` component (lines 721-748)
- `STREAM_OPTIONS` constant (line 721)
- `ToolbarButton` component (lines 750-774)

Import `TimeRange`, `PRESETS`, `formatShortDate`, `toLocalInput` from `./log-utils`.
Import icons from `lucide-react`.

Export all three components.

**Step 2: Update LogViewer.tsx**

Replace with imports. LogViewer.tsx should now be ~300 lines: state, effects, toolbar composition, and layout.

**Step 3: Verify build and tests**

Run: `cd frontend && npx tsc -b --noEmit && npx vitest run src/components/LogViewer.test.tsx`
Expected: all pass

**Step 4: Commit**

```
refactor: extract LogToolbar, TimeRangeSelector, and StreamFilterToggle
```

---

### Task 5: Fix fetch abort on unmount

**Files:**
- Modify: `frontend/src/components/LogViewer.tsx`
- Modify: `frontend/src/components/LogViewer.test.tsx`

**Step 1: Write the failing test**

Add to LogViewer.test.tsx:

```typescript
it("aborts in-flight fetch on unmount", async () => {
  let abortSignal: AbortSignal | undefined;
  mockServiceLogs.mockImplementation((_id, opts) => {
    abortSignal = opts?.signal;
    return new Promise(() => {}); // never resolves
  });
  const { unmount } = render(<LogViewer serviceId="svc1" />);

  await waitFor(() => {
    expect(abortSignal).toBeDefined();
  });
  expect(abortSignal!.aborted).toBe(false);

  unmount();
  expect(abortSignal!.aborted).toBe(true);
});
```

**Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/components/LogViewer.test.tsx -t "aborts in-flight fetch on unmount"`
Expected: FAIL — signal is not aborted on unmount

**Step 3: Implement the fix**

In `LogViewer.tsx`, modify `fetchLogs` to store the controller in `abortRef`:

```typescript
const fetchLogs = useCallback(() => {
  abortRef.current?.abort();
  setLoading(true);
  setError(null);
  const controller = new AbortController();
  abortRef.current = controller;
  const timeout = setTimeout(() => controller.abort(), 15_000);
  // ... rest unchanged
}, [logId, isTask, limit, timeRange, streamParam]);
```

Add a cleanup return to the fetch effect:

```typescript
useEffect(() => {
  fetchLogs();
  return () => {
    abortRef.current?.abort();
  };
}, [fetchLogs]);
```

**Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run src/components/LogViewer.test.tsx`
Expected: all pass

**Step 5: Commit**

```
fix: abort in-flight log fetch on unmount
```

---

### Task 6: Batch live SSE updates with requestAnimationFrame

**Files:**
- Modify: `frontend/src/components/LogViewer.tsx`
- Modify: `frontend/src/components/LogViewer.test.tsx`

**Step 1: Write the failing test**

Add to LogViewer.test.tsx:

```typescript
it("batches rapid SSE messages into single state update", async () => {
  mockServiceLogs.mockResolvedValue(
    logResponse([{ message: "initial", timestamp: "2024-01-01T00:00:00Z" }]),
  );
  mockServiceLogsStreamURL.mockReturnValue("/api/services/svc1/logs");
  render(<LogViewer serviceId="svc1" />);

  await waitFor(() => expect(screen.getByText(/initial/)).toBeInTheDocument());

  fireEvent.click(screen.getByTitle("Live tail"));
  const es = MockEventSource.instances[0];

  // Send 5 messages rapidly
  for (let i = 0; i < 5; i++) {
    es.emit(JSON.stringify({ timestamp: `2024-01-01T00:00:0${i + 1}Z`, message: `batch-${i}`, stream: "stdout" }));
  }

  // After rAF flush, all 5 should appear
  await waitFor(() => {
    expect(screen.getByText(/batch-4/)).toBeInTheDocument();
  });
  // Verify all lines rendered
  expect(screen.getByText(/batch-0/)).toBeInTheDocument();
  expect(screen.getByText(/batch-2/)).toBeInTheDocument();
});
```

**Step 2: Run test to verify it passes (it may already pass since the behavior is the same, just batched)**

Run: `cd frontend && npx vitest run src/components/LogViewer.test.tsx -t "batches rapid SSE"`
Expected: might pass already since vitest uses fake timers; that's fine — the test documents the contract

**Step 3: Implement batching**

In the live SSE `useEffect` in LogViewer.tsx, replace the per-message `setLines` with a buffered approach:

```typescript
useEffect(() => {
  if (!live) return;

  const lastTs = lines.length > 0 ? lines[lines.length - 1].timestamp : undefined;
  const after = lastTs || new Date().toISOString();
  const streamOpts = { after, stream: streamParam };
  const url = isTask
    ? api.taskLogsStreamURL(logId, streamOpts)
    : api.serviceLogsStreamURL(logId, streamOpts);

  const es = new EventSource(url);
  abortRef.current = { abort: () => es.close() } as AbortController;
  const buffer: ApiLogLine[] = [];
  let rafId = 0;

  const flush = () => {
    rafId = 0;
    if (buffer.length === 0) return;
    const batch = buffer.splice(0);
    setLines((current) => {
      const next = current.concat(batch.map((l, i) => toLogLine(l, current.length + i)));
      return next.length > MAX_LIVE_LINES ? next.slice(-MAX_LIVE_LINES) : next;
    });
  };

  es.onmessage = (event) => {
    try {
      buffer.push(JSON.parse(event.data));
      if (!rafId) rafId = requestAnimationFrame(flush);
    } catch {
      // skip malformed events
    }
  };

  es.onerror = () => {};

  return () => {
    es.close();
    cancelAnimationFrame(rafId);
    abortRef.current = null;
  };
}, [live, logId, isTask, streamParam]);
```

Note: `lines` is intentionally NOT in the dependency array. The `lastTs` is captured once when live mode starts. The `setLines` functional updater always has the latest state.

**Step 4: Run all tests**

Run: `cd frontend && npx vitest run src/components/LogViewer.test.tsx`
Expected: all pass

**Step 5: Commit**

```
perf: batch live SSE log updates via requestAnimationFrame
```

---

### Task 7: Add level filter dropdown

**Files:**
- Modify: `frontend/src/components/LogViewer.tsx` (add state + pass to filter)
- Modify: `frontend/src/components/LogToolbar.tsx` (add LevelFilter component)
- Modify: `frontend/src/components/log-utils.ts` (export Level type if not already)
- Modify: `frontend/src/components/LogViewer.test.tsx`

**Step 1: Write the failing test**

```typescript
it("filters logs by level", async () => {
  mockServiceLogs.mockResolvedValue(
    logResponse([
      { message: "INFO starting up" },
      { message: "ERROR something broke" },
      { message: "DEBUG verbose stuff" },
    ]),
  );
  render(<LogViewer serviceId="svc1" />);

  await waitFor(() => expect(screen.getByText(/starting up/)).toBeInTheDocument());

  fireEvent.change(screen.getByTitle("Filter by level"), { target: { value: "error" } });

  expect(screen.queryByText(/starting up/)).not.toBeInTheDocument();
  expect(screen.getByText(/something broke/)).toBeInTheDocument();
  expect(screen.queryByText(/verbose stuff/)).not.toBeInTheDocument();
});
```

**Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/components/LogViewer.test.tsx -t "filters logs by level"`
Expected: FAIL — no element with title "Filter by level"

**Step 3: Add LevelFilter to LogToolbar.tsx**

```typescript
export function LevelFilter({
  value,
  onChange,
}: {
  value: Level | "all";
  onChange: (v: Level | "all") => void;
}) {
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value as Level | "all")}
      title="Filter by level"
      className="h-8 px-2 text-xs border rounded-md bg-background"
    >
      <option value="all">All levels</option>
      <option value="error">Error</option>
      <option value="warn">Warn</option>
      <option value="info">Info</option>
      <option value="debug">Debug</option>
    </select>
  );
}
```

**Step 4: Add state and filtering to LogViewer.tsx**

Add state: `const [levelFilter, setLevelFilter] = useState<Level | "all">("all");`

Add `<LevelFilter value={levelFilter} onChange={setLevelFilter} />` to the toolbar, next to `StreamFilterToggle`.

Add level filtering to the `filtered` memo:

```typescript
if (levelFilter !== "all") {
  result = result.filter((l) => l.level === levelFilter);
}
```

**Step 5: Run tests**

Run: `cd frontend && npx vitest run src/components/LogViewer.test.tsx`
Expected: all pass

**Step 6: Commit**

```
feat: add log level filter dropdown
```

---

### Task 8: Add search match navigation

**Files:**
- Modify: `frontend/src/components/LogViewer.tsx`
- Modify: `frontend/src/components/LogTable.tsx`
- Modify: `frontend/src/components/LogViewer.test.tsx`

**Step 1: Write the failing test**

```typescript
it("navigates between search matches with Enter", async () => {
  mockServiceLogs.mockResolvedValue(
    logResponse([
      { message: "foo bar" },
      { message: "baz" },
      { message: "foo qux" },
    ]),
  );
  render(<LogViewer serviceId="svc1" />);

  await waitFor(() => expect(screen.getByText(/foo bar/)).toBeInTheDocument());

  const input = screen.getByPlaceholderText("Filter logs...");
  fireEvent.change(input, { target: { value: "foo" } });

  // Should show match count
  expect(screen.getByText("1/2")).toBeInTheDocument();

  // Press Enter to go to next match
  fireEvent.keyDown(input, { key: "Enter" });
  expect(screen.getByText("2/2")).toBeInTheDocument();

  // Shift+Enter to go back
  fireEvent.keyDown(input, { key: "Enter", shiftKey: true });
  expect(screen.getByText("1/2")).toBeInTheDocument();
});
```

**Step 2: Run test to verify it fails**

Expected: FAIL — shows `2/3` (current format) instead of `1/2`

**Step 3: Implement**

In LogViewer.tsx, add state:

```typescript
const [matchIndex, setMatchIndex] = useState(0);
```

Reset `matchIndex` to 0 whenever `filtered` changes (in a `useEffect` or by resetting in the search onChange).

Compute match display: `{matchIndex + 1}/{filtered.length}` (when search is active and filtered.length > 0).

Update the search input's `onKeyDown`:

```typescript
onKeyDown={(e) => {
  if (e.key === "Escape") { setSearch(""); return; }
  if (e.key === "Enter" && search && filtered.length > 0) {
    e.preventDefault();
    if (e.shiftKey) {
      setMatchIndex((i) => (i - 1 + filtered.length) % filtered.length);
    } else {
      setMatchIndex((i) => (i + 1) % filtered.length);
    }
  }
}}
```

Pass `matchIndex` to `LogTable`. In LogTable/VirtualLogBody, when `matchIndex` changes, scroll the virtualizer to that row index using `virtualizer.scrollToIndex(matchIndex)`. For the non-virtual path, use `scrollIntoView` on the matching row ref.

Highlight the current match row with a subtle background: add a `highlightIndex` prop to `LogRow`, and apply `bg-yellow-500/10` when `line.index === highlightIndex`.

**Step 4: Run all tests**

Run: `cd frontend && npx vitest run src/components/LogViewer.test.tsx`
Expected: all pass

**Step 5: Commit**

```
feat: add search match navigation with Enter/Shift+Enter
```

---

### Task 9: URL-persist time range

**Files:**
- Modify: `frontend/src/components/LogViewer.tsx`
- Modify: `frontend/src/components/LogViewer.test.tsx`

**Step 1: Write the failing test**

The test needs `MemoryRouter` wrapping since LogViewer currently doesn't use router hooks. All existing tests will need wrapping too — but check first if they already have one. If not, add a wrapper helper.

```typescript
import { MemoryRouter } from "react-router-dom";

function renderWithRouter(ui: React.ReactElement, initialEntries = ["/"]) {
  return render(<MemoryRouter initialEntries={initialEntries}>{ui}</MemoryRouter>);
}

it("reads time range from URL on mount", async () => {
  mockServiceLogs.mockResolvedValue(logResponse([{ message: "line" }]));
  renderWithRouter(<LogViewer serviceId="svc1" />, ["/?logRange=5m"]);

  await waitFor(() => {
    expect(mockServiceLogs).toHaveBeenCalledWith(
      "svc1",
      expect.objectContaining({ after: expect.any(String) }),
    );
  });
});
```

Note: All existing tests need to be updated to use `renderWithRouter` instead of bare `render`, since LogViewer will now call `useSearchParams`.

**Step 2: Run test to verify it fails**

Expected: FAIL — `useSearchParams` called outside Router context

**Step 3: Implement**

In LogViewer.tsx, add `useSearchParams` from react-router-dom. On mount, read `logRange` param. Map preset labels to duration strings: `{ "5m": "Last 5m", "15m": "Last 15m", "1h": "Last 1h", "6h": "Last 6h", "24h": "Last 24h", "7d": "Last 7d" }`. For custom ranges, read `logSince` and `logUntil`.

When time range changes (via `setTimeRange`), update URL params:
- Preset: set `logRange=5m`, delete `logSince`/`logUntil`
- Custom: set `logSince` and/or `logUntil`, delete `logRange`
- "All": delete all three

Use `setParams` with `{ replace: true }` to avoid polluting browser history.

Add a `RANGE_TO_DURATION` map in `log-utils.ts`:
```typescript
export const RANGE_DURATIONS: Record<string, string> = {
  "5m": "Last 5m", "15m": "Last 15m", "1h": "Last 1h",
  "6h": "Last 6h", "24h": "Last 24h", "7d": "Last 7d",
};
```

**Step 4: Update all existing tests to use `renderWithRouter`**

Wrap every `render(<LogViewer ... />)` with `renderWithRouter`.

**Step 5: Run all tests**

Run: `cd frontend && npx vitest run src/components/LogViewer.test.tsx`
Expected: all pass

**Step 6: Commit**

```
feat: persist log time range in URL params
```

---

### Task 10: Cursor-based pagination — load older logs

**Files:**
- Modify: `frontend/src/components/LogViewer.tsx`
- Modify: `frontend/src/components/LogTable.tsx`
- Modify: `frontend/src/components/LogViewer.test.tsx`

**Step 1: Write the failing test**

```typescript
it("loads older logs when scrolling to top", async () => {
  const initialLines = logResponse([
    { message: "line 1", timestamp: "2024-01-01T00:00:05Z" },
    { message: "line 2", timestamp: "2024-01-01T00:00:06Z" },
  ]);
  mockServiceLogs
    .mockResolvedValueOnce(initialLines)
    .mockResolvedValueOnce(
      logResponse([
        { message: "older line", timestamp: "2024-01-01T00:00:01Z" },
      ]),
    );

  renderWithRouter(<LogViewer serviceId="svc1" />);

  await waitFor(() => expect(screen.getByText("line 1")).toBeInTheDocument());

  // Simulate scroll to top
  const container = screen.getByText("line 1").closest(".log-panel")!;
  Object.defineProperty(container, "scrollTop", { value: 0, writable: true });
  fireEvent.scroll(container);

  await waitFor(() => {
    expect(mockServiceLogs).toHaveBeenCalledWith(
      "svc1",
      expect.objectContaining({ before: "2024-01-01T00:00:05Z" }),
    );
  });

  await waitFor(() => {
    expect(screen.getByText("older line")).toBeInTheDocument();
  });
});
```

**Step 2: Run test to verify it fails**

Expected: FAIL — no second fetch triggered on scroll

**Step 3: Implement**

In LogViewer.tsx, add state:

```typescript
const [loadingOlder, setLoadingOlder] = useState(false);
const [hasOlderLogs, setHasOlderLogs] = useState(true);
const oldestRef = useRef<string | undefined>();
```

Track `oldest` from each fetch response:

```typescript
.then((resp) => {
  const newLines = (resp.lines ?? []).map(toLogLine);
  setLines(newLines);
  oldestRef.current = resp.oldest;
  setHasOlderLogs(newLines.length >= limit);
  setLoading(false);
})
```

Add a `loadOlder` callback:

```typescript
const loadOlder = useCallback(() => {
  if (loadingOlder || !hasOlderLogs || !oldestRef.current) return;
  setLoadingOlder(true);
  const opts = { limit, before: oldestRef.current, stream: streamParam };
  const req = isTask ? api.taskLogs(logId, opts) : api.serviceLogs(logId, opts);
  req.then((resp) => {
    const older = (resp.lines ?? []).map((l, i) => toLogLine(l, i));
    if (older.length === 0) {
      setHasOlderLogs(false);
    } else {
      oldestRef.current = resp.oldest;
      setLines((current) => {
        // Re-index all lines
        const combined = [...older, ...current];
        return combined.map((l, i) => ({ ...l, index: i }));
      });
    }
    setLoadingOlder(false);
  }).catch(() => setLoadingOlder(false));
}, [loadingOlder, hasOlderLogs, limit, streamParam, isTask, logId]);
```

Modify `handleScroll` to detect scroll-to-top:

```typescript
const handleScroll = useCallback(() => {
  const el = containerRef.current;
  if (!el) return;
  const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 50;
  setFollowing(atBottom);
  if (el.scrollTop < 100 && !loadingOlder) {
    loadOlder();
  }
}, [loadOlder, loadingOlder]);
```

Pass `loadingOlder` and `hasOlderLogs` to `LogTable`. In LogTable, render a status row at the very top of the table body:

```typescript
{loadingOlder && (
  <tr><td colSpan={colCount} className="text-center py-2 text-xs text-muted-foreground">
    <Loader2 className="w-3 h-3 animate-spin inline mr-1" /> Loading older logs...
  </td></tr>
)}
{!hasOlderLogs && (
  <tr><td colSpan={colCount} className="text-center py-2 text-xs text-muted-foreground">
    Beginning of logs
  </td></tr>
)}
```

For scroll position preservation when prepending: measure `scrollHeight` before the state update, and after React commits, set `scrollTop += (newScrollHeight - oldScrollHeight)`. Use `useLayoutEffect` or a ref-based approach with `requestAnimationFrame`:

```typescript
// In LogTable or LogViewer, before prepend:
const prevScrollHeight = containerRef.current?.scrollHeight ?? 0;

// After setLines with prepend, in a useEffect or rAF:
requestAnimationFrame(() => {
  if (containerRef.current) {
    const delta = containerRef.current.scrollHeight - prevScrollHeight;
    containerRef.current.scrollTop += delta;
  }
});
```

The cleanest approach: store `prevScrollHeight` in a ref, set it before `loadOlder` triggers `setLines`, and adjust in a `useLayoutEffect` that depends on `lines.length`.

**Step 4: Run all tests**

Run: `cd frontend && npx vitest run src/components/LogViewer.test.tsx`
Expected: all pass

**Step 5: Commit**

```
feat: load older logs on scroll-to-top with cursor pagination
```

---

### Task 11: Load newer logs (non-live mode)

**Files:**
- Modify: `frontend/src/components/LogViewer.tsx`
- Modify: `frontend/src/components/LogViewer.test.tsx`

**Step 1: Write the failing test**

```typescript
it("loads newer logs when scrolling to bottom in non-live mode", async () => {
  const initialLines = logResponse([
    { message: "line 1", timestamp: "2024-01-01T00:00:01Z" },
    { message: "line 2", timestamp: "2024-01-01T00:00:02Z" },
  ]);
  mockServiceLogs
    .mockResolvedValueOnce(initialLines)
    .mockResolvedValueOnce(
      logResponse([
        { message: "newer line", timestamp: "2024-01-01T00:00:03Z" },
      ]),
    );

  renderWithRouter(<LogViewer serviceId="svc1" />);

  await waitFor(() => expect(screen.getByText("line 2")).toBeInTheDocument());

  // Simulate scroll to bottom
  const container = screen.getByText("line 1").closest(".log-panel")!;
  Object.defineProperty(container, "scrollTop", { value: 1000, writable: true });
  Object.defineProperty(container, "scrollHeight", { value: 1050, writable: true });
  Object.defineProperty(container, "clientHeight", { value: 1000, writable: true });
  fireEvent.scroll(container);

  await waitFor(() => {
    expect(mockServiceLogs).toHaveBeenCalledWith(
      "svc1",
      expect.objectContaining({ after: "2024-01-01T00:00:02Z" }),
    );
  });

  await waitFor(() => {
    expect(screen.getByText("newer line")).toBeInTheDocument();
  });
});
```

**Step 2: Run test to verify it fails**

Expected: FAIL — no fetch with `after` triggered

**Step 3: Implement**

Similar to loadOlder but in the opposite direction. Add:

```typescript
const [loadingNewer, setLoadingNewer] = useState(false);
const newestRef = useRef<string | undefined>();
```

Track `newest` from fetch responses. Add `loadNewer` callback that fetches with `?after={newest}` and appends + re-indexes.

In `handleScroll`, when at bottom AND not live:

```typescript
if (atBottom && !live && !loadingNewer) {
  loadNewer();
}
```

Skip load-newer when live mode is on (SSE handles it).

**Step 4: Run all tests**

Run: `cd frontend && npx vitest run src/components/LogViewer.test.tsx`
Expected: all pass

**Step 5: Commit**

```
feat: load newer logs on scroll-to-bottom in non-live mode
```

---

### Task 12: Final integration test and lint

**Files:**
- All modified files

**Step 1: Run full test suite**

Run: `cd frontend && npx vitest run`
Expected: all tests pass

**Step 2: Run lint and format**

Run: `cd frontend && npm run lint && npm run fmt:check`
Expected: clean

**Step 3: Run type check**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: no errors

**Step 4: Commit any lint fixes**

```
chore: lint and format fixes
```
