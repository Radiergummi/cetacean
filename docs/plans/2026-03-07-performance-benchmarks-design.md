# Performance Benchmarks Design

## Goal

Add Go benchmarks and pprof support to Cetacean so we can measure, track, and debug performance.

## Components

### 1. Cache benchmarks (`internal/cache/cache_bench_test.go`)

Benchmark hot paths at sizes 10, 100, 1000:

- `rebuildStacks` — most expensive, called on every service/config/secret/network/volume mutation
- `SetService` (triggers rebuild) vs `SetNode`/`SetTask` (no rebuild)
- `ListNodes`/`ListServices`/`ListTasks` — slice alloc from map
- `ListTasksByService`/`ListTasksByNode` — linear scan
- `Snapshot` — aggregation across all maps
- `GetStackDetail` — multi-map lookups
- Concurrent parallel reads via `b.RunParallel`

Shared helper: `populateCache(c *cache.Cache, n int)` fills all resource types with n items each, with realistic stack labels.

### 2. HTTP handler benchmarks (`internal/api/handlers_bench_test.go`)

- List endpoints via `httptest` — full handler-to-JSON-response path
- With/without `?search=` query param
- SSE `Broadcast` to N clients (10, 100)

### 3. pprof endpoint

- Register `net/http/pprof` at `/debug/pprof/` in existing router
- Always on, no conditional gating
- Zero overhead when not actively profiling

## File structure

```
internal/cache/cache_bench_test.go   — cache benchmarks + populateCache helper
internal/api/handlers_bench_test.go  — HTTP handler + SSE benchmarks
internal/api/router.go               — pprof registration (1 import + 1 line)
```

## Running

```bash
go test -bench=. -benchmem ./internal/cache/
go test -bench=. -benchmem ./internal/api/
go test -bench=. -benchmem ./...       # all benchmarks
```
