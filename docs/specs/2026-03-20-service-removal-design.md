# Service Removal

## Summary

Add `DELETE /services/{id}` endpoint and a "Remove" button on the service detail page, enabling users to delete a service and all its tasks.

## Backend

### Docker client

Add `RemoveService` to `internal/docker/client.go`:

```go
func (c *Client) RemoveService(ctx context.Context, id string) error {
    return c.docker.ServiceRemove(ctx, id)
}
```

### Interface

Add to `DockerWriteClient` in `internal/api/handlers.go`:

```go
RemoveService(ctx context.Context, id string) error
```

### Handler

Add `HandleRemoveService` to `internal/api/write_handlers.go`:

- Reads `{id}` from path
- Validates service exists in cache (404 if not)
- Calls `RemoveService`
- Returns 204 No Content on success
- Uses `writeDockerError` for error mapping (same as all other write handlers)

### Route

In `internal/api/router.go`:

```go
mux.Handle("DELETE /services/{id}", tier2(h.HandleRemoveService))
```

Tier 2 (impactful) — consistent with `DELETE /tasks/{id}`.

## Frontend

### API client

Add to `frontend/src/api/client.ts`:

```ts
removeService: (id: string) => del(`/services/${id}`)
```

### UI

In `ServiceActions` component, add a "Remove" button using the existing `ConfirmAction` component:

- Icon: `Trash2` from lucide-react
- Gated by `level >= 2` (impactful tier)
- Confirmation dialog warns: "This will permanently remove the service and all its tasks."
- On success, navigates to the stack page (`/stacks/{stackName}`) if the service has a `com.docker.stack.namespace` label, otherwise to `/services`
- Uses `useNavigate` from react-router for post-deletion redirect

### Navigation after deletion

`ConfirmAction.onConfirm` currently returns void. The remove action will call `useAsyncAction.execute()` with a `.then()` callback that navigates away. The stack name is derived from `service.Spec.Labels["com.docker.stack.namespace"]`.

## Tests

### Backend

Add to `write_handlers_test.go`:

- `TestHandleRemoveService_Success` — mock returns nil, expect 204
- `TestHandleRemoveService_NotFound` — service not in cache, expect 404
- `TestHandleRemoveService_DockerError` — mock returns error, expect 500

### Mock

Add `RemoveService` to `mockWriteClient` in test file.

## Files changed

1. `internal/docker/client.go` — add `RemoveService` method
2. `internal/api/handlers.go` — add `RemoveService` to `DockerWriteClient` interface
3. `internal/api/write_handlers.go` — add `HandleRemoveService` handler
4. `internal/api/write_handlers_test.go` — add tests + mock method
5. `internal/api/router.go` — add `DELETE /services/{id}` route
6. `frontend/src/api/client.ts` — add `removeService` method
7. `frontend/src/components/service-detail/ServiceActions.tsx` — add Remove button with post-deletion navigation
