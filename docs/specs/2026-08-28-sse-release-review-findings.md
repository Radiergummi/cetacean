# SSE branch — pre-release review findings

Four independent reviews of `fix/log-viewer-reconnect-backoff` (14 commits) before cutting a
release: `/code-review max` (10 sub-reviewers), an adversarial seam review, a deep dive on the one
unreviewed commit, and a docs/contract accuracy pass.

**Verdict: do not release.** The findings are not 15 independent bugs; they trace to five
decisions, four of which are mine from this branch. Several convert a previously *visible*
failure into a *silent* one, which is the worst direction for an observability tool.

## Root cause A — the cursor was made authoritative over the whole SSE stream, not just the backlog

Task 5 added a per-line `since` filter to `serveLogsSSE` so a resumed stream would not replay.
But the filter applies to every line for the connection's lifetime, including live output the
connection itself produces. Before Task 5 there was no post-filter at all (Docker ignores `since`
for service logs), so these all worked.

- `?after=30m` (a Go duration, documented in `api/openapi.yaml` and accepted by
  `validLogTimestamp`) now drops every line: `'2' < '3'` lexicographically. The stream emits
  nothing but keepalives forever — and because keepalives count as activity, the UI shows a
  pulsing green "Live" badge over an empty view and never errors.
- `?after=2026-08-28T10:00:00Z` (second precision) drops a line at `...T10:00:00.123456789Z`
  that is 123ms newer: `'.'` (0x2E) < `'Z'` (0x5A).
- `?after=...T12:00:00+02:00` — the same instant as `10:00:00Z` — drops everything.
- A browser clock running ahead (laptop resume, no NTP) blanks the tail for the skew duration:
  `useLogData` seeds the cursor from `new Date().toISOString()`, and that value is now
  authoritative. Same code path also means millisecond precision (3 digits) vs Docker's 9, so
  the first sub-millisecond is always dropped.
- The `tail = "0"` fresh-stream branch is dead in practice: the client always sends `after`, so
  every connection takes the 500-line backlog path.

**Fix direction:** scope the filter to the resumed backlog — the lines Docker replays before the
stream catches up — and never filter live output. That collapses all five symptoms.

## Root cause B — the retry budget resets on any byte, so it is not a budget

`logStream.ts`'s `onActivity` sets `attempt = 0`, and `openLogStream` calls it once per read-loop
iteration. A single keepalive comment therefore restores the full budget, so `maxReconnectAttempts`
and `onGaveUp` are unreachable for any stream that delivers one byte before dropping.

A crash-looping task (Docker EOFs the follow stream on each restart) or a proxy with a short
`proxy_read_timeout` yields: connect → one chunk → `attempt = 0` → drop → `attempt = 1` → wait 1s
→ repeat, for the life of the page. Each iteration makes the server pull 500 lines *per task* out
of Docker against a 128-connection-capped endpoint. The UI pins on amber "Reconnecting (1/5)…"
with no Resume affordance, because `setLive(false)` never runs.

The code this replaced had a hard bound of exactly one failure. Nothing re-establishes any bound.

**Fix direction:** reset the budget on a connection that stays up (e.g. sustained liveness), not
on first byte.

## Root cause C — the manual EventSource reopen drops `Last-Event-ID`

`internal/api/sse/broadcaster.go:176` gates *all* recovery on `Last-Event-ID`: `replayEvents` is
the only caller of the `full_sync` write. `Last-Event-ID` is state the browser keeps on an
EventSource instance and sends only on its own native retry — `new EventSource(url)` sends nothing.

So the exact case commit 2 added (a 429-rejected stream reopened by `openEventStream`) is the one
case that gets neither replay nor `full_sync`. `onOpen` then sets `connected = true`. With
`refetchOnWindowFocus: false` and no polling, `sync` is the only refetch trigger, so the page shows
stale data behind a green indicator. Before this change the stream stayed dead and the indicator
stayed red — the staleness was visible.

**Fix direction:** on a manual reopen, force a refetch client-side, since we know events were
missed and cannot ask the server to replay them.

## Root cause D — untimestamped continuation lines are exempt from the cursor unconditionally

`readDockerLogFrames` splits a frame's payload on `\n` and runs `parseLine` per fragment; only the
first carries Docker's `TIMESTAMP ` prefix, so continuation lines of any multi-line entry (stack
traces, pretty-printed JSON — content the log viewer explicitly special-cases) get `Timestamp: ""`.
`FilterSince` keeps untimestamped lines unconditionally, by design and with tests.

- Every continuation line inside the resume window is re-sent on **every** reconnect, and
  `flush` concatenates with no dedupe. The CHANGELOG claims the opposite.
- MCP: when every line surviving the filter is untimestamped, the backward cursor scan finds
  nothing and `resp.Cursor` falls back to the incoming `since` unchanged — the agent pages
  forever on identical output, at full Docker-fetch cost. That is precisely the loop the
  CHANGELOG says was fixed.
- REST: `resp.Newest` can come back `""`, which zeroes the frontend cursor; `loadNewer`'s
  `!newestRef.current` guard then returns early forever, and `toggleLive` restarts from
  `new Date().toISOString()`, skipping everything logged in between.

**Fix direction:** give continuation lines an orderable identity — inherit the preceding line's
timestamp at parse time — so the filter needs no exemption.

## Root cause E — the cursor is the last-arrived timestamp, not the maximum, over unsorted input

`serveLogs` sorts merged task output ("Docker interleaves lines from multiple tasks");
`serveLogsSSE` and `readServiceLogsImpl` do not. Both then take the last-arrived timestamp as the
next cursor.

Task A's `10:00:05Z` arriving before task B's `10:00:03Z` sets the cursor to `:05`, so on reconnect
every task-B line between `:03` and `:05` is discarded. Reverse the arrival order and they are
returned twice. `newestRef.current` can therefore move backwards, making a Stop/Resume replay up
to 500 already-rendered lines. It also makes `LogResourceResponse`'s "Lines are time-ordered
(oldest first)" false for any multi-replica service, which MCP clients are told to trust.

**Fix direction:** sort, then take the max — in both paths, matching `serveLogs`.

## Pre-existing, surfaced by the review (not regressions from this branch)

- `appendMetricPoint` matches series with bare `seriesLabel(metric)` while `parseRangeResult`
  labels a single-result series with the chart title, so every streamed point appends a hard `0`
  to titled single-aggregate charts (node disk/network, task CPU). The Task 3 extraction was
  verbatim — the omission predates it — but the new test now pins the wrong behaviour as intended.
- `TimeSeriesChart`'s visibility handler resets `hasOpenedRef.current` in the same handler that
  bumps `sseKey`, so the effect re-run early-returns and no listener remains to bump it again:
  after one tab switch, live metrics streaming is dead until the range changes or the panel
  remounts. Our CHANGELOG now claims these charts recover.
- The log tail treats 401 like any other failure, bypassing the app-wide login redirect every
  path in `api/client.ts` performs.
- The live-tail buffer is unbounded in a hidden tab: `requestAnimationFrame` is suspended,
  `animationFrameId` latches non-zero, and `maxLiveLines` is only applied inside `flush`.
- MCP's 10× `tail` widening for cursored reads is never truncated back, so
  `get_logs(tail=100, since=…)` can return up to 1000 lines per task against a schema documenting
  `tail` as the number returned. The REST path truncates; MCP does not.
- `nextCursor` formats with `time.RFC3339Nano`, which trims trailing fractional zeros, so the
  cursor sorts above genuinely newer fixed-width timestamps. Verified: a line at `.999999999Z`
  yields cursor `…:57Z`, dropping the entire `:57` second. Milder forms fire on ~10% of pages.
  The `+1ns` is also redundant now that `FilterSince` excludes the boundary with `<=`.
- The 500-line backlog is Docker's `tail`, which applies **per task**, so a 10-replica service
  replays ~5,000 lines where `api/openapi.yaml` promises "at most 500".

## Why the live testing missed this

The earlier verification against a real single-node swarm exercised: one replica, single-line log
output, short outages, a correct clock, and no MCP client. Root causes A (needs skew, a duration
cursor, or sub-ms precision), B (needs a flapping stream), D (needs multi-line output), and E
(needs multiple replicas) are all unreachable under those conditions. The test was real but its
coverage was narrow, and reporting it as "verified end to end" overstated what it established.
