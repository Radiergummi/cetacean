# API Explorer + Guide Trim

Embed the Scalar OpenAPI browser on the documentation website as an interactive API reference, and trim the existing API docs page into a focused guide.

## Scope

- New `/api/explorer` page embedding Scalar against the OpenAPI spec
- Trim `docs/api.md` to remove endpoint reference tables (Scalar covers these)
- Build-time copy of `api/openapi.yaml` into `website/public/`
- Navigation updates

## New Page: `/api/explorer`

An Astro page at `website/src/pages/api/explorer.astro` that embeds the [Scalar API Reference](https://github.com/scalar/scalar) component.

- Loads Scalar from CDN (`https://cdn.jsdelivr.net/npm/@scalar/api-reference`)
- Points at `/openapi.yaml` (copied into `public/` at build time)
- Uses `DocsLayout` for consistent header/sidebar/nav
- The Scalar embed fills the content area without `.prose` width constraints
- Title: "API Reference"

## Build-Time Spec Copy

Add pre-scripts to `website/package.json` that copy the canonical OpenAPI spec before Astro runs:

```json
"prebuild": "cp ../api/openapi.yaml public/openapi.yaml",
"predev": "cp ../api/openapi.yaml public/openapi.yaml"
```

Add `public/openapi.yaml` to `website/.gitignore` (or the root `.gitignore`). The canonical spec stays at `api/openapi.yaml` where the Go integration tests validate it.

## Trim `docs/api.md`

**Title change:** "API Reference" → "API Guide"

**Remove** the "Endpoint Reference" section (everything from `## Endpoint Reference` through the end of the Authentication/API Documentation tables). This covers ~550 lines of per-resource endpoint tables and curl examples that Scalar renders interactively.

**Keep** the following sections that follow the removed endpoint reference:
- Rate Limits
- Self-Discovery
- Request ID

These describe cross-cutting behavior that the OpenAPI spec doesn't surface coherently.

**Add** a callout where the endpoint reference used to be, linking to the explorer:

> For the full endpoint reference with request/response schemas and try-it-out, see the [API Reference](/api/explorer).

## Navigation

Update `website/src/lib/navigation.ts`:

```ts
{ slug: "api", title: "API Guide" },
{ slug: "api/explorer", title: "API Reference" },
{ slug: "api/schema", title: "Schema Reference" },
```

Also update any internal links that reference `/api` as "API Reference" to say "API Guide" (e.g., in the header nav).

## File Changes

### New files

| File | Purpose |
|------|---------|
| `website/src/pages/api/explorer.astro` | Scalar embed page |

### Modified files

| File | Change |
|------|--------|
| `website/package.json` | Add `prebuild` and `predev` copy scripts |
| `docs/api.md` | Rename title, remove endpoint tables, add explorer callout, keep rate limits/self-discovery/request ID |
| `website/src/lib/navigation.ts` | Rename "API Reference" → "API Guide", add "API Reference" entry for explorer |
| `.gitignore` or `website/.gitignore` | Ignore `public/openapi.yaml` |

### Header nav

The header currently links "API" to `/api`. This should continue pointing to `/api` (the guide), or could point to `/api/explorer` (the reference). Keeping it at `/api` makes more sense — the guide is the better landing page for someone clicking "API" in the nav.

## Out of Scope

- No changes to the Go backend's `/api` endpoint or Scalar embedding
- No changes to `api/openapi.yaml` content
- No changes to `api/schema` page
