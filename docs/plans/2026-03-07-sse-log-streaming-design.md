# SSE Log Streaming Design

## Summary

Replace the current chunked HTTP log streaming with content-negotiated endpoints. The same `/api/services/{id}/logs` and `/api/tasks/{id}/logs` endpoints serve two modes based on `Accept` header:

| Accept | Mode | Response |
|---|---|---|
| `text/event-stream` | SSE follow stream | SSE events with parsed log lines |
| `application/json` (default) | Paginated batch | JSON with parsed lines + cursors |

## JSON Mode

**Query params:** `?limit=500&after=<timestamp>&before=<timestamp>`

- `limit` defaults to 500, max 10000
- `after` and `before` are RFC3339 timestamps (map to Docker's `since`/`until`)
- Lines are returned oldest-first

**Response:**
```json
{
  "lines": [
    {"timestamp": "2024-01-01T00:00:00.000Z", "message": "started", "stream": "stdout"}
  ],
  "oldest": "2024-01-01T00:00:00.000Z",
  "newest": "2024-01-01T00:00:05.000Z"
}
```

## SSE Mode

**Query params:** `?after=<timestamp>` (optional, start streaming from this point)

**Events:**
```
data: {"timestamp":"2024-01-01T00:00:00.000Z","message":"started","stream":"stdout"}

data: {"timestamp":"2024-01-01T00:00:01.000Z","message":"request","stream":"stderr"}
```

Connection stays open with Docker `Follow: true`. Client reconnects via `EventSource` with `Last-Event-ID` or `?after=` set to the last received timestamp.

## Docker Multiplex Parsing

Replace `stdcopy.StdCopy` with custom frame reader to preserve stream type (stdout=1, stderr=2). Docker multiplex format: 8-byte header `[type(1)][padding(3)][size(4 big-endian)]` followed by `size` bytes of payload.

## Implementation Plan

1. Add `logline.go` — `LogLine` struct, `ParseDockerStream` function that reads Docker multiplex frames and returns `[]LogLine` (batch) or emits to a channel (streaming)
2. Modify `handlers.go` — rewrite `HandleServiceLogs`/`HandleTaskLogs` to content-negotiate, use new parser
3. Update `router.go` — no changes needed (same endpoints)
4. Add tests for multiplex parsing and both response modes
5. Update frontend `client.ts` to use new JSON shape and `EventSource` for SSE
