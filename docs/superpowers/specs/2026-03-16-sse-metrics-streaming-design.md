# SSE Metrics Streaming

## Problem

Metrics charts currently poll Prometheus on a 30-second auto-refresh timer. This creates unnecessary latency (up to 30s stale data) and wastes requests when nothing has changed. The existing SSE infrastructure handles Docker resource events but not metrics data.

## Solution

Allow the frontend to subscribe to a Prometheus query via SSE. The server periodically executes the query and pushes new data points. Uses content negotiation on `/-/metrics/query_range` — JSON requests continue through the existing proxy, SSE requests go to a new streaming handler.

## Backend

### Handler: `HandleMetricsStream`

New handler in a dedicated file `internal/api/metricsstream.go`. Registered at `GET /-/metrics/query_range` using `contentNegotiatedWithSSE` — the existing `PrometheusProxy` handles JSON, this handler handles `text/event-stream`.

**Query parameters:**
- `query` (required): PromQL expression
- `step` (optional): push interval in seconds. Default 15s, minimum 5s, maximum 300s. Also used as the range query step.

**Connection lifecycle:**
1. Validate params. Return 400 if `query` is missing or `step` is out of range.
2. Check connection count against limit (256). Return 429 with `Retry-After: 5` if full.
3. Set SSE headers (`Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`).
4. Run an initial range query covering the last `step * 20` data points (i.e., `start = now - step*20`, `end = now`, `step = step`). Send the result as an `initial` event. This gives the chart enough history to render immediately on connect/reconnect.
5. Start a ticker at the `step` interval.
6. On each tick, run an instant query (`/api/v1/query`). Send the result as a `point` event.
7. On `r.Context().Done()` (client disconnect), stop the ticker and decrement the connection count.

**Uses `PromClient`** (not the proxy) for all queries. `PromClient` already supports instant queries; a `RangeQuery` method will be added.

### SSE Event Format

**`initial` event** — full range query result, same shape as Prometheus `query_range`:
```
event: initial
data: {"status":"success","data":{"resultType":"matrix","result":[{"metric":{"__name__":"up","instance":"..."},"values":[[1710000000,"1"],[1710000015,"1"],...]}]}}
```

**`point` event** — single instant query result:
```
event: point
data: {"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"up","instance":"..."},"value":[1710000030,"1"]}]}}
```

Both use the standard Prometheus response format. This means the frontend can reuse existing parsing logic.

### Connection Limits

Separate counter from the Docker SSE broadcaster (which has its own 256-client cap). Atomic int, incremented on connect, decremented on disconnect. Returns 429 with `Retry-After: 5` header when full.

### PromClient Changes

Add `RangeQuery(ctx, query, start, end, step)` method to `PromClient` that hits `/api/v1/query_range` and returns the raw Prometheus JSON response bytes. The handler sends these bytes directly as SSE event data without re-encoding.

## Frontend

### TimeSeriesChart Changes

After the initial JSON fetch completes successfully:
1. Open an `EventSource` to `/-/metrics/query_range?query=...&step=15` (step derived from the chart's current step calculation).
2. On `initial` event: parse and replace chart data (handles reconnects gracefully).
3. On `point` event: parse the vector result. For each series, append the new `[timestamp, value]` pair. Drop the oldest point to keep the window size fixed.
4. On EventSource error: close the connection. Fall back to a 30s setInterval poll as a degraded mode. Retry SSE connection after 10s.

The existing `fetchData` function remains for the initial load and for manual refresh. SSE handles ongoing updates.

**Custom time ranges (from/to params) do not stream.** When the user selects a custom historical range, the chart fetches JSON only and does not open an SSE connection. Streaming only applies to "live" ranges (1h, 6h, 24h, 7d).

### MetricsPanel Changes

- Remove the auto-refresh toggle (play/pause button) — SSE replaces polling.
- Keep the manual refresh button — it triggers a full refetch and SSE reconnect.

### Connection Management

- Each `TimeSeriesChart` instance manages its own `EventSource`.
- Close on unmount (cleanup in `useEffect`).
- Close and reopen when query/range changes.
- When the tab is hidden (`document.visibilitychange`), close the connection to save resources. Reopen + refetch when the tab becomes visible again.

## Router Changes

The `/-/metrics/query_range` route currently points to the `PrometheusProxy` handler directly. It needs to be changed to use `contentNegotiatedWithSSE` dispatch:
- JSON → existing `PrometheusProxy.ServeHTTP`
- SSE → new `HandleMetricsStream`
- HTML → SPA fallback (standard behavior)

This requires extracting the `/-/metrics/query_range` route from the catch-all `/-/metrics/` proxy registration and registering it separately. The catch-all continues to handle `/-/metrics/query` and other paths.

## Scope Exclusions

- No authentication or per-query rate limiting beyond the connection cap.
- No server-side query caching — each SSE connection runs its own Prometheus queries independently.
- No multiplexing of identical queries across clients — simplicity over optimization.
- No streaming for `/-/metrics/query` (instant queries) — only range queries benefit from streaming.
