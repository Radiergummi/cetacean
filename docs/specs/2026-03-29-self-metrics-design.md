# Self-Metrics Design

Expose Prometheus metrics about Cetacean's own operation at `GET /-/metrics`.

## Endpoint

`GET /-/metrics` serves Prometheus exposition format. No content negotiation, no discovery link headers (same as other `/-/*` meta endpoints). Exempt from auth (like all `/-/*` routes).

## Metrics

### HTTP (middleware, all handlers)

| Metric | Type | Labels |
|---|---|---|
| `cetacean_http_requests_total` | Counter | `method`, `handler`, `status` |
| `cetacean_http_request_duration_seconds` | Histogram | `method`, `handler` |
| `cetacean_http_request_size_bytes` | Histogram | `method`, `handler` |
| `cetacean_http_response_size_bytes` | Histogram | `method`, `handler` |

`handler` uses `r.Pattern` (Go 1.22+), e.g. `GET /nodes/{id}`. SPA catch-all falls back to `unknown`.

### SSE

| Metric | Type | Labels |
|---|---|---|
| `cetacean_sse_connections_active` | Gauge | -- |
| `cetacean_sse_events_broadcast_total` | Counter | -- |
| `cetacean_sse_events_dropped_total` | Counter | -- |

### Cache

| Metric | Type | Labels |
|---|---|---|
| `cetacean_cache_resources` | Gauge | `type` |
| `cetacean_cache_sync_duration_seconds` | Histogram | -- |
| `cetacean_cache_mutations_total` | Counter | `type`, `action` |

### Prometheus proxy

| Metric | Type | Labels |
|---|---|---|
| `cetacean_prometheus_requests_total` | Counter | `status` |
| `cetacean_prometheus_request_duration_seconds` | Histogram | -- |

### Recommendations

| Metric | Type | Labels |
|---|---|---|
| `cetacean_recommendations_check_duration_seconds` | Histogram | `checker` |
| `cetacean_recommendations_total` | Gauge | `severity` |

## Architecture

- New package `internal/metrics` owns a custom `prometheus.Registry`, defines all metric descriptors, and exposes the HTTP handler.
- HTTP instrumentation: middleware in `internal/api/middleware.go` wrapping the mux. Uses `r.Pattern` for the `handler` label.
- SSE, cache, proxy, and recommendations each call `metrics.RecordX()` functions. Subsystems do not import Prometheus directly.
- Custom registry (not `prometheus.DefaultRegistry`) avoids polluting with Go runtime metrics from other libraries.

## Compose config

Add `prometheus.endpoint: /-/metrics` to the cetacean service deploy labels.
