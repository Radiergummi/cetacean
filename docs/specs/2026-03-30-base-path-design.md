# Sub-Path (Base Path) Support

Allow Cetacean to be served under a configurable URL prefix (e.g., `/cetacean/`) so it can coexist with other services behind a reverse proxy.

## Configuration

New field `base_path`:

- **TOML**: `base_path = "/cetacean"`
- **Env var**: `CETACEAN_BASE_PATH=/cetacean`
- **Default**: `""` (root, current behavior)
- **Normalization**: Any input (`cetacean`, `/cetacean/`, `/cetacean`) normalizes to `/cetacean` (leading slash, no trailing slash). Empty string means root.
- **Validation**: No query strings, fragments, double slashes, or dot segments. Single or nested path segments are valid (e.g., `/tools/cetacean`).

## Approach: Strip-then-Delegate

A new outermost HTTP middleware wraps the entire handler chain. Internally, all route registrations and handler logic remain unchanged — they continue to work with unprefixed paths (`/nodes`, `/services`, etc.). The middleware translates at the boundary.

### Inbound (request path stripping)

The middleware checks that `r.URL.Path` starts with the configured base path. If it does, the prefix is stripped before passing to the inner handler. If it doesn't match, the middleware returns 404.

Special cases:
- Path equals the base path without trailing slash (`/cetacean`): serve the SPA (equivalent to `/`).
- Path equals the base path with trailing slash (`/cetacean/`): serve the SPA (equivalent to `/`).

### Trailing slash canonicalization

Any path with a trailing slash (except the base path root) gets a 301 redirect to the same path without the trailing slash, preserving the query string.

Examples:
- `/cetacean/nodes/` → 301 `/cetacean/nodes`
- `/cetacean/` → serves SPA (no redirect)
- `/cetacean` → serves SPA (no redirect)

### Base path in context

The middleware stores the base path in the request context. Handlers access it via `BasePathFromContext(ctx)` to generate correct outbound URLs.

### Outbound URL rewriting

A helper function `absPath(ctx, path)` prepends the base path from context to a path string. Used in:

- `NewDetailResponse` — `@id` field
- `NewCollectionResponse` — `@context` field
- `discoveryLinks` middleware — Link header values
- `writeProblem` / `writeProblemTyped` — error type URIs and instance field
- Location headers in write handlers (config/secret creation)
- Pagination Link headers in list handlers
- Auth cookie `Path` field and OIDC redirect URLs

`NewDetailResponse` and `NewCollectionResponse` gain a context parameter (or base path string) so they can prefix URLs. All call sites already have access to `r`.

### Middleware chain position

```
basePath → requestID → recovery → securityHeaders → auth → negotiate → discoveryLinks → requestLogger → mux
```

The base path wrapper is outermost so that all inner middleware sees stripped paths identical to today's behavior.

## Frontend: Runtime Base Path

### HTML injection

The SPA handler (`spa.go`) modifies the cached `index.html` bytes at startup to inject into `<head>`:

```html
<base href="/cetacean/">
<link rel="canonical" href="/cetacean/">
```

When the base path is empty (root), these become `<base href="/">` and `<link rel="canonical" href="/">`.

The injection uses string replacement on the `<head>` tag. The modified bytes are computed once at startup, not per-request.

### React Router

`<BrowserRouter basename={basePath}>` where `basePath` is derived from `document.baseURI` (parsed to extract the pathname, with trailing slash stripped).

### API client

A helper function (e.g., `basePath()`) reads from `document.baseURI` once and caches it. All fetch URLs, EventSource URLs, and `window.location.href` assignments use this to prefix paths:

- `fetch(basePath() + "/nodes", ...)` in `client.ts`
- `new EventSource(basePath() + path)` in `useResourceStream.ts`
- `window.location.href = basePath() + "/auth/login?redirect=..."` in `client.ts`
- `<form action={basePath() + "/auth/logout"}>` in `ProfilePage.tsx`

### Vite dev server

No changes. Dev always runs at `/`. The proxy already forwards resource paths to `:9000`. Sub-path behavior is tested via `go run .` with `CETACEAN_BASE_PATH` set.

## What Stays Unchanged

- All route registrations in `router.go`
- All handler business logic
- `isResourcePath` function
- Auth middleware exemption paths
- `negotiate` middleware
- SSE broadcaster internals
- Frontend route definitions in `App.tsx` (React Router `basename` handles the offset)
- Frontend test setup

## Testing

- Unit test for base path normalization/validation in `config/`
- Unit test for the base path middleware (stripping, 404 on mismatch, trailing slash redirect)
- Integration tests for `absPath` producing correct `@id`, Link headers, error type URIs
- Frontend: existing tests continue to pass (they don't depend on base path since routes are relative)
