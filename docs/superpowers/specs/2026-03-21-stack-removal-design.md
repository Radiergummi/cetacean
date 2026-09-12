# Stack Removal

## Summary

Add a `DELETE /stacks/{name}` endpoint that removes all services, networks, configs, and secrets belonging to a stack, matching `docker stack rm` behavior. Volumes are intentionally preserved (data safety). The frontend gets a type-to-confirm dialog on the stack detail page.

## Decisions

- **Scope**: Match `docker stack rm` — remove services, networks, configs, secrets. Not volumes.
- **Orchestration**: Single backend endpoint handles multi-step removal. Frontend makes one call.
- **Error handling**: Best-effort. Try all removals, collect errors, return 200 with summary. Skip resources that are already gone (404 from Docker).
- **Operations tier**: Tier 3 (impactful).

## Backend

### Docker Client (`internal/docker/client.go`)

Three new methods:

**`RemoveNetwork(ctx, id string) error`** — calls `c.docker.NetworkRemove(ctx, id)`.

**`RemoveConfig(ctx, id string) error`** — calls `c.docker.ConfigRemove(ctx, id)`.

**`RemoveSecret(ctx, id string) error`** — calls `c.docker.SecretRemove(ctx, id)`.

All three are thin wrappers, same pattern as `RemoveService`.

### DockerWriteClient Interface (`internal/api/handlers.go`)

Add three methods:
```go
RemoveNetwork(ctx context.Context, id string) error
RemoveConfig(ctx context.Context, id string) error
RemoveSecret(ctx context.Context, id string) error
```

### Handler (`internal/api/write_handlers.go`)

**`HandleRemoveStack`** — `DELETE /stacks/{name}`

1. Extract `name` from `r.PathValue("name")`.
2. Look up stack via `h.cache.GetStack(name)` — return 404 if not found.
3. Remove resources in order (matching Docker CLI): services → networks → secrets → configs.
4. For each resource list, iterate and call the corresponding write client method.
5. If the error is `cerrdefs.IsNotFound`, skip it — the resource is already gone. (Use `cerrdefs` — the containerd errdefs package already imported in `write_handlers.go`.)
6. Other errors are collected into an `errors` slice.
7. Log the operation and results via `slog.Info`.
8. Return 200 with JSON body:

```json
{
  "removed": {
    "services": 3,
    "networks": 2,
    "configs": 1,
    "secrets": 1
  },
  "errors": [
    {"type": "network", "id": "abc123", "error": "network is in use"}
  ]
}
```

The `errors` field is omitted when empty. Each entry in `removed` counts only successful removals.

### Router (`internal/api/router.go`)

```
DELETE /stacks/{name} → tier3(HandleRemoveStack)
```

Register in the stacks section, after the existing GET routes.

## Frontend

### API Client (`frontend/src/api/client.ts`)

```ts
removeStack: (name: string) =>
  mutationFetch<{
    removed: { services: number; networks: number; configs: number; secrets: number };
    errors?: { type: string; id: string; error: string }[];
  }>(`/stacks/${name}`, "DELETE"),
```

Add this as a property of the `export const api` object. `mutationFetch` is used directly (not `del`) because we need the response body. `del` returns `void` for 204 responses, but this endpoint returns 200 with a summary. `mutationFetch` is a module-private function accessible within `client.ts` where the `api` object is defined.

### StackActions Component (`frontend/src/components/stack-detail/StackActions.tsx`)

Follows the `NodeActions` type-to-confirm pattern. Controlled `AlertDialog`.

**Props**: `{ stackName: string; serviceCounts: { services: number; networks: number; configs: number; secrets: number } }`

**Remove button**:
- `Button` with `variant="outline"`, destructive styling, `Trash2` icon.
- No disabled state (stacks can always be removed if they exist).
- Opens controlled `AlertDialog`.

**AlertDialog content**:
- Title: "Remove stack?"
- Description: "This will remove all services, networks, configs, and secrets in the **{stackName}** stack. Volumes will not be removed."
- Resource summary: "This stack contains N services, N networks, N configs, N secrets."
- Text input: "Type **{stackName}** to confirm", monospace font.
- Remove button disabled until input matches stack name exactly (case-sensitive).
- On success: check `response.errors` — if present, show inline warning with partial failure details. If clean, navigate to `/stacks` with `{ replace: true }`.

**Gating**: Hidden when `operationsLevel < opsLevel.impactful`.

### StackDetail.tsx Changes

- Import `StackActions`.
- Pass `<StackActions>` as the `actions` prop on `<PageHeader>`, matching the pattern used by `NodeActions` and `ServiceActions` on their respective detail pages.
- Pass `stackName` and resource counts derived from the stack detail response (count of services, configs, secrets, networks arrays).

## Testing

### Backend

Extend `mockWriteClient` with `removeNetworkFn`, `removeConfigFn`, `removeSecretFn` fields + stub methods.

Tests for `HandleRemoveStack`:
- **Success**: all resources removed, returns 200 with correct counts, empty errors.
- **Not found**: stack not in cache, returns 404.
- **Partial failure**: some removals fail (non-404 errors), returns 200 with errors array populated.
- **Already gone**: some resources return 404 from Docker, those are skipped (not counted as errors), counts reflect only resources that were actually removed.
- **Empty stack**: stack exists but has no resources, returns 200 with all counts at 0.
