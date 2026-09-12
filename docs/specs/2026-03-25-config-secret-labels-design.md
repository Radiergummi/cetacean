# Config & Secret Label Editing

## Overview

Add label editing to configs and secrets, cloning the existing service label editing pattern. GET endpoints return current labels with ETag; PATCH endpoints accept JSON Patch or Merge Patch to update labels via Docker's `ConfigUpdate`/`SecretUpdate` API.

## Decisions

- **Operations level**: Tier 2 (configuration) — matches service labels
- **Pattern**: Clone of service label GET/PATCH handlers
- **Frontend**: Replace read-only `LabelSection` with `KeyValueEditor` on detail pages

## Backend

### Docker Client

Two new methods on `Client`, added to `DockerWriteClient`:

```go
UpdateConfigLabels(ctx context.Context, id string, labels map[string]string) (swarm.Config, error)
UpdateSecretLabels(ctx context.Context, id string, labels map[string]string) (swarm.Secret, error)
```

Both follow the existing pattern: inspect → mutate `Spec.Labels` → call `ConfigUpdate`/`SecretUpdate` with version → re-inspect and return.

### Handlers

Four new handlers:

- `HandleGetConfigLabels` — cache lookup, return labels as JSON-LD with ETag
- `HandlePatchConfigLabels` — validate Content-Type, apply JSON Patch or Merge Patch, call `writeClient.UpdateConfigLabels`, return updated labels
- `HandleGetSecretLabels` — same pattern as config
- `HandlePatchSecretLabels` — same pattern as config, clears `Spec.Data` if returning the full secret

PATCH handlers validate Content-Type (`application/json-patch+json` or `application/merge-patch+json`), returning `API004` for mismatches. Body limit is 1MB. Patch errors map to `API010` (test failed) or `API011` (invalid patch). Version conflicts map to resource-specific codes.

### Error Codes

| Code | Title | Status | When |
|------|-------|--------|------|
| `CFG005` | Config Version Conflict | 409 | Concurrent modification detected |
| `SEC005` | Secret Version Conflict | 409 | Concurrent modification detected |

### Routes

```go
mux.HandleFunc("GET /configs/{id}/labels", contentNegotiated(h.HandleGetConfigLabels, spa))
mux.Handle("PATCH /configs/{id}/labels", tier2(h.HandlePatchConfigLabels))
mux.HandleFunc("GET /secrets/{id}/labels", contentNegotiated(h.HandleGetSecretLabels, spa))
mux.Handle("PATCH /secrets/{id}/labels", tier2(h.HandlePatchSecretLabels))
```

## Frontend

### API Client

```typescript
patchConfigLabels: (id: string, ops: PatchOp[]) =>
    patch<Record<string, string>>(`/configs/${id}/labels`, ops, "application/json-patch+json"),
patchSecretLabels: (id: string, ops: PatchOp[]) =>
    patch<Record<string, string>>(`/secrets/${id}/labels`, ops, "application/json-patch+json"),
```

### Detail Pages

Replace the read-only `LabelSection` on `ConfigDetail` and `SecretDetail` with `KeyValueEditor`:

```tsx
<KeyValueEditor
  title="Labels"
  entries={labels}
  editDisabled={levelLoading || level < opsLevel.configuration}
  isKeyReadOnly={isReservedLabelKey}
  validateKey={validateLabelKey}
  onSave={async (ops) => {
    const updated = await api.patchConfigLabels(id, ops);
    setLabels(updated);
    return updated;
  }}
/>
```

This requires adding label state management (`useState` + initialization from the detail response) to both detail pages, matching how `NodeDetail` and `ServiceDetail` manage their label state.

## Testing

Handler tests for config and secret labels:
- GET: happy path, not found
- PATCH: happy path (JSON Patch), merge patch, invalid content type, not found, version conflict, Docker error

## Files to Create/Modify

### Backend
- `internal/docker/client.go` — add `UpdateConfigLabels`, `UpdateSecretLabels`
- `internal/api/handlers.go` — add to `DockerWriteClient` interface
- `internal/api/write_handlers.go` — add four handlers
- `internal/api/write_handlers_test.go` — add mock methods + tests
- `internal/api/router.go` — register routes
- `internal/api/errors.go` — add CFG005, SEC005

### Frontend
- `frontend/src/api/client.ts` — add `patchConfigLabels`, `patchSecretLabels`
- `frontend/src/pages/ConfigDetail.tsx` — replace `LabelSection` with `KeyValueEditor`
- `frontend/src/pages/SecretDetail.tsx` — replace `LabelSection` with `KeyValueEditor`

### Docs
- `api/openapi.yaml` — add GET/PATCH endpoints
- `docs/api.md` — document new endpoints
- `CHANGELOG.md` — add entry
