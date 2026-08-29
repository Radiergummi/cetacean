# SSE Cleanup Remediation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the four cleanup findings deferred during the `/simplify` review of PR #148, on the same branch.

**Architecture:** Three small frontend extractions plus one Go package move. The frontend work shares the backoff *formula* while keeping each stream's *policy values* separate, stops background tabs retrying into a saturated server, and moves an untested data transform into the module that already owns its siblings. The Go work moves Docker log parsing into a new `internal/logs` package so the `since` workaround has one home instead of three, and fixes MCP cursor paging, which is currently broken.

**Tech Stack:** TypeScript / React 19 / Vitest; Go 1.26 / stdlib testing.

**Spec:** No separate design doc. This plan implements findings from the `/simplify` review of PR #148, restated in full under "Findings" below so the plan argues from them.

## Global Constraints

- Branch: `fix/log-viewer-reconnect-backoff`. All work lands here, one commit per task.
- TDD is mandatory: write the failing test, watch it fail for the right reason, then implement.
- Frontend style (CLAUDE.md): no abbreviations in identifiers, brace single-statement `if` bodies, blank lines around logical blocks, camelCase module constants, multi-line JSDoc.
- Verification gate for every task: `cd frontend && npx tsc -b --noEmit && npm run lint && npm run fmt:check && npx vitest run` for frontend tasks; `make check` for Go tasks.
- Current baselines to preserve: **581 frontend tests**, full Go suite green, 11 CI checks green.
- `CHANGELOG.md` gets an entry only for Task 5 (the sole user-visible fix). Tasks 1–4 are internal.

## Findings

1. **Duplicated backoff formula.** `lib/eventStream.ts` and `components/log/logStream.ts` both compute `Math.min(base * 2 ** (attempt - 1), max)`. Reviewers split: two argued the two *policies* must stay independent, one argued the *formula* should be shared. Both are right — share the function, keep the constants local. The policies have since diverged (5s base vs 1s base), which makes the parameterised form the honest one.
2. **Background tabs retry forever.** Before PR #148 a permanently-failed stream cost the server nothing afterwards. Now every one retries until the tab closes, including hidden tabs, holding broadcaster slots (256 cap) for a UI nobody is looking at.
3. **Metrics point-merge is inline and untested.** ~25 lines of pure transform live in a listener closure in `TimeSeriesChart.tsx`. `lib/metricsParser.ts` already owns `parseRangeResult`, `seriesLabel`, `seriesChanged`, `normalizePrometheusRows`, each with tests. This is the only member of that family in a component, and consequently the only untested one.
4. **The Docker `since` workaround has three callers and two implementations.** Docker ignores `since` for service logs. `internal/api/log_handlers.go` works around it twice (paginated path, SSE path). `internal/mcp/logs.go` does not work around it at all — it passes `since` to Docker, filters only by level, and then returns a cursor. `docs/api.md` promises MCP clients that passing the cursor back as `since` yields only newer lines. That promise is currently false: every call returns the same newest N lines.

---

### Task 1: Shared backoff formula

**Files:**
- Create: `frontend/src/lib/backoff.ts`
- Create: `frontend/src/lib/backoff.test.ts`
- Modify: `frontend/src/lib/eventStream.ts` (constants block and the `setTimeout` delay expression)
- Modify: `frontend/src/components/log/logStream.ts` (constants block and the delay expression in `streamLogsWithRetry`)

**Interfaces:**
- Consumes: nothing.
- Produces: `backoffDelay(attempt: number, options: BackoffOptions): number` and `interface BackoffOptions { base: number; max: number }`, both exported from `@/lib/backoff`. `attempt` is 1-based.

- [ ] **Step 1: Write the failing test**

Create `frontend/src/lib/backoff.test.ts`:

```typescript
import { backoffDelay } from "./backoff";
import { describe, expect, it } from "vitest";

const options = { base: 1000, max: 30_000 };

describe("backoffDelay", () => {
  it("waits the base delay before the first retry", () => {
    expect(backoffDelay(1, options)).toBe(1000);
  });

  it("doubles on each subsequent attempt", () => {
    expect([2, 3, 4, 5].map((attempt) => backoffDelay(attempt, options))).toEqual([
      2000, 4000, 8000, 16_000,
    ]);
  });

  it("never exceeds the ceiling", () => {
    expect(backoffDelay(20, options)).toBe(30_000);
  });

  it("treats a first attempt below one as the first attempt", () => {
    expect(backoffDelay(0, options)).toBe(1000);
  });

  it("keeps each caller's own policy values", () => {
    expect(backoffDelay(1, { base: 5000, max: 30_000 })).toBe(5000);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/lib/backoff.test.ts`
Expected: FAIL — `Failed to resolve import "./backoff"`.

- [ ] **Step 3: Write minimal implementation**

Create `frontend/src/lib/backoff.ts`:

```typescript
export interface BackoffOptions {
  /** Delay before the first retry, in milliseconds. */
  base: number;
  /** Ceiling the doubling never exceeds, in milliseconds. */
  max: number;
}

/**
 * Exponential backoff: the first retry waits `base`, each subsequent one
 * doubles, and none waits longer than `max`.
 *
 * The schedule is shared; the values are not. Each stream passes its own
 * `base` and `max` because the policies genuinely differ — the log tail
 * starts fast because a person is waiting, background streams start at the
 * interval the server asks for in its Retry-After.
 *
 * @param attempt 1-based index of the retry about to be made.
 */
export function backoffDelay(attempt: number, { base, max }: BackoffOptions): number {
  return Math.min(base * 2 ** (Math.max(1, attempt) - 1), max);
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run src/lib/backoff.test.ts`
Expected: PASS, 5 tests.

- [ ] **Step 5: Call it from `lib/eventStream.ts`**

Add the import beside the existing ones:

```typescript
import { backoffDelay } from "./backoff";
```

Replace the `retryTimer` assignment inside `source.onerror`:

```typescript
      retryTimer = setTimeout(
        connect,
        backoffDelay(attempt, { base: baseReconnectDelay, max: maxReconnectDelay }),
      );
```

Leave `baseReconnectDelay` and `maxReconnectDelay` where they are — they are this stream's policy and the comment above them explains the 5s choice.

- [ ] **Step 6: Call it from `components/log/logStream.ts`**

Add the import:

```typescript
import { backoffDelay } from "../../lib/backoff";
```

Replace the delay expression in `streamLogsWithRetry`:

```typescript
    const delay =
      outcome.failure.retryAfterMilliseconds ??
      backoffDelay(attempt, { base: baseReconnectDelay, max: maxReconnectDelay });
```

- [ ] **Step 7: Verify nothing regressed**

Run: `cd frontend && npx tsc -b --noEmit && npx vitest run`
Expected: PASS, 586 tests (581 + 5 new).

- [ ] **Step 8: Confirm the tests still bite**

Run: temporarily change `Math.max(1, attempt) - 1` to `attempt` in `backoff.ts`, then `npx vitest run src/lib/backoff.test.ts src/lib/eventStream.test.ts src/components/log/logStream.test.ts`
Expected: FAIL in all three files. Revert the change and confirm they pass again.

- [ ] **Step 9: Commit**

```bash
cd /Users/moritz/GolandProjects/cetacean
git add frontend/src/lib/backoff.ts frontend/src/lib/backoff.test.ts \
        frontend/src/lib/eventStream.ts frontend/src/components/log/logStream.ts
git commit -m "refactor(frontend): share the backoff schedule, keep the policies apart"
```

---

### Task 2: Stop background tabs retrying into a saturated server

**Files:**
- Modify: `frontend/src/lib/eventStream.ts`
- Modify: `frontend/src/lib/eventStream.test.ts`

**Interfaces:**
- Consumes: `backoffDelay` from Task 1.
- Produces: no signature change. `openEventStream` gains internal behaviour: while `document.visibilityState === "hidden"` it defers a pending reconnect until the tab is next visible.

**Scope note:** this defers *reconnects only*. An already-open stream in a hidden tab stays open — closing it would strand the tab's cache and force a resync on return, which is a different change.

- [ ] **Step 1: Write the failing tests**

Add to `frontend/src/lib/eventStream.test.ts`, inside the existing `describe("openEventStream", …)`:

```typescript
  /** Drives document.visibilityState, which jsdom leaves read-only. */
  function setVisibility(state: "visible" | "hidden") {
    Object.defineProperty(document, "visibilityState", {
      value: state,
      configurable: true,
    });
    document.dispatchEvent(new Event("visibilitychange"));
  }

  it("does not reconnect while the tab is hidden", async () => {
    setVisibility("hidden");
    const handle = openEventStream("/events", { listeners: {} });

    MockEventSource.instance.simulateError(true);
    await vi.advanceTimersByTimeAsync(120_000);

    expect(MockEventSource.instances).toHaveLength(1);
    handle.close();
    setVisibility("visible");
  });

  it("reconnects as soon as a hidden tab is shown again", async () => {
    setVisibility("hidden");
    const handle = openEventStream("/events", { listeners: {} });

    MockEventSource.instance.simulateError(true);
    await vi.advanceTimersByTimeAsync(120_000);
    expect(MockEventSource.instances).toHaveLength(1);

    setVisibility("visible");
    await vi.advanceTimersByTimeAsync(0);

    expect(MockEventSource.instances).toHaveLength(2);
    handle.close();
  });

  it("leaves no visibility listener behind after close", async () => {
    setVisibility("hidden");
    const handle = openEventStream("/events", { listeners: {} });

    MockEventSource.instance.simulateError(true);
    handle.close();
    setVisibility("visible");
    await vi.advanceTimersByTimeAsync(120_000);

    expect(MockEventSource.instances).toHaveLength(1);
  });
```

Add `setVisibility("visible")` as the first line of the existing `beforeEach` in the `describe`, so a test that leaves the tab hidden cannot bleed into the next one.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd frontend && npx vitest run src/lib/eventStream.test.ts`
Expected: FAIL — "does not reconnect while the tab is hidden" gets 2 instances, not 1.

- [ ] **Step 3: Implement the deferral**

In `frontend/src/lib/eventStream.ts`, add to the local state at the top of `openEventStream`:

```typescript
  let awaitingVisibility = false;
```

Add this above `connect`:

```typescript
  // A hidden tab retrying into a capped server holds a connection slot for a
  // view nobody is looking at. Wait for the tab to come back instead, then
  // reconnect at once — the person is looking now.
  const reconnectWhenVisible = () => {
    if (document.visibilityState !== "visible" || closed) {
      return;
    }

    document.removeEventListener("visibilitychange", reconnectWhenVisible);
    awaitingVisibility = false;
    connect();
  };
```

Replace the scheduling block at the end of `source.onerror`:

```typescript
      source.close();
      attempt += 1;

      if (document.visibilityState === "hidden") {
        awaitingVisibility = true;
        document.addEventListener("visibilitychange", reconnectWhenVisible);

        return;
      }

      retryTimer = setTimeout(
        connect,
        backoffDelay(attempt, { base: baseReconnectDelay, max: maxReconnectDelay }),
      );
```

Extend `close` to drop the listener:

```typescript
    close: () => {
      closed = true;
      clearTimeout(retryTimer);

      if (awaitingVisibility) {
        document.removeEventListener("visibilitychange", reconnectWhenVisible);
        awaitingVisibility = false;
      }

      eventSource?.close();
    },
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd frontend && npx vitest run src/lib/eventStream.test.ts`
Expected: PASS, 14 tests.

- [ ] **Step 5: Verify nothing regressed**

Run: `cd frontend && npx tsc -b --noEmit && npm run lint && npx vitest run`
Expected: PASS, 589 tests.

- [ ] **Step 6: Commit**

```bash
cd /Users/moritz/GolandProjects/cetacean
git add frontend/src/lib/eventStream.ts frontend/src/lib/eventStream.test.ts
git commit -m "perf(frontend): defer stream reconnects while the tab is hidden"
```

---

### Task 3: Extract the metrics point-merge

**Files:**
- Modify: `frontend/src/lib/metricsParser.ts` (append the new export)
- Modify: `frontend/src/lib/metricsParser.test.ts` (append tests)
- Modify: `frontend/src/components/metrics/TimeSeriesChart.tsx` (`handlePoint`)

**Interfaces:**
- Consumes: `ParsedMetrics`, `seriesLabel` — both already exported from `lib/metricsParser.ts`.
- Produces: `appendMetricPoint(previous: ParsedMetrics, response: PrometheusResponse): ParsedMetrics | null`, exported from `@/lib/metricsParser`. Returns `null` when the response carries no usable sample, meaning the caller should keep what it has.

- [ ] **Step 1: Write the failing tests**

Append to `frontend/src/lib/metricsParser.test.ts` (add `appendMetricPoint` to the existing import from `./metricsParser`):

```typescript
describe("appendMetricPoint", () => {
  const previous = {
    labels: ["10:00:00", "10:00:15"],
    timestamps: [1000, 1015],
    series: [
      { label: "web", color: "#111", data: [1, 2] },
      { label: "api", color: "#222", data: [3, 4] },
    ],
  };

  function pointResponse(samples: [string, string][]) {
    return {
      data: {
        resultType: "vector",
        result: samples.map(([job, value]) => ({
          metric: { job },
          value: [1030, value] as [number, string],
        })),
      },
    } as unknown as PrometheusResponse;
  }

  it("drops the oldest sample and appends the newest", () => {
    const next = appendMetricPoint(previous, pointResponse([["web", "9"]]));

    expect(next?.timestamps).toEqual([1015, 1030]);
    expect(next?.series[0]?.data).toEqual([2, 9]);
  });

  it("keeps each series matched to its own label", () => {
    const next = appendMetricPoint(previous, pointResponse([["api", "8"], ["web", "7"]]));

    expect(next?.series[0]?.data).toEqual([2, 7]);
    expect(next?.series[1]?.data).toEqual([4, 8]);
  });

  it("appends a zero for a series the response omits", () => {
    const next = appendMetricPoint(previous, pointResponse([["web", "9"]]));

    expect(next?.series[1]?.data).toEqual([4, 0]);
  });

  it("preserves series colour and label", () => {
    const next = appendMetricPoint(previous, pointResponse([["web", "9"]]));

    expect(next?.series[0]).toMatchObject({ label: "web", color: "#111" });
  });

  it("returns null when the response carries no samples", () => {
    expect(appendMetricPoint(previous, pointResponse([]))).toBeNull();
  });

  it("does not mutate the previous window", () => {
    appendMetricPoint(previous, pointResponse([["web", "9"]]));

    expect(previous.timestamps).toEqual([1000, 1015]);
    expect(previous.series[0]?.data).toEqual([1, 2]);
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd frontend && npx vitest run src/lib/metricsParser.test.ts`
Expected: FAIL — `appendMetricPoint is not a function`.

- [ ] **Step 3: Implement it**

Append to `frontend/src/lib/metricsParser.ts`:

```typescript
/**
 * Advances a fixed-width window by one sample: drops the oldest point and
 * appends the newest, matching each series by its label. A series the
 * response omits gets a zero so every series stays the same length.
 *
 * Returns null when the response carries nothing usable, meaning the caller
 * should keep the window it already has.
 */
export function appendMetricPoint(
  previous: ParsedMetrics,
  response: PrometheusResponse,
): ParsedMetrics | null {
  const result = response.data?.result?.filter(({ value }) => value);

  if (!result?.length) {
    return null;
  }

  const timestamp = Number(result[0]?.value?.[0]);
  const timeLabel = new Date(timestamp * 1000).toLocaleTimeString();

  return {
    labels: [...previous.labels.slice(1), timeLabel],
    timestamps: [...previous.timestamps.slice(1), timestamp],
    series: previous.series.map((series) => {
      const match = result.find(({ metric }) => seriesLabel(metric) === series.label);
      const value = match ? Number(match.value?.[1]) : 0;

      return { ...series, data: [...series.data.slice(1), value] };
    }),
  };
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd frontend && npx vitest run src/lib/metricsParser.test.ts`
Expected: PASS, including the 6 new tests.

- [ ] **Step 5: Call it from the component**

In `frontend/src/components/metrics/TimeSeriesChart.tsx`, add `appendMetricPoint` to the existing import from `@/lib/metricsParser.ts`, then replace the whole `handlePoint` const with:

```typescript
    const handlePoint = (event: MessageEvent) => {
      try {
        const response = JSON.parse(event.data) as PrometheusResponse;

        setParsedMetrics((previous) =>
          previous ? (appendMetricPoint(previous, response) ?? previous) : previous,
        );
      } catch {
        /* ignore parse errors */
      }
    };
```

Then check whether `seriesLabel` is still referenced anywhere else in the file; if it is not, remove it from the import.

- [ ] **Step 6: Verify nothing regressed**

Run: `cd frontend && npx tsc -b --noEmit && npm run lint && npm run fmt:check && npx vitest run`
Expected: PASS, 595 tests.

- [ ] **Step 7: Commit**

```bash
cd /Users/moritz/GolandProjects/cetacean
git add frontend/src/lib/metricsParser.ts frontend/src/lib/metricsParser.test.ts \
        frontend/src/components/metrics/TimeSeriesChart.tsx
git commit -m "refactor(frontend): move the metrics point-merge into metricsParser"
```

---

### Task 4: Move Docker log parsing into `internal/logs`

**Files:**
- Create: `internal/logs/logs.go` (moved verbatim from `internal/api/logparse.go`, package renamed)
- Create: `internal/logs/logs_test.go` (moved verbatim from `internal/api/logparse_test.go`, package renamed)
- Delete: `internal/api/logparse.go`, `internal/api/logparse_test.go`
- Create: `internal/api/logtypes.go` (the `LogLine` alias)
- Modify: `internal/api/log_handlers.go` (call sites)
- Modify: `internal/api/loghandler_test.go` (gains its own `buildFrame`)
- Modify: `internal/mcp/logs.go` (use `logs.` instead of `api.`)

**Interfaces:**
- Consumes: nothing.
- Produces: package `github.com/radiergummi/cetacean/internal/logs` exporting `LogLine`, `ParseDockerLogs`, `ParseDockerLogsWithIdleCancel`, `StreamDockerLogs`, with the same signatures they had in `internal/api`. `internal/api` keeps `type LogLine = logs.LogLine` so the JSON shape and OpenAPI schema are untouched.

**This task changes no behaviour.** It is a pure move, and the existing tests are the proof.

**Why a new package rather than `internal/cluster`:** `internal/cluster` is the shared *domain* layer (enrich, search, derive state). Decoding Docker's multiplexed frame format is not domain logic, and folding it in would blur a boundary the codebase currently keeps clean.

- [ ] **Step 1: Create the package by moving the files**

```bash
cd /Users/moritz/GolandProjects/cetacean
mkdir -p internal/logs
git mv internal/api/logparse.go internal/logs/logs.go
git mv internal/api/logparse_test.go internal/logs/logs_test.go
sed -i '' 's/^package api$/package logs/' internal/logs/logs.go internal/logs/logs_test.go
```

- [ ] **Step 2: Add the alias in `internal/api`**

Create `internal/api/logtypes.go`:

```go
package api

import "github.com/radiergummi/cetacean/internal/logs"

// LogLine is the wire shape for a single log line. It is an alias rather than
// a distinct type so the JSON produced here and by the MCP transport cannot
// drift, and so the OpenAPI schema keeps describing one thing.
type LogLine = logs.LogLine
```

- [ ] **Step 3: Repoint the `internal/api` call sites**

In `internal/api/log_handlers.go`, add `"github.com/radiergummi/cetacean/internal/logs"` to the import block, then qualify the two calls:

- `ParseDockerLogsWithIdleCancel(` → `logs.ParseDockerLogsWithIdleCancel(`
- `done <- StreamDockerLogs(` → `done <- logs.StreamDockerLogs(`

Note the local variable named `logs` in `serveLogsSSE` (`logs, err := fetch(...)`) shadows the package name. Rename that variable to `stream` throughout `serveLogsSSE` — including `defer stream.Close()`, `logs.StreamDockerLogs(stream, ch)`, and the `stream.Close()` inside the `r.Context().Done()` case. Do the same for the identically-named variable in the paginated handler.

- [ ] **Step 4: Give `internal/api` tests their own frame builder**

`buildFrame` moved out with `logparse_test.go` but `loghandler_test.go` still uses it. Append to `internal/api/loghandler_test.go`:

```go
// buildFrame writes one Docker multiplexed log frame. The logs package has its
// own copy; Go test helpers do not cross package boundaries.
func buildFrame(streamType byte, payload string) []byte {
	header := make([]byte, 8)
	header[0] = streamType
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))

	return append(header, []byte(payload)...)
}
```

Add `"encoding/binary"` to that file's imports.

- [ ] **Step 5: Repoint `internal/mcp`**

In `internal/mcp/logs.go`, replace `api.LogLine` with `logs.LogLine` and `api.ParseDockerLogsWithIdleCancel` with `logs.ParseDockerLogsWithIdleCancel`, adding the `internal/logs` import. Then check every other file in `internal/mcp` for `api.LogLine` and repoint those too:

```bash
grep -rn "api\.LogLine\|api\.ParseDockerLogs" internal/mcp/
```

Leave `api.DockerLogStreamer` and `api.DockerWriteClient` alone — those are transport interfaces and stay in `internal/api`.

- [ ] **Step 6: Verify the move changed nothing**

Run: `cd /Users/moritz/GolandProjects/cetacean && go build ./... && go test ./...`
Expected: PASS, every package, with no test edits beyond the moved files and the `buildFrame` copy. A failure here means the move was not verbatim.

- [ ] **Step 7: Run the full gate**

Run: `make check`
Expected: exit 0.

- [ ] **Step 8: Commit**

```bash
cd /Users/moritz/GolandProjects/cetacean
git add -A
git commit -m "refactor(logs): move Docker log parsing into its own package"
```

---

### Task 5: One `since` filter, used by all three callers

**Files:**
- Modify: `internal/logs/logs.go` (add `FilterSince`)
- Modify: `internal/logs/logs_test.go` (add its tests)
- Modify: `internal/api/log_handlers.go` (both paths call it)
- Modify: `internal/mcp/logs.go` (gains the filter it never had)
- Modify: `internal/mcp/logs_test.go` (add the MCP regression test)
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: `internal/logs` from Task 4.
- Produces: `logs.FilterSince(lines []LogLine, since string) []LogLine` — returns only lines strictly after `since`; returns the input unchanged when `since` is empty.

**This task fixes a real defect.** MCP cursor paging currently returns the same newest N lines on every call.

- [ ] **Step 1: Write the failing test for the shared filter**

Append to `internal/logs/logs_test.go`:

```go
func TestFilterSince(t *testing.T) {
	lines := []LogLine{
		{Timestamp: "2024-01-01T00:00:00.000000000Z", Message: "before"},
		{Timestamp: "2024-01-01T00:00:01.000000000Z", Message: "at cursor"},
		{Timestamp: "2024-01-01T00:00:02.000000000Z", Message: "after"},
	}

	t.Run("keeps only lines past the cursor", func(t *testing.T) {
		got := FilterSince(lines, "2024-01-01T00:00:01.000000000Z")

		if len(got) != 1 || got[0].Message != "after" {
			t.Errorf("FilterSince = %v, want just the line after the cursor", got)
		}
	})

	t.Run("returns everything when no cursor is given", func(t *testing.T) {
		if got := FilterSince(lines, ""); len(got) != len(lines) {
			t.Errorf("FilterSince with empty cursor dropped lines: got %d, want %d", len(got), len(lines))
		}
	})

	t.Run("keeps lines that carry no timestamp", func(t *testing.T) {
		untimed := []LogLine{{Message: "no timestamp"}}

		if got := FilterSince(untimed, "2024-01-01T00:00:01.000000000Z"); len(got) != 1 {
			t.Errorf("FilterSince dropped an untimestamped line")
		}
	})
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/logs/ -run TestFilterSince`
Expected: FAIL — `undefined: FilterSince`.

- [ ] **Step 3: Implement it**

Append to `internal/logs/logs.go`:

```go
// FilterSince returns the lines strictly newer than the given cursor.
//
// Docker ignores the Since option for service logs, so every caller that
// offers a cursor has to enforce it after parsing. This is that one place.
// Lines without a timestamp are kept: they cannot be placed relative to the
// cursor, and dropping them would silently lose output.
func FilterSince(lines []LogLine, since string) []LogLine {
	if since == "" {
		return lines
	}

	filtered := lines[:0]
	for _, line := range lines {
		if line.Timestamp != "" && line.Timestamp <= since {
			continue
		}
		filtered = append(filtered, line)
	}

	return filtered
}
```

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/logs/ -run TestFilterSince`
Expected: PASS, 3 subtests.

- [ ] **Step 5: Write the failing MCP test**

Append to `internal/mcp/logs_test.go`. The fixtures it uses — `fakeLogStreamer`,
`newLogTestServer`, `buildLogFrame` — already exist in that file.

```go
// The get_logs cursor is only meaningful if passing it back returns newer
// lines. Docker ignores Since for service logs, so the filter has to happen
// here — without it every call returns the same newest lines and an agent
// paging through a log loops on the same output.
func TestReadServiceLogsHonoursCursor(t *testing.T) {
	c := cache.New(nil)
	streamer := &fakeLogStreamer{
		frames: append(
			append(
				buildLogFrame(1, "2024-01-01T00:00:00.000000000Z before\n"),
				buildLogFrame(1, "2024-01-01T00:00:01.000000000Z at cursor\n")...,
			),
			buildLogFrame(1, "2024-01-01T00:00:02.000000000Z after\n")...,
		),
	}
	srv := newLogTestServer(t, c, streamer)

	got, err := srv.readServiceLogsImpl(
		context.Background(),
		"svc1",
		logOptions{since: "2024-01-01T00:00:01.000000000Z"},
	)
	if err != nil {
		t.Fatalf("readServiceLogsImpl: %v", err)
	}

	if len(got.Lines) != 1 {
		t.Fatalf("got %d lines, want only the one after the cursor: %+v", len(got.Lines), got.Lines)
	}
	if !strings.Contains(got.Lines[0].Message, "after") {
		t.Errorf("line = %q, want the line after the cursor", got.Lines[0].Message)
	}
}
```

Add `"strings"` to that file's imports if it is not already there.

- [ ] **Step 6: Run it to verify it fails**

Run: `go test ./internal/mcp/ -run TestReadServiceLogsHonoursCursor`
Expected: FAIL — 3 lines returned instead of 1.

- [ ] **Step 7: Apply the filter in MCP**

In `internal/mcp/logs.go`, immediately after the existing level filter:

```go
	lines = filterLogLines(lines, opts.level)
	lines = logs.FilterSince(lines, opts.since)
```

Also raise the requested tail when a cursor is given, or the backlog the cursor points into may already have scrolled past — mirror what the REST path does:

```go
	tail := opts.tail
	if tail <= 0 {
		tail = defaultLogTail
	}
	if opts.since != "" {
		tail = min(tail*10, maxLogTail)
	}
	if tail > maxLogTail {
		tail = maxLogTail
	}
```

- [ ] **Step 8: Run it to verify it passes**

Run: `go test ./internal/mcp/`
Expected: PASS, including the new test.

- [ ] **Step 9: Route the two REST paths through the shared filter**

In `internal/api/log_handlers.go`, replace the `since`/`until` compaction block in the paginated handler so `since` goes through the shared helper and only `until` stays local:

```go
	lines = logs.FilterSince(lines, since)

	if until != "" {
		filtered := lines[:0]
		for _, l := range lines {
			if l.Timestamp >= until {
				continue
			}
			filtered = append(filtered, l)
		}
		lines = filtered
	}
```

Leave the SSE path's per-line `if since != "" && line.Timestamp <= since` alone — it filters a stream one line at a time, not a slice, so it cannot call a slice helper. Update its comment to point at the shared one:

```go
			// The per-line form of logs.FilterSince; Docker ignores since for
			// service logs, so a resumed stream has to enforce its own cursor.
```

- [ ] **Step 10: Run the full gate**

Run: `make check`
Expected: exit 0, full Go suite and frontend suite green.

- [ ] **Step 11: Add the changelog entry**

Under `## [Unreleased]` → `### Fixed` in `CHANGELOG.md`, add:

```markdown
- AI agents reading service logs over MCP now advance through history correctly. Passing the returned cursor back fetched the same newest lines every time instead of the next page, so an agent paging through a log could loop on the same output.
```

- [ ] **Step 12: Commit**

```bash
cd /Users/moritz/GolandProjects/cetacean
git add -A
git commit -m "fix(mcp): honour the log cursor when paging service logs"
```

---

## Post-plan verification

- [ ] `make check` exits 0
- [ ] `cd frontend && npx vitest run` reports 595 tests passing
- [ ] `git push` and all 11 CI checks pass on PR #148
- [ ] `CLAUDE.md` mentions `internal/logs` in the backend architecture list — add a line if Task 4 did not
- [ ] `docs/api.md`'s MCP cursor promise is now true; no doc change needed, but re-read it to confirm
