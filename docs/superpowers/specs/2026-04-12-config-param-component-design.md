# Config Parameter Card Component

Replace the unwieldy multi-column parameter tables in documentation with a card-based `<ConfigParam>` Astro component. The TOML config key is the primary identifier; CLI flags and env vars are secondary metadata.

## Scope

- New `ConfigParam.astro` component for the documentation website
- CSS additions to `global.css` for card styling
- Convert `configuration.md` to `.mdx` and replace parameter tables with component instances
- Simplify `authentication.md` by removing duplicated tables and linking to the configuration reference
- Astro content config update to support `.mdx` files

## Component API

```astro
---
interface Props {
  name: string       // TOML config path (e.g. "auth.oidc.issuer")
  flag?: string      // CLI flag (e.g. "-auth-oidc-issuer")
  env?: string       // Environment variable (e.g. "CETACEAN_AUTH_OIDC_ISSUER")
  default?: string   // Default value; omit row if absent
  required?: boolean // Show "required" badge
  deprecated?: boolean // Show "deprecated" badge with muted styling
}
---
```

**Slot (children):** Description text. Supports inline markdown rendered by Astro (links, code, emphasis).

### Usage

```mdx
import ConfigParam from '@/components/ConfigParam.astro'

## OIDC

<ConfigParam
  name="auth.oidc.issuer"
  flag="-auth-oidc-issuer"
  env="CETACEAN_AUTH_OIDC_ISSUER"
  required
>
  OIDC issuer URL. Must support [OIDC Discovery](https://openid.net/specs/openid-connect-discovery-1_0.html).
</ConfigParam>

<ConfigParam
  name="auth.oidc.scopes"
  flag="-auth-oidc-scopes"
  env="CETACEAN_AUTH_OIDC_SCOPES"
  default="openid,profile,email"
>
  Comma-separated OIDC scopes.
</ConfigParam>
```

## Rendered Structure

Each `<ConfigParam>` renders as:

```html
<div class="config-param">
  <div class="config-param-heading">
    <code>auth.oidc.issuer</code>
    <span class="config-param-badge required">required</span>
  </div>
  <div class="config-param-description">
    <!-- slotted content -->
  </div>
  <div class="config-param-meta">
    <span class="config-param-label">Flag</span>
    <code>-auth-oidc-issuer</code>
    <span class="config-param-label">Env var</span>
    <code>CETACEAN_AUTH_OIDC_ISSUER</code>
    <!-- Default row only if prop provided -->
  </div>
</div>
```

## Visual Design

- **Heading:** TOML key in monospace bold, full foreground color. Optional badge(s) inline after the key.
- **Required badge:** Small uppercase "REQUIRED" in red with a subtle red border.
- **Deprecated badge:** Small uppercase "DEPRECATED" in muted foreground with a muted border.
- **Description:** Standard prose font size (0.875rem), full line-height. Supports links and inline code.
- **Metadata grid:** Two-column grid (label + value). Labels ("Flag", "Env var", "Default") in muted foreground with font-weight 500. Values in monospace, muted foreground. Rows only appear when the corresponding prop is provided.
- **Card container:** 1px border using `--border`, `--radius` border-radius, standard background. Adjacent cards separated by 0.25rem gap.
- **Width:** Inherits from `.prose` (max-width 720px). No horizontal scrolling.
- **Dark mode:** Uses existing CSS variables — no special handling needed.

## CSS

Add to `global.css` alongside existing prose styles:

```css
/* ── Config parameter cards ──────────────────────────────────────── */

.prose .config-param { ... }
.prose .config-param-heading { ... }
.prose .config-param-badge { ... }
.prose .config-param-badge.required { ... }
.prose .config-param-badge.deprecated { ... }
.prose .config-param-description { ... }
.prose .config-param-meta { ... }
.prose .config-param-label { ... }
```

Scoped under `.prose` to match existing convention. Uses `var(--border)`, `var(--foreground)`, `var(--muted-foreground)`, `var(--radius)` for theme consistency.

## File Changes

### New files

| File | Purpose |
|------|---------|
| `website/src/components/ConfigParam.astro` | The component |

### Modified files

| File | Change |
|------|--------|
| `website/src/styles/global.css` | Add `.config-param` styles |
| `website/src/content.config.ts` | Update glob to include `*.mdx` (if needed) |
| `docs/configuration.md` → `docs/configuration.mdx` | Replace parameter tables with `<ConfigParam>` instances |
| `docs/authentication.md` | Remove duplicated parameter tables; add links to configuration reference sections |

### Tables that stay as tables

These are genuinely tabular and fit fine:

- Health check endpoints table (`configuration.md`)
- Operations level matrix (`configuration.md`)
- API content types, sortable fields, etc. (`api.md`)
- Tailscale mode comparison table (`authentication.md`)

## Out of Scope

- No shared data files or parameter registry — components are authored inline
- No changes to `api.md` or other docs
- No build-time parameter validation
- No interactive features (expand/collapse, copy buttons)
