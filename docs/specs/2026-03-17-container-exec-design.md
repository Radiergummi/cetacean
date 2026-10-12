# Container Exec

**Date:** 2026-03-17
**Revised:** 2026-10-09 — backend redesigned around an ephemeral relay; access control and audit
added; revised again after a pre-mortem review
**Status:** Draft
**Issue:** #6

## Summary

Interactive shell access to running containers from the dashboard, rendered with ghostty-web over
a WebSocket.

Swarm has no cluster-level exec API: a manager's socket can only exec into containers on its own
node. The first revision of this spec stopped there and postponed the feature until a per-node
agent existed. This revision ships it without one, by scheduling a short-lived relay onto the
target node for the length of a session, and leaves a seam for an optional agent to take over
later.

## Scope

**v1:** an interactive shell over the local and relay transports, with metadata audit.

**Deferred**, each with the constraints already settled so it does not get redesigned:

- **Transcripts.** Output only: asciicast `o` and `r` events, with control keys as markers. No
  printable input, ever. Echoed input is already in the output, so an input stream only adds the
  non-echoed input that must not be recorded, and no heuristic can separate the two reliably.
  File output only, never `stdout` (see [Audit](#audit)).
- **`run_command`** (REST and MCP). Behind its own `exec.mcp` toggle, default off, independent of
  `mcp.operations_level`; rate-limited per identity; argv array, never a shell string. Each relay
  call costs several Raft writes, and MCP feeds container output to a model that holds the
  action, so local-transport-only is an option for its first version.
- **Approval flows**, until someone asks.
- **The node agent.** This spec fixes only the interface it implements.

## Decisions

- **No permanently deployed component.** The relay is a one-off swarm job per session. A future
  node agent takes precedence where it is deployed.
- **Authorization and audit live in Cetacean**, in front of one shell-transport interface. The
  relay only moves bytes.
- **`exec` is never granted by default**, not by the absence of a policy and not by `write`.
- **Helper services are untouchable through Cetacean.** They hold the Docker socket.
- **No promise Docker cannot keep.** Docker cannot kill an exec, a log driver cannot report a
  failed write, and a heuristic cannot find a password. Where a guarantee is not available, the
  spec says so and the audit records what actually happened.

## Threat model

- **Anyone with the Docker API on a manager is already cluster root.** They can read any secret by
  mounting it and can run any container. Nothing here defends against them, and nothing needs to.
- **What the relay design defends against:** other workloads on the overlay network, which could
  otherwise impersonate Cetacean to a relay (and so drive a node's Docker socket) or impersonate a
  relay to Cetacean (and so read a user's keystrokes).
- **What the access control defends against:** Cetacean users exceeding their grants, including
  through the helper services a session creates.
- **Exec into a service is at least as strong as reading every secret mounted into it**, whatever
  the `secret:` grants say, and exec into a service that mounts the Docker socket is cluster root.
  The authorization docs state both.

## Access control

All of these must pass:

1. **`exec.enabled = true`.** Cetacean refuses to start with `exec.enabled = true` and
   `auth.mode = none`.
2. **Operations level 3.** `oauth.token_operations_level` narrows it as it narrows every other
   write.
3. **An explicit `exec` grant** on the task's service, resolved the way task writes resolve today.
   `exec` implies `read`; nothing implies `exec`. Without a policy, every identity has full access
   *except* `exec`, so a deployment without a policy has no exec.
4. **The target is eligible:**
   - The task is `running` and has a container ID.
   - Its node is `ready` and `active`, not paused or drained. Cetacean is not an SSH gateway, and
     a relay cannot be scheduled there anyway.
   - Its node is Linux, on an architecture the relay image ships for.
   - It is not a Cetacean task and not a helper.

Exec runs with `Privileged=false`, the container's default user and working directory, and no
extra environment. Cetacean sets these, and the relay enforces them again whatever it is sent.

**Revocation.** When the ACL policy reloads, every live session is re-checked, and those whose
identity no longer holds `exec` on the target are ended.

**Caps** (per Cetacean replica, not cluster-wide):
- `exec.max_sessions` in total.
- `exec.max_sessions_per_identity` for one caller.
- `exec.max_duration` for one session, however busy it is.
- `exec.idle_timeout` for one session, counting **input** only. A running `top` is idle.

A request over a cap gets `429` with `Retry-After`, like the stream caps.

## Sessions as resources

A session is a resource, not a side effect of a `GET`. This keeps the `Allow` convention and gives
operators a way to see and end sessions. The layout mirrors tasks: `/services/{id}/tasks` is a
view of `/tasks` whose items live at `/tasks/{id}`, and `/tasks/{id}/exec` is the view of `/exec`
for one task, whose items live at `/exec/{sid}`. Every URL answers every client.

| URL | HTML | JSON | Other |
|---|---|---|---|
| `/exec` | **Shell sessions** page: the live sessions the caller may see, linked to task, service, node and owner, each with End | `CollectionResponse` of sessions | SSE for live updates, like every list |
| `/exec/{sid}` | **Session detail**: owner, target, transport, phase or outcome, End. Not the terminal | The session | `Upgrade: websocket` attaches the terminal; `DELETE` ends the session |
| `/tasks/{id}/exec` | The task page with its **Shell panel**: the Shell button and the sessions on this task | `CollectionResponse` of the task's sessions | `POST` creates a session: `201`, `Location: /exec/{sid}` |

- **Visibility.** A caller sees their own sessions plus any on a target where they hold `exec`.
  The same rule governs `DELETE`.
- **`Allow` on `/tasks/{id}/exec`** lists `POST` only when every check above passes. It is the
  only URL whose `Allow` can say "may open a shell on *this* task", which is why sessions are
  created there and not on `/exec`. `setAllowSubResource` hard-codes `write` today; it takes the
  permission as a parameter. The Shell button gates on this `Allow`.
- **Representations.** The terminal is one more representation of the session, chosen by
  `Upgrade` the way `/services/{id}/logs` chooses SSE by `Accept`. A type an endpoint cannot
  produce (Atom, CSV) is refused, never answered with JSON.
- **Lifetime.** Sessions are held per replica, in memory. An ended session stays at `/exec/{sid}`
  with its outcome for a few minutes, so the modal and the detail page can show how it ended, and
  is a `404` after that.
- **Attaching.** The upgrade authenticates like any other request: every auth mode reaches a
  browser WebSocket on its own (cookie, proxy headers, Tailscale, client certificate), and bearer
  tokens are only used by clients that can send the header. Only the session's owner may attach,
  once, and the request must pass the trusted-origin logic in `csrf.go` (which honours
  `server.cors.origins`). Creating a session is a `POST` and goes through the existing CSRF
  handling.
- **No reattach in v1.** A dropped WebSocket ends the session; reconnecting creates a new one. This
  is why the terminal is a modal in the task's context and not the page at `/exec/{sid}`:
  reloading that page would end the session. With reattach, the terminal can move onto the page.

## Shell transports

```go
type ShellTransport interface {
	Open(ctx context.Context, req ExecRequest) (ExecSession, error)
}
```

`ExecRequest` names the container, its node and the terminal size. `ExecSession` exposes stdin,
output, resize, and a close that reports how the process actually ended. Chosen per request in
this order:

1. **Local** — the target is on the node whose daemon Cetacean talks to (`docker.host`, identified
   by `Info().Swarm.NodeID`). Exec goes through Cetacean's own client.
2. **Agent** (future).
3. **Relay** — everything else.

### Starting a shell

The same procedure runs in Cetacean (local) and in the relay:

1. Probe for `/bin/sh` with `HEAD /containers/{id}/archive?path=/bin/sh`. If it is missing, the
   result is `no_shell`. Exec-start errors for a missing binary arrive in-stream and vary by
   runtime, so they are not used to detect it.
2. Exec `/bin/sh -c 'echo "$$"; command -v bash >/dev/null && exec bash; exec sh'` with a TTY. The
   first output line is the shell's PID **inside the container**. The transport consumes it, and
   it never reaches the terminal.

### Ending a shell

The Engine API has no call to kill or remove an exec, and closing the attach connection does not
reliably end the process. On teardown the transport:

1. Execs `sh -c 'kill -HUP <pid>'`, then `kill -KILL` after a grace period if `ExecInspect` still
   reports the exec running.
2. Reports the outcome: `exited` (the user ended the shell), `terminated` (the kill took), or
   `detached` (it did not, and the process may still be running).

The audit records the outcome as reported, never an assumed one.

*To verify in the e2e DinD lane before implementing:*
- that the PID line arrives intact through the TTY (as `\r\n`-terminated output);
- that `HUP` ends the shell's children;
- what attach does when the connection closes.

## The relay

### Image

A separate image, working name `cetacean-node`: one static binary on `scratch` with a `relay`
subcommand. It speaks HTTP over the Unix socket for the few endpoints it needs instead of linking
the Docker SDK.

- **Released with Cetacean.** The same workflow builds, signs and SBOMs both images at the same
  version, for `linux/amd64` and `linux/arm64`. A future agent becomes a second subcommand.
- **Pinned by digest.** The relay image is built first and its digest is compiled into the
  Cetacean binary, so a tag moved in the registry cannot put a different image next to every
  node's Docker socket. A build without an embedded digest (`go run`, local images) has no default
  and requires `exec.relay_image`; the relay transport reports itself unavailable without it.
- **Private registries.** `exec.relay_registry_auth` (a secret setting, so it also accepts
  `_FILE`) is sent as `X-Registry-Auth` on service creation. Without it, workers pull
  anonymously.

### Scheduling

For a new session Cetacean:

1. Generates the session key and certificate (see [Authentication](#authentication)) and stores
   them as a swarm secret `cetacean-exec-<sid>`. The private key passes through Raft, which is
   fine under the threat model.
2. Creates the service `cetacean-exec-<sid>` (the session ID is short enough to keep the name
   under 63 characters):
   - mode `replicated-job`, one completion, restart condition `none`
   - constraint `node.id == <target node>`
   - bind mount of `exec.relay_socket` (default `/var/run/docker.sock`), so a deployment running a
     per-node socket proxy can point the relay at it; the proxy must pass the exec hijack
   - endpoint mode `dnsrr`, so a helper takes no virtual IP
   - attached to `exec.network`, Cetacean's own overlay when unset; a dedicated network is
     recommended, to keep helper churn out of a stack's address range and away from label-based
     discovery such as Traefik's
   - the session secret; as arguments, the session ID, the container ID, Cetacean's dial address
     and the CA certificate
   - labels `cetacean.exec.session` and `cetacean.exec.owner`, informational only, and no
     `com.docker.stack.namespace`, so stack views and `stack deploy --prune` ignore it
3. Records the service ID in an in-memory helper registry.
4. Streams progress to the browser (`scheduling`, `pulling`, `connecting`) for up to
   `exec.relay_timeout`.

The relay dials **the IP of the Cetacean task that owns the session**, not the service VIP; with
more than one replica, the VIP may route elsewhere. Cetacean finds its own task through the swarm
API (its service, its node, and the task whose network attachment matches its address), not by
inspecting its own container, whose ID a hostname override or cgroup v2 can hide. When Cetacean
is not running as a swarm service, the relay transport is unavailable and only local exec works.

### Helper protection

A helper holds the Docker socket of its node. Changing its spec is enough to run an arbitrary
image there, and re-running a completed job takes only a spec change.

- **`internal/cluster` refuses every write to a helper**, at every operations level and through
  both transports. Helpers are identified by the registry and by their spec (the image digest and
  the socket mount), **never by labels**, which anyone can set on any service.
- **Recommendations skip only registered helpers.** Adding `cetacean.exec.*` labels to a service
  hides nothing.
- **Helpers stay visible**, marked and linked to their session. Every reader can therefore see who
  has a shell in which container, and when. That is accepted, and the docs say so.

A helper cannot be removed before its session ends: removing the service stops the relay's
container.

### Cleanup

- When a session ends, Cetacean removes the service, then the secret (a secret cannot be removed
  while a service references it).
- **The relay exits on its own** when its WebSocket closes, and when it has not connected within
  `exec.relay_timeout`.
- **A reaper** runs at startup and every minute. It removes helpers and secrets whose session is
  not live and whose owner task is not `running`, or has desired state `shutdown`. It reads that
  state from the swarm API, not the cache, and waits a grace period before removing anything.
  Swarm keeps dead task records under its history retention limit, so whether the owner task still
  exists says nothing.

### Socket proxies

Deployments that restrict Cetacean's socket must allow exec, archive stat, and service and secret
creation and removal, and must pass the exec start hijack (`Upgrade: tcp`). A refused call fails
with a problem detail naming it.

## Authentication

Swarm has no workload identity issuer. SPIFFE/SPIRE needs an agent on every node, which is the
deployment this design avoids. OIDC workload federation and OAuth client credentials both still
have to get a credential onto the relay. A swarm secret is the only confidential channel to one
specific task.

- **Cetacean is an in-memory CA.** The key is generated at startup and never persisted. Sessions
  do not survive a restart, so neither needs the CA.
- **Server certificate:** server-auth EKU only, with an IP SAN for the task address the relay
  dials. The relay verifies both.
- **Per-session client certificate:** client-auth EKU only, short validity, URI SAN
  `spiffe://cetacean/exec/<sid>/<container>`, delivered in the session secret.
- **The relay pins the CA**, which is public and passed as an argument.
- **Cetacean accepts a client certificate once:** for a pending session, from the IP of that
  session's relay task, within the timeout. Services join any swarm-scoped overlay, attachable or
  not, so the source-IP check means nothing without the certificate, and the certificate means
  nothing without being single-use.
- **The relay refuses every container but the one in its certificate.** This guards against a
  Cetacean bug, not a compromised Cetacean, which is root already.
- **A separate listener** (`exec.relay_listen`) serves relay connections. It is never published,
  and it trusts only this CA.

The SPIFFE-shaped IDs are deliberate. A future agent gets a long-lived certificate from the same
CA, and could act as a SPIFFE Workload API provider itself, or a deployment already running SPIRE
could supply the identities.

## Relay protocol

After the mTLS handshake the relay opens a WebSocket at `/relay/v1/sessions/{sid}` on the relay
listener, with subprotocol `cetacean-relay.v1`. A version mismatch is an error, not a best effort.
Framing is the browser protocol below. Cetacean sends one extra first message:

```json
{"type":"open","container":"<id>","cols":120,"rows":40}
```

The relay runs the [start](#starting-a-shell) procedure, streams, and on close runs the
[end](#ending-a-shell) procedure. It reports the outcome as
`{"type":"exit","code":…,"outcome":"exited"|"terminated"|"detached"}`.

## Browser protocol

The WebSocket at `GET /exec/{sid}`:

- **Binary frames:** terminal I/O in both directions.
- **Text frames,** client → server: `{"type":"resize","cols":120,"rows":40}`
- **Text frames,** server → client:
  - `{"type":"status","phase":"scheduling"|"pulling"|"connecting"|"connected"}`
  - `{"type":"exit","code":0,"outcome":"exited"}`, or
    `{"type":"exit","reason":"idle_timeout"|"max_duration"|"terminated_by"|"revoked","outcome":"…"}`
  - `{"type":"error","code":"no_shell"|"relay_timeout"|"relay_unavailable"|"audit_unavailable"|…,"message":"…"}`

Everything decidable before the session exists (checks, caps, eligibility) fails on the `POST` as
an RFC 9457 problem detail. Only what is learned once the transport runs arrives as an `error`
frame.

## Audit

Off unless `audit.enabled = true`. Cetacean emits JSON records on a dedicated stream
(`audit.output`: `stdout`, tagged so a collector can route it, or a file path). Storage, shipping
and retention are out of scope.

**Records:** `exec.denied`, `exec.start`, `exec.end`, carrying:
- time and session ID;
- identity (subject, auth provider, groups) and client address;
- the shell transport used;
- the target (task, service, stack, node, container);
- for `exec.end`: the reason, the outcome as reported (`exited`, `terminated`, `detached`), the
  duration, and bytes in and out.

**Delivery:**
- **File output fails closed.** A write that errors or misses its deadline refuses a session that
  has not started and ends a running one with `audit_unavailable`. The file is reopened on
  `SIGHUP` for rotation.
- **`stdout` is best effort, and the docs say so.** A blocking log driver stalls the write rather
  than failing it, and a non-blocking one drops it silently. Neither returns an error to act on.
- **Records on `stdout` are part of Cetacean's own service logs**, readable by anyone with `read`
  on that service. Metadata is accepted there. Transcripts, when they come, are not allowed on
  `stdout` (see [Scope](#scope)).

## Configuration

Proposed. On implementation these go into `docs/configuration.mdx`, which stays canonical.

| TOML path | Default | |
|---|---|---|
| `exec.enabled` | `false` | Turns exec on; refused with `auth.mode = none` |
| `exec.idle_timeout` | `30m` | Without input |
| `exec.max_duration` | `4h` | Per session, regardless of activity |
| `exec.max_sessions` | `4` | Per replica |
| `exec.max_sessions_per_identity` | `2` | Per replica |
| `exec.relay_image` | the digest built into the release | Required for builds without one |
| `exec.relay_registry_auth` | — | Secret; sent as `X-Registry-Auth` |
| `exec.relay_socket` | `/var/run/docker.sock` | Socket the relay mounts on the node |
| `exec.relay_timeout` | `60s` | Scheduling, pull and connect |
| `exec.relay_listen` | `:9443` | Relay listener; never published |
| `exec.network` | Cetacean's own overlay | A dedicated network is recommended |
| `audit.enabled` | `false` | Metadata records |
| `audit.output` | `stdout` | `stdout` (best effort) or a file path (fails closed) |

## Frontend

The terminal is unchanged from the first revision.

- **Dependency:** `ghostty-web` (~400 KB WASM, loaded on first open via `init()`).
- **`ExecModal`** posts to `/tasks/{id}/exec`, then upgrades `/exec/{sid}` from the `Location`.
  Binary frames are terminal I/O; the terminal's resize events become `resize` messages. It
  carries a link to the session's detail page.
- **States:**
  - connecting, by phase, since a cold relay can take seconds;
  - connected;
  - disconnected, with the reason and outcome. A `detached` outcome is shown as a warning that
    the process may still be running. Reconnect starts a new session.
- **Errors:** `POST` failures (403, 409 for an ineligible target, 429) appear as toasts, without
  opening the modal. `error` frames appear inside it.
- **Routes:**
  - `/tasks/:id/exec` — the task detail page with its Shell panel open.
  - `/exec` — the Shell sessions list, live over SSE like the other lists, linked from the
    navigation only when exec is enabled.
  - `/exec/:sid` — the session detail page.
- **Entry points:** a Shell button in the task's Shell panel and per task in the service detail's
  task table, gated on `Allow` from `GET /tasks/{id}/exec`. End buttons gate on `Allow` from the
  session.

## Out of scope

Beyond what [Scope](#scope) defers:

- File transfer through the terminal
- Session sharing, or watching another user's session live
- Reattaching to a session after a dropped connection
- Exec on Windows nodes
