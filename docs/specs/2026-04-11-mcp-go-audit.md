# mcp-go API Surface Audit

**Date:** 2026-05-20
**Audited by:** Task 0.5 subagent

---

## Pinned Version

| Field | Value |
|---|---|
| Module path | `github.com/mark3labs/mcp-go` |
| Pinned version | `v0.54.0` |
| Source location | `$(go env GOMODCACHE)/github.com/mark3labs/mcp-go@v0.54.0/` |

`go.mod` now carries:

```
require github.com/mark3labs/mcp-go v0.54.0
```

---

## API Surface Audit

All signatures are copied verbatim from the module cache source.

### `server.NewMCPServer`

**Status: matches plan**

```go
// server/server.go:619
func NewMCPServer(
    name, version string,
    opts ...ServerOption,
) *MCPServer
```

`ServerOption` is `func(*MCPServer)`.

---

### `server.NewStreamableHTTPServer`

**Status: matches plan**

```go
// server/streamable_http.go:273
func NewStreamableHTTPServer(server *MCPServer, opts ...StreamableHTTPOption) *StreamableHTTPServer
```

`StreamableHTTPOption` is `func(*StreamableHTTPServer)`.  Note that the option type for `NewStreamableHTTPServer` is **`StreamableHTTPOption`**, distinct from `ServerOption` used for `NewMCPServer`. The plan's code already uses `mcpserver.*` for both, which is correct since they're in the same `server` package.

---

### `server.WithStateful`

**Status: exists, signature matches plan**

```go
// server/streamable_http.go:85
// WithStateful enables stateful session management using InsecureStatefulSessionIdManager.
// This requires sticky sessions in multi-instance deployments.
func WithStateful(stateful bool) StreamableHTTPOption
```

The default is **stateless-generating** (`StatelessGeneratingSessionIdManager` — generates IDs but does not validate them locally). `WithStateful(true)` switches to `InsecureStatefulSessionIdManager`, which tracks live sessions in a local map and is the correct choice for single-node deployments like Cetacean.

A counterpart `WithStateLess(stateLess bool) StreamableHTTPOption` also exists to force pure statelessness (no IDs generated at all). The design's use of `WithStateful(true)` is correct.

---

### `server.WithSessionIdleTTL`

**Status: exists, signature matches plan**

```go
// server/streamable_http.go:172
// WithSessionIdleTTL sets the idle TTL for per-session transport state.
// When enabled, a background sweeper periodically removes entries from
// per-session stores (tools, resources, resource templates, log levels,
// request IDs) for sessions that have been idle longer than the given
// duration.
func WithSessionIdleTTL(ttl time.Duration) StreamableHTTPOption
```

Name is exact. Takes a `time.Duration`. A zero or negative value disables the sweeper (the default).

---

### `server.WithHTTPContextFunc`

**Status: exists, signature matches plan**

```go
// server/http_transport_options.go:11
type HTTPContextFunc func(ctx context.Context, r *http.Request) context.Context

// server/streamable_http.go:117
func WithHTTPContextFunc(fn HTTPContextFunc) StreamableHTTPOption
```

`fn` receives `(ctx context.Context, r *http.Request)` and returns an enriched `context.Context`. This is where the plan extracts the bearer token from `r.Header` and injects identity into the context for downstream tool/resource handlers.

---

### `server.WithResourceCapabilities`

**Status: exists, signature matches plan**

```go
// server/server.go:271
func WithResourceCapabilities(subscribe, listChanged bool) ServerOption
```

Both arguments are positional booleans. The plan calls it as `WithResourceCapabilities(true, true)` — subscribe and listChanged both true — which is correct.

---

### `server.WithToolCapabilities`

**Status: exists — plan's assumption about signature is correct**

```go
// server/server.go:517
func WithToolCapabilities(listChanged bool) ServerOption
```

Takes a single `listChanged bool`, not a variadic. The plan calls `WithToolCapabilities(true)`, which compiles correctly.

---

### `MCPServer.AddTool`

**Status: exists, handler signature confirmed**

```go
// server/server.go:877
func (s *MCPServer) AddTool(tool mcp.Tool, handler ToolHandlerFunc)

// server/server.go:61
type ToolHandlerFunc func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)
```

The plan's handler literals match this signature:

```go
func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error)
```

---

### `MCPServer.AddResource`

**Status: exists, signature confirmed**

```go
// server/server.go:696
func (s *MCPServer) AddResource(
    resource mcp.Resource,
    handler ResourceHandlerFunc,
)

// server/server.go:52
type ResourceHandlerFunc func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error)
```

---

### `MCPServer.AddResourceTemplate`

**Status: exists, signature confirmed**

```go
// server/server.go:782
func (s *MCPServer) AddResourceTemplate(
    template mcp.ResourceTemplate,
    handler ResourceTemplateHandlerFunc,
)

// server/server.go:55
type ResourceTemplateHandlerFunc func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error)
```

`ResourceHandlerFunc` and `ResourceTemplateHandlerFunc` have identical signatures; the distinction is type-level only.

---

### `mcp.NewTool` and option helpers

**Status: all exist, signatures match plan**

```go
// mcp/tools.go:794
func NewTool(name string, opts ...ToolOption) Tool

// mcp/tools.go:779
type ToolOption func(*Tool)

// mcp/tools.go:837
func WithDescription(description string) ToolOption

// mcp/tools.go:1204
func WithString(name string, opts ...PropertyOption) ToolOption

// mcp/tools.go:1182
func WithNumber(name string, opts ...PropertyOption) ToolOption

// mcp/tools.go:783
type PropertyOption func(map[string]any)

// mcp/tools.go:1014
func Required() PropertyOption

// mcp/tools.go:1006
func Description(desc string) PropertyOption
```

All option helpers used in the plan (`WithDescription`, `WithString`, `WithNumber`, `Required()`, `Description()`) exist with the expected shapes.

---

### `mcp.NewToolResultText` and `mcp.NewToolResultError`

**Status: both exist, signatures match plan**

```go
// mcp/utils.go:301
func NewToolResultText(text string) *CallToolResult

// mcp/utils.go:423
func NewToolResultError(text string) *CallToolResult
```

Both return `*mcp.CallToolResult`. `NewToolResultError` sets `IsError: true`.

Additional helpers available (not in the plan but useful): `NewToolResultErrorFromErr(text string, err error)`, `NewToolResultErrorf(format string, a ...any)`.

---

### Hook for intercepting `tools/list` (per-identity ACL filtering)

**Status: clean dedicated API exists**

The plan asked whether a hook exists for filtering `tools/list` by identity. Two mechanisms are available:

**1. `WithToolFilter` (recommended — preferred for ACL use)**

```go
// server/server.go:75
type ToolFilterFunc func(ctx context.Context, tools []mcp.Tool) []mcp.Tool

// server/server.go:346
func WithToolFilter(toolFilter ToolFilterFunc) ServerOption
```

`WithToolFilter` is applied inside `handleListTools` after session-specific tools are merged but before pagination. The `ctx` argument carries the session context, so identity injected by `WithHTTPContextFunc` is available. Multiple filters can be registered; they are applied in registration order.

This is the correct approach for per-identity ACL filtering of `tools/list`. Register it at server construction:

```go
mcpSrv := mcpserver.NewMCPServer("cetacean", version,
    mcpserver.WithToolFilter(func(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
        identity := identityFromContext(ctx) // extracted by WithHTTPContextFunc
        return filterToolsByACL(tools, identity)
    }),
)
```

**2. `Hooks.OnAfterListTools` (observer-only, not recommended for filtering)**

```go
// server/hooks.go:104–105
type OnBeforeListToolsFunc func(ctx context.Context, id any, message *mcp.ListToolsRequest)
type OnAfterListToolsFunc  func(ctx context.Context, id any, message *mcp.ListToolsRequest, result *mcp.ListToolsResult)
```

`OnAfterListTools` fires after the result is assembled. The `result` pointer is mutable, so tools can technically be removed by modifying `result.Tools` in place — but this is not the intended use of hooks and may break if the library adds result immutability. **Use `WithToolFilter` instead.**

---

## Reconciliation

All names referenced in the plan exist in v0.54.0 with matching signatures. No plan edits are required.

| Name checked | Exists in v0.54.0 | Signature drift | Plan edit needed |
|---|---|---|---|
| `server.NewMCPServer` | Yes | None | No |
| `server.NewStreamableHTTPServer` | Yes | None | No |
| `server.WithStateful(bool)` | Yes | None | No |
| `server.WithSessionIdleTTL(d)` | Yes | None | No |
| `server.WithHTTPContextFunc(fn)` | Yes | None | No |
| `server.WithResourceCapabilities(subscribe, listChanged bool)` | Yes | None | No |
| `server.WithToolCapabilities(listChanged bool)` | Yes | None | No |
| `MCPServer.AddTool(tool, handler)` | Yes | None | No |
| `MCPServer.AddResource(res, handler)` | Yes | None | No |
| `MCPServer.AddResourceTemplate(tmpl, handler)` | Yes | None | No |
| `mcp.NewTool(name, opts...)` | Yes | None | No |
| `mcp.WithDescription`, `mcp.WithString`, `mcp.WithNumber` | Yes | None | No |
| `mcp.Required()`, `mcp.Description()` | Yes | None | No |
| `mcp.NewToolResultText`, `mcp.NewToolResultError` | Yes | None | No |
| `tools/list` filter hook | `WithToolFilter` + `ToolFilterFunc` — clean API present | N/A (new info) | No edits required; plan's fallback note is superseded |

**Summary:** The plan can be implemented as written against v0.54.0 without any name changes. The `WithToolFilter` + `ToolFilterFunc` API provides a clean hook for per-identity `tools/list` filtering via the context — the plan's fallback recommendation ("filter at call time") is not needed.

### Notable additions in v0.54.0 not in the plan

These are not blockers, but implementers should be aware:

- **`WithStrictInputSchemaDefault()`** — server-side enforcement of `additionalProperties: false` on all tool schemas. Recommend enabling.
- **`WithInputSchemaValidation()`** — validates tool call arguments against declared schemas. Recommend enabling for production.
- **`WithToolHandlerMiddleware(ToolHandlerMiddleware)`** — middleware chain for tool handlers, useful for logging and tracing.
- **`mcp.NewToolResultErrorFromErr(text string, err error)`** — convenience wrapper that appends the error detail to the message text.
- **`ServerTool` / `ServerResource` / `ServerResourceTemplate`** structs — bulk-registration variants (`AddTools`, `AddResources`, etc.) available alongside the single-item `Add*` methods used by the plan.
- **Task tools (`AddTaskTool`, `TaskToolHandlerFunc`)** — async tool execution returning `*mcp.CreateTaskResult`. Not needed for the current design but available if long-running tools are added later.
