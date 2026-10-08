# Config & Secret Creation

## Overview

Add the ability to create Docker configs and secrets from the dashboard. This is the first resource creation feature — configs and secrets are the simplest case (name + data) and establish patterns for future resource creation (volumes, networks, services).

## Decisions

- **UI**: Modal dialog triggered from list page header (not a dedicated page)
- **Operations level**: Tier 2 (configuration) — non-destructive, reversible
- **Data input**: Text area + file upload toggle
- **Labels**: Not included in initial creation dialog — can be added later
- **Shared base**: `CreateResourceDialog` handles dialog chrome; per-resource form components handle fields

## Backend

### Docker Client

Two new methods on `Client`, added to `DockerWriteClient` interface:

```go
func (c *Client) CreateConfig(ctx context.Context, spec swarm.ConfigSpec) (string, error)
func (c *Client) CreateSecret(ctx context.Context, spec swarm.SecretSpec) (string, error)
```

Both wrap the Docker SDK's `ConfigCreate`/`SecretCreate` (which return `swarm.ConfigCreateResponse`/`swarm.SecretCreateResponse`) and extract `.ID` to return a plain string.

### API Endpoints

| Method | Path | Tier | Request Body | Response |
|--------|------|------|-------------|----------|
| `POST /configs` | 2 | `{ "name": string, "data": string }` | 201 + JSON-LD detail |
| `POST /secrets` | 2 | `{ "name": string, "data": string }` | 201 + JSON-LD detail |

Data is base64-encoded in the request body using standard base64 (RFC 4648, with padding — compatible with browser `btoa()`). The handler decodes with `base64.StdEncoding.DecodeString` to get `[]byte` for the Docker SDK's `ConfigSpec.Data` / `SecretSpec.Data` fields.

### Handlers

`HandleCreateConfig` and `HandleCreateSecret` follow the existing write handler pattern:

1. Decode JSON request body (malformed JSON returns `API006`)
2. Validate: name required (non-empty), data required (valid standard base64)
3. Build `swarm.ConfigSpec` / `swarm.SecretSpec` with `Name` and decoded `Data` (`[]byte`)
4. Call `writeClient.CreateConfig` / `CreateSecret`
5. On success: inspect the created resource via cache or Docker API, return 201 with `Location: /configs/{id}` header and JSON-LD detail response (same shape as `HandleGetConfig`/`HandleGetSecret`: `{ @context, @id, @type, config/secret, services: [] }`)
6. On Docker error: use `cerrdefs.IsConflict` to detect name conflicts (→ CFG003/SEC003), fall through to `writeDockerError` for other errors

### Error Codes

| Code | Title | Status | When |
|------|-------|--------|------|
| `CFG003` | Config Name Conflict | 409 | Name already exists in the swarm |
| `CFG004` | Invalid Config | 400 | Missing name or invalid base64 data |
| `SEC003` | Secret Name Conflict | 409 | Name already exists in the swarm |
| `SEC004` | Invalid Secret | 400 | Missing name or invalid base64 data |

### Routes

```go
mux.Handle("POST /configs", tier2(h.HandleCreateConfig))
mux.Handle("POST /secrets", tier2(h.HandleCreateSecret))
```

## Frontend

### `CreateResourceDialog` (base component)

Reusable dialog shell for resource creation. Props:

- `resourceType: string` — display label ("config", "secret")
- `requiredLevel: number` — operations level gate
- `onSubmit: (formData) => Promise<response>` — called on confirm
- `children: (props) => ReactNode` — render prop for form fields
- `trigger` — optional custom trigger button; defaults to `+ Create`

Responsibilities:
- Dialog open/close state
- `useAsyncAction` for loading/error states
- Operations level gating (disables trigger below required level)
- Success: toast notification, closes dialog
- Error: inline error display from `ApiError`

### `CreateConfigForm` / `CreateSecretForm`

Rendered inside `CreateResourceDialog`. Fields:

- **Name** — text input, required
- **Data** — two input modes via tab/segmented control:
  - **Text** — textarea for pasting content directly
  - **File** — file input for uploading from disk

The form base64-encodes data before submitting. Secret form is structurally identical to config form but exists as a separate component for independent evolution.

### API Client

```typescript
createConfig: (name: string, data: string) =>
  mutationFetch<ConfigDetail>("/configs", "POST", { name, data }, "application/json"),
createSecret: (name: string, data: string) =>
  mutationFetch<SecretDetail>("/secrets", "POST", { name, data }, "application/json"),
```

Note: the existing `post()` helper takes no body parameter. These use `mutationFetch` directly (same pattern as `installPlugin`).

### Integration

- `ConfigList` and `SecretList` pages get the create dialog button in their page header
- On successful creation, `useSwarmResource` picks up the new resource via SSE optimistic update — no manual refetch needed
- After creation, navigate to the new resource's detail page using the ID from the response

## Testing

### Backend

- Handler tests for both config and secret creation (happy path, validation errors, name conflicts)
- Docker client method tests (mock Docker SDK)
- Router integration: verify tier 2 gating

### Frontend

- Dialog open/close behavior
- Form validation (empty name, empty data)
- Base64 encoding of text input and file upload
- Submit flow (loading state, error display, success navigation)

## Files to Create/Modify

### Backend
- `internal/docker/client.go` — add `CreateConfig`, `CreateSecret` methods
- `internal/api/write_handlers.go` — add `HandleCreateConfig`, `HandleCreateSecret`
- `internal/api/router.go` — register `POST /configs`, `POST /secrets` routes
- `internal/api/errors.go` — add `CFG003`, `CFG004`, `SEC003`, `SEC004`
- `internal/api/write_handlers_test.go` — handler tests

### Frontend
- `frontend/src/components/CreateResourceDialog.tsx` — new base dialog component
- `frontend/src/components/CreateConfigForm.tsx` — new config form
- `frontend/src/components/CreateSecretForm.tsx` — new secret form
- `frontend/src/pages/ConfigList.tsx` — add create button
- `frontend/src/pages/SecretList.tsx` — add create button
- `frontend/src/api/client.ts` — add `createConfig`, `createSecret` methods

### Docs
- `api/openapi.yaml` — add POST endpoints
- `docs/api.md` — document new endpoints
- `CHANGELOG.md` — add entry
