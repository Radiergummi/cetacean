# Healthcheck Editor Design

**Date:** 2026-03-20

## Overview

Replace the read-only healthcheck KVTable on the service detail page with a dedicated HealthcheckEditor component that shows the healthcheck as stat cards in display mode and transforms into an inline editor on click.

## Display Mode

- **Top row**: Mode badge ("Shell" green / "Exec" blue) followed by the command in a monospace code block. The raw `CMD`/`CMD-SHELL` prefix from the Docker `Test` array is stripped; the badge conveys the mode. A copy-to-clipboard button sits on the right end.
- **Bottom row**: Grid of stat cards for timing fields. Duration values converted from nanoseconds to human-readable seconds (e.g. "30s", "0.5s"). Fields with zero/undefined values mean "use Docker default" and display as "default" (not "0s"). Cards are only shown when the value is non-zero or explicitly set.
- **Stat cards**: Interval, Timeout, Start Period, Start Interval (during start period, Docker 25+), Retries. Start Interval only shown when non-zero.
- **NONE / missing healthcheck**: If `Test[0]` is `NONE` or the healthcheck is absent, show a muted "Healthcheck disabled" message.
- **Edit button**: Pencil icon in the CollapsibleSection header, matching the pattern used by EnvEditor and KeyValueEditor.

## Edit Mode

Inline transformation — stat cards become input fields, the command block becomes a text input:

- **Enabled toggle** (on/off): Off collapses command and timing fields. Saving in this state sends `["NONE"]` as the Test array.
- **Use Shell toggle**: When on, command is sent as `["CMD-SHELL", command]`. When off, the command string is parsed into args via quote-aware splitting and sent as `["CMD", ...args]`.
- **Command input**: Single full-width monospace text input. In exec mode, a hint reads "Executed directly, not via shell".
- **Duration inputs** (Interval, Timeout, Start Period, Start Interval): Number inputs accepting decimals (e.g. `0.5` for 500ms). Labels show "(s)" suffix. Empty/blank fields are sent as `0` (meaning "use Docker default"). Values converted to nanoseconds (`value * 1e9`) on save.
- **Retries**: Integer number input. Empty means "use Docker default" (sent as `0`).
- **Save / Cancel** buttons bottom right.

## Backend

### Endpoints

- `GET /services/{id}/healthcheck` — returns the current healthcheck config. Content-negotiated via `contentNegotiated` (JSON for API clients, SPA for browsers). Returns `null` healthcheck field when no healthcheck is configured.
- `PUT /services/{id}/healthcheck` — full replacement. Accepts the complete healthcheck config as JSON, replaces the service's healthcheck entirely. Used by the UI editor. Protected by `requireWrite`.
- `PATCH /services/{id}/healthcheck` with `Content-Type: application/merge-patch+json` — partial update. Merges provided fields into the existing healthcheck. Useful for API clients that want to update a single field (e.g. just the timeout). The `Test` array, if present, is replaced wholesale per RFC 7396. Protected by `requireWrite`.

### Docker Write Client

- Add `UpdateServiceHealthcheck(ctx context.Context, id string, hc *container.HealthConfig) (swarm.Service, error)` to the `DockerWriteClient` interface.
- Implementation in `internal/docker/client.go`: inspect service spec, set `Spec.TaskTemplate.ContainerSpec.Healthcheck`, call `ServiceUpdate`.

### Data Model

The healthcheck object uses PascalCase field names matching the Docker SDK's JSON serialization:

```json
{
  "Test": ["CMD-SHELL", "curl -f http://localhost/ || exit 1"],
  "Interval": 30000000000,
  "Timeout": 10000000000,
  "Retries": 3,
  "StartPeriod": 15000000000,
  "StartInterval": 0
}
```

All duration fields are nanoseconds (int64). Zero means "use Docker daemon default", not "zero seconds".

To disable a healthcheck, send `{"Test": ["NONE"]}`.

The response wraps the updated service in a detail response:
```json
{"@context": "...", "@id": "/services/abc123", "@type": "Service", "service": {...}}
```

## Frontend Components

### HealthcheckEditor (`frontend/src/components/service-detail/HealthcheckEditor.tsx`)

New component handling both display and edit modes. Props:
- `serviceId: string`
- `healthcheck: Healthcheck | undefined` — from `service.Spec.TaskTemplate.ContainerSpec.Healthcheck`
- `onSaved: (updated: Healthcheck | null) => void` — callback after successful save

The `Healthcheck` type already exists inline in `types.ts` on the ContainerSpec. Add `StartInterval?: number` to it to match Docker 25+.

### parseCommand (`frontend/src/lib/parseCommand.ts`)

Quote-aware command string parser. Splits a command string into an argument array, respecting single and double quotes:
- `'curl -f "http://localhost/"'` → `["curl", "-f", "http://localhost/"]`
- `'/bin/sh -c "echo hello"'` → `["/bin/sh", "-c", "echo hello"]`

Used when "Use Shell" is off to convert the text input into the `["CMD", ...args]` array.

### API Client

- `api.serviceHealthcheck(id, signal?)` — GET, returns the healthcheck config
- `api.putServiceHealthcheck(id, healthcheck)` — PUT with JSON body (used by the editor)

## Integration

In `ServiceDetail.tsx`, the existing healthcheck CollapsibleSection with KVTable is replaced by the HealthcheckEditor component. The healthcheck data is fetched via the GET endpoint on mount (alongside env, labels, resources). Updates go through the PUT endpoint and the `onSaved` callback refreshes the local state.
