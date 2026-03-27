# Label Integrations Design

## Problem

Many swarm ecosystem tools (Traefik, Shepherd, Swarm Cronjob, Diun) are configured via Docker service labels. These labels encode structured configuration in flat key-value pairs, making them hard to read in the raw labels view. Cetacean should detect well-known label patterns and present them as structured, readable panels.

## Decisions

- **Read-only** for the initial implementation. Structured editing follows as a separate task.
- **Backend parsing** — the API returns structured integration data so all consumers benefit, not just the UI.
- **Hardcoded per-tool detectors** — each tool gets its own parser module. The label structures vary too much between tools for a generic config-driven approach to be worthwhile.
- **One panel per integration** — each detected integration renders as its own `CollapsibleSection` on the service detail page, above the labels section.
- **HTTP only** — the Traefik parser handles `traefik.http.*` labels. TCP/UDP routers (`traefik.tcp.*`, `traefik.udp.*`) are out of scope for v1 but all `traefik.*` labels are consumed to avoid partial display.
- **Request-time detection** — `Detect()` runs on each service detail request. Label parsing is cheap; caching at mutation time adds complexity for negligible gain.

## Backend

### Package: `internal/integrations/`

**`integrations.go`** — Shared types and detection entry point.

```go
// Result holds detected integrations for a service.
type Result struct {
    Integrations []any             // Detected integrations (omitted from JSON if empty).
    Remaining    map[string]string // Labels not claimed by any integration.
}
```

Each integration type is a concrete struct added directly to the `[]any` slice. This matches how `DetailResponse.extra` already works — Go's `encoding/json` serializes the concrete type's fields.

**`traefik.go`** — Traefik types and detector:

```go
type TraefikIntegration struct {
    Name        string              `json:"name"`
    Enabled     bool                `json:"enabled"`
    Routers     []TraefikRouter     `json:"routers,omitempty"`
    Services    []TraefikService    `json:"services,omitempty"`
    Middlewares []TraefikMiddleware  `json:"middlewares,omitempty"`
}

type TraefikRouter struct {
    Name        string      `json:"name"`
    Rule        string      `json:"rule,omitempty"`
    Entrypoints []string    `json:"entrypoints,omitempty"`
    TLS         *TraefikTLS `json:"tls,omitempty"`
    Middlewares []string    `json:"middlewares,omitempty"`
    Service     string      `json:"service,omitempty"`
    Priority    int         `json:"priority,omitempty"`
}

type TraefikTLS struct {
    CertResolver string              `json:"certResolver,omitempty"`
    Domains      []TraefikTLSDomain  `json:"domains,omitempty"`
    Options      string              `json:"options,omitempty"`
}

type TraefikTLSDomain struct {
    Main string   `json:"main"`
    SANs []string `json:"sans,omitempty"`
}

type TraefikService struct {
    Name   string `json:"name"`
    Port   int    `json:"port,omitempty"`
    Scheme string `json:"scheme,omitempty"`
}

type TraefikMiddleware struct {
    Name   string            `json:"name"`
    Type   string            `json:"type"`
    Config map[string]string `json:"config,omitempty"`
}
```

`TraefikService` is a simplified representation covering the common `loadbalancer.server.port` and `loadbalancer.server.scheme` cases. Advanced LB settings (sticky sessions, passHostHeader, weighted services) are not parsed in v1. `TraefikMiddleware.Config` flattens middleware-specific settings into a string map, which loses nesting for complex middlewares (e.g., headers with multiple sub-keys) — acceptable for a read-only display.

The Traefik detector:
- Consumes all labels prefixed with `traefik.` (including TCP/UDP labels that are not parsed into typed objects).
- Parses `traefik.http.*` into routers, services, and middlewares.
- Maps `traefik.enable` to the `enabled` field.

**`Detect(labels map[string]string) Result`** — Runs all registered detectors. Returns detected integrations and remaining (unconsumed) labels.

### Response shape

The service detail endpoint adds an `integrations` field:

```json
{
  "@context": "...",
  "@id": "/services/abc",
  "@type": "Service",
  "service": { "Spec": { "Labels": { "all": "raw labels still here" } } },
  "integrations": [
    {
      "name": "traefik",
      "enabled": true,
      "routers": [
        {
          "name": "myapp",
          "rule": "Host(`example.com`)",
          "entrypoints": ["websecure"],
          "tls": { "certResolver": "letsencrypt" },
          "middlewares": ["auth"],
          "service": "myapp"
        }
      ],
      "services": [
        { "name": "myapp", "port": 8080, "scheme": "http" }
      ],
      "middlewares": [
        { "name": "auth", "type": "basicauth", "config": { "users": "..." } }
      ]
    }
  ]
}
```

- `integrations` is omitted when no integrations are detected (not an empty array).
- Raw labels in `service.Spec.Labels` are unchanged — the frontend filters consumed keys from the labels editor.
- SSE: integrations are computed at request time, so SSE change events trigger a re-fetch which re-runs detection. No SSE-specific changes needed.

### Integration point

In the service detail handler (`api/handlers.go`), after fetching the service from cache, call `integrations.Detect(service.Spec.Labels)`. Add the result's `Integrations` slice to the detail response extras.

For list endpoints and stack detail: no change. Integrations are only computed for service detail views.

## Frontend

### Types (`api/types.ts`)

```typescript
interface TraefikTLSDomain {
  main: string;
  sans?: string[];
}

interface TraefikRouter {
  name: string;
  rule?: string;
  entrypoints?: string[];
  tls?: { certResolver?: string; domains?: TraefikTLSDomain[]; options?: string };
  middlewares?: string[];
  service?: string;
  priority?: number;
}

interface TraefikService {
  name: string;
  port?: number;
  scheme?: string;
}

interface TraefikMiddleware {
  name: string;
  type: string;
  config?: Record<string, string>;
}

interface TraefikIntegration {
  name: "traefik";
  enabled: boolean;
  routers?: TraefikRouter[];
  services?: TraefikService[];
  middlewares?: TraefikMiddleware[];
}

type Integration = TraefikIntegration;
```

The `ServiceDetailResponse` type gains `integrations?: Integration[]`.

### Components

**`components/service-detail/IntegrationPanel.tsx`** — Dispatcher:

```tsx
function IntegrationPanel({ integration }: { integration: Integration }) {
  switch (integration.name) {
    case "traefik":
      return <TraefikPanel integration={integration} />;
    default:
      return null;
  }
}
```

**`components/service-detail/TraefikPanel.tsx`** — `CollapsibleSection` titled "Traefik":

- **Routers**: name, rule (monospace), entrypoint badges, TLS indicator, middleware ref badges, target service.
- **Services**: port, scheme.
- **Middlewares**: type badge + config as key-value pills.
- If `enabled: false`, muted indicator.
- Visual style matches existing spec sections (compact badges/pills, not heavy tables).

**`pages/ServiceDetail.tsx`** — Changes:

1. Render `IntegrationPanel` for each entry in `integrations`, placed above the labels `KeyValueEditor`.
2. Filter labels: when the Traefik integration is present, exclude keys starting with `traefik.` from the labels editor entries.

### Label filtering

The frontend filters consumed keys by prefix (`traefik.`). This is safe because the backend consumes all `traefik.*` labels (including unparsed TCP/UDP ones), so no information is lost — the integration panel shows the parsed HTTP config, and no `traefik.*` labels remain in the "remaining" set.

## OpenAPI

Add the integration types to `api/openapi.yaml` under the service detail response schema. Use `name` as the discriminator field for the integration union type.

## Testing

### Backend
- `internal/integrations/traefik_test.go` — Table-driven tests: typical label sets, edge cases (no traefik labels, `traefik.enable=false` only, routers without services, middlewares with no matching type, multiple routers, TCP/UDP labels consumed but not parsed).
- `internal/integrations/integrations_test.go` — `Detect()` tests: no integrations, single integration, remaining labels correct.
- Service detail handler test: verify `integrations` appears when Traefik labels are present, omitted when absent.

### Frontend
- No new tests. Panel components are straightforward rendering.
