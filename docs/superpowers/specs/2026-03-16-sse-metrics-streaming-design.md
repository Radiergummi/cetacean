# SSE Metrics Streaming

## Problem

Metrics charts currently poll Prometheus on a 30-second auto-refresh timer. This creates unnecessary latency (up to 30s stale data) and wastes requests when nothing has changed. The existing SSE infrastructure handles Docker resource events but not metrics data.

## Solution

Allow the frontend to subscribe to a Prometheus query via SSE. The server periodically executes the query and pushes new data points. Uses content negotiation on `/-/metrics/query_range` — JSON requests continue through the existing proxy, SSE requests go to a new streaming handler.

**Note on meta endpoint convention:** CLAUDE.md states that `/-/` meta endpoints have no content negotiation. This is an intentional exception — `/-/metrics/query_range` is a data endpoint (proxied Prometheus queries), not a health/status endpoint. The convention applies to operational endpoints like `/-/health` and `/-/ready`. The global `negotiate` middleware already runs on all routes; this change simply makes the `/-/metrics/query_range` route act on the negotiated content type.

## Backend

### Handler: `HandleMetricsStream`

New handler in a dedicated file `internal/api/metricsstream.go`. Method on `Handlers` (which already holds `PromClient` for `HandleClusterMetrics` and `HandleMonitoringStatus`). Registered at `GET /-/metrics/query_range` using `contentNegotiatedWithSSE`.

If `PromClient` is nil (Prometheus not configured), the handler returns 503 immediately — consistent with the existing `PrometheusNotConfiguredHandler`.

**Query parameters:**
- `query` (required): PromQL expression
- `step` (optional): push interval in seconds. Default 15s, minimum 5s, maximum 300s. Also used as the Prometheus range query step (deliberate simplification — decoupling push interval from query resolution is not needed yet).
- `range` (optional): initial history window in seconds. Default 3600 (1h). Determines how many data points the `initial` event contains.

**Connection lifecycle:**
1. Validate params. Return 400 if `query` is missing or `step` is out of range.
2. Check connection count against limit (64). Return 429 with `Retry-After: 5` if full. (Lower than the Docker SSE broadcaster's 256 limit because each metrics stream connection creates recurring Prometheus query load.)
3. Set SSE headers (`Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`).
4. Run an initial range query covering the `range` window (i.e., `start = now - range`, `end = now`, `step = step`). Send the result as an `initial` event. This provides the full chart history on connect and reconnect.
5. Start a ticker at the `step` interval.
6. On each tick: if the previous query is still in-flight, skip this tick (prevents query pile-up when Prometheus is slow or `step` is short). Otherwise, run an instant query. Send the result as a `point` event. If Prometheus returns an error or is unreachable, send an `error` event and continue (do not close the connection — Prometheus may recover on the next tick).
7. Send an SSE comment (`: keepalive\n\n`) if no event of any kind (data or comment) was sent in the last 15 seconds. This is a separate ticker that resets whenever any write occurs. Prevents reverse proxies from closing idle connections (important when `step` is large, e.g., 300s).
8. On `r.Context().Done()` (client disconnect), stop all tickers and decrement the connection count.

### SSE Event Format

**`initial` event** — full range query result, raw Prometheus JSON bytes:
```
event: initial
data: {"status":"success","data":{"resultType":"matrix","result":[{"metric":{"__name__":"up","instance":"..."},"values":[[1710000000,"1"],[1710000015,"1"],...]}]}}
```

**`point` event** — single instant query result, raw Prometheus JSON bytes:
```
event: point
data: {"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"up","instance":"..."},"value":[1710000030,"1"]}]}}
```

**`error` event** — Prometheus query failure:
```
event: error
data: {"error":"connection refused","errorType":"server_error"}
```

All data events use the standard Prometheus response format so the frontend can reuse existing parsing logic. The `error` event signals a transient failure; the stream continues and may recover on the next tick.

### Connection Limits

Package-level `sync/atomic.Int32` in `metricsstream.go`. Incremented on connect, decremented on disconnect (via `defer`). Capped at 64 connections. Returns 429 with `Retry-After: 5` header when full.

### PromClient Changes

Add two raw-bytes methods to `PromClient`:
- `RangeQueryRaw(ctx, query, start, end, step) ([]byte, error)` — hits `/api/v1/query_range`, returns the raw Prometheus JSON response body.
- `InstantQueryRaw(ctx, query) ([]byte, error)` — hits `/api/v1/query`, returns the raw Prometheus JSON response body.

These complement the existing `InstantQuery` (which parses into `[]PromResult`). The streaming handler sends these bytes directly as SSE event data without re-encoding. Both methods validate the HTTP status code and return an error for non-2xx responses.

## Frontend

### TimeSeriesChart Changes

The initial JSON fetch provides fast time-to-first-paint. After it completes successfully (for "live" ranges only — not custom from/to ranges), the chart opens an SSE connection for ongoing updates:

1. Open an `EventSource` to `/-/metrics/query_range?query=...&step=<S>&range=<R>` where:
   - `S` is the step: `Math.max(Math.floor(rangeSec / 300), 15)` (same formula already used in the existing `fetchData` function)
   - `R` is the range in seconds matching the selected preset (3600, 21600, 86400, 604800)
2. On `initial` event: parse and replace chart data. This handles reconnects — the full range is included so the chart doesn't show gaps after a disconnect.
3. On `point` event: parse the vector result. For each series, append the new `[timestamp, value]` pair. Drop the oldest point to keep the window size fixed (maintaining the same number of data points).
4. On `error` event: log to console. Do not close the connection — the server will retry on the next tick. Optionally show a transient indicator in the chart header.
5. On EventSource error (HTTP-level): close the connection. Fall back to a 30s `setInterval` poll as degraded mode. Retry SSE connection after 10s.

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

The `/-/metrics/query_range` route currently falls through to the `/-/metrics/` catch-all which routes to `PrometheusProxy`. Extract it as a separate route using `contentNegotiatedWithSSE`:
- JSON → `promProxy.ServeHTTP` (the proxy's `TrimPrefix("/-/metrics")` logic works unchanged)
- SSE → `h.HandleMetricsStream` (method on `Handlers`)
- HTML → SPA fallback (standard behavior)

The catch-all `GET /-/metrics/` continues to handle `/-/metrics/query` and other paths. Note: `/-/metrics/status` is already registered as a separate explicit route.

## Scope Exclusions

- No authentication or per-query rate limiting beyond the connection cap.
- No server-side query caching — each SSE connection runs its own Prometheus queries independently.
- No multiplexing of identical queries across clients — simplicity over optimization.
- No streaming for `/-/metrics/query` (instant queries) — only range queries benefit from streaming.
