# Container Exec via ghostty-web

**Date:** 2026-03-17
**Status:** Draft

## Summary

Add interactive shell access to running containers via the Cetacean dashboard. Users can open a terminal modal from task or service detail pages that connects to a container's shell over WebSocket, rendered with ghostty-web (WASM-based terminal emulator).

This is the first write/interactive operation in an otherwise read-only system.

## Data Flow

```
Browser (ghostty-web)
  ↕ WebSocket (binary frames for I/O, text frames for control)
Go exec handler
  ↕ Docker ContainerExecCreate + ContainerExecAttach (hijacked connection)
Container TTY (/bin/bash or /bin/sh)
```

## WebSocket Protocol

**Route:** `GET /tasks/{id}/exec` — WebSocket upgrade

**Frame types:**
- **Binary frames** — raw terminal I/O in both directions (stdin from client, stdout/stderr from container)
- **Text frames** — JSON control messages:
  - Client → Server: `{"type":"resize","cols":120,"rows":40}`
  - Server → Client: `{"type":"exit","code":0}` or `{"type":"exit","reason":"idle_timeout"}`
  - Server → Client: `{"type":"error","message":"..."}`

## Backend

### Docker Client Extension (`internal/docker/client.go`)

Three new methods on `Client`:

```go
ExecCreate(ctx context.Context, containerID string, cmd []string) (string, error)
ExecAttach(ctx context.Context, execID string) (types.HijackedResponse, error)
ExecResize(ctx context.Context, execID string, height, width uint) error
```

Thin wrappers around the Docker SDK. Not added to the `DockerClient` interface used by the watcher — exec is independent of the watch/sync cycle.

### Exec Handler (`internal/api/exec.go`)

Registered on the router as `GET /tasks/{id}/exec`. Sits behind the existing auth middleware (route doesn't match any exempt prefix).

**Lifecycle:**
1. Resolve task ID → container ID via cache (`task.Status.ContainerStatus.ContainerID`)
2. Shell cascade: attempt `ContainerExecCreate` with `["/bin/bash"]`; on failure, retry with `["/bin/sh"]`; if both fail, return error before upgrade
3. Create exec with `AttachStdin: true`, `AttachStdout: true`, `AttachStderr: true`, `Tty: true`
4. Upgrade HTTP connection to WebSocket (using `coder/websocket`)
5. `ContainerExecAttach` returns a hijacked connection
6. Two goroutines pipe WebSocket ↔ hijacked conn:
   - Read goroutine: WebSocket → exec stdin (binary frames) + handle control messages (text frames for resize)
   - Write goroutine: exec stdout → WebSocket (binary frames)
7. On close: cancel context, close exec stream, close WebSocket

**Because `Tty: true`:** Docker returns a raw stream (no multiplexing header). stdout/stderr are interleaved as a single stream, which is what the terminal emulator expects.

**Shell cascade detail:** The cascade happens before the WebSocket upgrade. If neither shell is available, the client gets an HTTP error response (not a WebSocket that immediately errors). This avoids the awkward UX of upgrading and then failing.

### Idle Timeout

- Timer resets on every incoming WebSocket message (binary or text)
- Default: 30 minutes, configurable via `CETACEAN_EXEC_IDLE_TIMEOUT`
- On fire: send `{"type":"exit","reason":"idle_timeout"}`, close exec stream, close WebSocket

### Connection Limit

- 1 concurrent exec session, enforced server-side with an atomic counter
- Additional attempts get `429 Too Many Requests` with RFC 9457 problem detail before the upgrade

### Abnormal Disconnect

- WebSocket read error → context cancel → exec stream cleanup
- Docker handles killing the exec process when the session is torn down

## Configuration

| Variable | Default | Required |
|---|---|---|
| `CETACEAN_EXEC_IDLE_TIMEOUT` | `30m` | No |

Parsed in `internal/config/` alongside existing env vars.

## Frontend

### Dependencies

- `ghostty-web` npm package (~400KB WASM, loaded at runtime via `init()`)

### Components

**`ExecModal`** — Modal/drawer containing the terminal. Manages WebSocket connection and ghostty-web instance lifecycle.

**Integration:**
```typescript
import { init, Terminal } from 'ghostty-web';

// On modal open:
await init();  // load WASM (only when needed)
const term = new Terminal({ fontSize: 14, theme: {...} });
term.open(containerRef);

const ws = new WebSocket(`ws://.../tasks/${taskId}/exec`);
ws.binaryType = 'arraybuffer';

term.onData(data => ws.send(new TextEncoder().encode(data)));

ws.onmessage = (e) => {
  if (e.data instanceof ArrayBuffer) {
    term.write(new Uint8Array(e.data));
  } else {
    const msg = JSON.parse(e.data);
    // handle exit, error control messages
  }
};
```

**Resize:** On terminal resize event, send `{"type":"resize","cols":N,"rows":N}` as text frame. Backend calls `ExecResize`.

**Connection states displayed in modal:**
- **Connecting** — WebSocket opening + shell detection
- **Connected** — terminal active
- **Disconnected** — shell exited / idle timeout / error, with reason and "Reconnect" button

### Entry Points

- **Task detail page:** "Shell" button opens `ExecModal` with task ID
- **Service detail page:** "Shell" button per task/replica in the tasks table, opens `ExecModal` with that task's ID

### Single Session Enforcement

Active session tracked in React state (context). While a session is open, all other "Shell" buttons are disabled with "Session active" tooltip.

### Shell-less Containers

When neither bash nor sh is available, the modal is never opened — the HTTP request fails before WebSocket upgrade. The UI shows an error toast: "No shell available in this container."

## Out of Scope

- Authorization beyond existing auth (exec-specific roles/groups)
- Multiple concurrent exec sessions
- File upload/download through the terminal
- Recording/playback of sessions
