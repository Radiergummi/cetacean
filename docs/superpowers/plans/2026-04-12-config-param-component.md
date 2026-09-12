# ConfigParam Card Component Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace multi-column parameter tables in documentation with a card-based `<ConfigParam>` Astro component, and deduplicate parameter definitions between the configuration reference and authentication guide.

**Architecture:** A single `ConfigParam.astro` component renders each parameter as a definition-list card. CSS is added to `global.css` alongside existing prose styles. The configuration page converts to `.mdx` to import the component. The authentication page drops its duplicated tables and links to the configuration reference instead.

**Tech Stack:** Astro 6 (MDX integration), Tailwind CSS v4, existing CSS variable theming

---

### Task 1: Install MDX Integration

Astro requires `@astrojs/mdx` to use components inside content collection pages. Currently only `.md` is supported.

**Files:**
- Modify: `website/package.json`
- Modify: `website/astro.config.ts`
- Modify: `website/src/content.config.ts`
- Modify: `website/src/pages/[...slug].md.ts`

- [ ] **Step 1: Install the MDX integration**

Run:
```bash
cd website && npm install @astrojs/mdx
```

- [ ] **Step 2: Add MDX to Astro config**

In `website/astro.config.ts`, add the import at the top:

```ts
import mdx from "@astrojs/mdx";
```

Then add `mdx()` to the `integrations` array:

```ts
integrations: [sitemap(), mdx()],
```

- [ ] **Step 3: Update content config glob to include MDX files**

In `website/src/content.config.ts`, change the glob pattern from:

```ts
pattern: "!(test_*)*.md",
```

to:

```ts
pattern: "!(test_*)*.{md,mdx}",
```

- [ ] **Step 4: Update raw markdown endpoint to handle MDX files**

In `website/src/pages/[...slug].md.ts`, the `GET` handler reads `${props.id}.md` from disk. Update it to try `.mdx` first, then `.md`:

```ts
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { docsDir, getDocPaths } from "../lib/docs";

export async function getStaticPaths() {
  const docs = await getDocPaths();
  return docs.map((doc) => ({
    params: { slug: doc.id },
    props: { id: doc.id },
  }));
}

export async function GET({ props }: { props: { id: string } }) {
  let content: string;
  try {
    content = await readFile(join(docsDir, `${props.id}.mdx`), "utf-8");
  } catch {
    content = await readFile(join(docsDir, `${props.id}.md`), "utf-8");
  }
  return new Response(content, {
    headers: { "Content-Type": "text/markdown; charset=utf-8" },
  });
}
```

- [ ] **Step 5: Verify the website still builds**

Run:
```bash
cd website && npm run build
```

Expected: Clean build, no errors. All existing pages render unchanged.

- [ ] **Step 6: Commit**

```bash
git add website/package.json website/package-lock.json website/astro.config.ts website/src/content.config.ts website/src/pages/\[...slug\].md.ts
git commit -m "feat(website): add MDX integration for component-in-content support"
```

---

### Task 2: Create ConfigParam Component and CSS

**Files:**
- Create: `website/src/components/ConfigParam.astro`
- Modify: `website/src/styles/global.css`

- [ ] **Step 1: Create the ConfigParam component**

Create `website/src/components/ConfigParam.astro`:

```astro
---
interface Props {
  name: string
  flag?: string
  env?: string
  default?: string
  required?: boolean
  deprecated?: boolean
}

const { name, flag, env, default: defaultValue, required, deprecated } = Astro.props;
---

<div class:list={["config-param", { deprecated }]}>
  <div class="config-param-heading">
    <code>{name}</code>
    {required && <span class="config-param-badge required">required</span>}
    {deprecated && <span class="config-param-badge deprecated">deprecated</span>}
  </div>
  <div class="config-param-description">
    <slot />
  </div>
  {(flag || env || defaultValue !== undefined) && (
    <div class="config-param-meta">
      {flag && (
        <>
          <span class="config-param-label">Flag</span>
          <code>{flag}</code>
        </>
      )}
      {env && (
        <>
          <span class="config-param-label">Env var</span>
          <code>{env}</code>
        </>
      )}
      {defaultValue !== undefined && (
        <>
          <span class="config-param-label">Default</span>
          {defaultValue ? <code>{defaultValue}</code> : <span class="config-param-no-default">—</span>}
        </>
      )}
    </div>
  )}
</div>
```

- [ ] **Step 2: Add CSS to global.css**

In `website/src/styles/global.css`, add after the table styles section (after the `/* Tables — wrapped ... */` block and before the `/* Inline code */` comment):

```css
/* ── Config parameter cards ──────────────────────────────────────── */

.prose .config-param {
  @apply py-3.5 px-4 border border-border rounded-lg;
}
.prose .config-param + .config-param {
  @apply mt-1;
}
.prose .config-param.deprecated {
  @apply opacity-60;
}
.prose .config-param-heading {
  @apply flex items-baseline gap-2 mb-1.5;
}
.prose .config-param-heading > code {
  @apply text-[0.9375rem] font-semibold rounded-none px-0 py-0;
  background: none;
  color: var(--foreground);
}
.prose .config-param-badge {
  @apply text-[0.625rem] font-semibold uppercase tracking-wider leading-tight rounded px-1.5 py-0.5;
}
.prose .config-param-badge.required {
  color: oklch(0.55 0.15 25);
  border: 1px solid oklch(0.55 0.15 25 / 40%);
}
.dark .prose .config-param-badge.required {
  color: oklch(0.7 0.15 25);
  border: 1px solid oklch(0.7 0.15 25 / 40%);
}
.prose .config-param-badge.deprecated {
  @apply text-muted-foreground;
  border: 1px solid var(--border);
}
.prose .config-param-description {
  @apply text-sm leading-relaxed mb-2.5;
}
.prose .config-param-description > :first-child {
  @apply mt-0;
}
.prose .config-param-description > :last-child {
  @apply mb-0;
}
.prose .config-param-meta {
  @apply text-[0.8rem] text-muted-foreground;
  display: grid;
  grid-template-columns: 4.5rem 1fr;
  gap: 0.15rem 0.75rem;
}
.prose .config-param-label {
  @apply font-medium;
}
.prose .config-param-meta code {
  @apply text-[0.8rem] rounded-none px-0 py-0;
  background: none;
  color: var(--muted-foreground);
}
.prose .config-param-no-default {
  @apply italic;
}
```

- [ ] **Step 3: Verify the build still passes**

Run:
```bash
cd website && npm run build
```

Expected: Clean build. The component isn't used yet, but CSS should compile.

- [ ] **Step 4: Commit**

```bash
git add website/src/components/ConfigParam.astro website/src/styles/global.css
git commit -m "feat(website): add ConfigParam card component and styles"
```

---

### Task 3: Convert Configuration Page to MDX

Replace all parameter tables in `configuration.md` with `<ConfigParam>` instances. Non-parameter tables (health checks, operations level matrix) stay as markdown tables.

**Files:**
- Rename: `docs/configuration.md` → `docs/configuration.mdx`

- [ ] **Step 1: Rename the file**

```bash
mv docs/configuration.md docs/configuration.mdx
```

- [ ] **Step 2: Add the component import and replace the General Settings table**

At the top of `docs/configuration.mdx`, after the frontmatter closing `---`, add:

```mdx
import ConfigParam from '../website/src/components/ConfigParam.astro'
```

Then replace the General Settings table (the markdown table after `## General Settings`) with:

```mdx
<ConfigParam name="server.listen_addr" flag="-listen" env="CETACEAN_LISTEN_ADDR" default=":9000">
  HTTP server bind address.
</ConfigParam>

<ConfigParam name="server.base_path" flag="-base-path" env="CETACEAN_BASE_PATH">
  URL base path prefix for sub-path deployments (e.g., `/cetacean`).
</ConfigParam>

<ConfigParam name="docker.host" flag="-docker-host" env="CETACEAN_DOCKER_HOST" default="unix:///var/run/docker.sock">
  Docker socket URI.
</ConfigParam>

<ConfigParam name="prometheus.url" flag="-prometheus-url" env="CETACEAN_PROMETHEUS_URL">
  Prometheus base URL. Unset = metrics disabled.
</ConfigParam>

<ConfigParam name="logging.level" flag="-log-level" env="CETACEAN_LOG_LEVEL" default="info">
  Log level: `debug`, `info`, `warn`, `error`.
</ConfigParam>

<ConfigParam name="logging.format" flag="-log-format" env="CETACEAN_LOG_FORMAT" default="json">
  Log format: `json` or `text`.
</ConfigParam>

<ConfigParam name="server.pprof" flag="-pprof" env="CETACEAN_PPROF" default="false">
  Expose Go pprof endpoints at `/debug/pprof/`.
</ConfigParam>

<ConfigParam name="server.self_metrics" flag="-self-metrics" env="CETACEAN_SELF_METRICS" default="true">
  Expose Prometheus metrics at `/-/metrics`.
</ConfigParam>

<ConfigParam name="server.recommendations" flag="-recommendations" env="CETACEAN_RECOMMENDATIONS" default="true">
  Enable recommendation engine.
</ConfigParam>

<ConfigParam name="server.operations_level" flag="-operations-level" env="CETACEAN_OPERATIONS_LEVEL" default="1">
  Write operation tier: 0=read-only, 1=operational, 2=configuration, 3=impactful.
</ConfigParam>

<ConfigParam name="server.sse.batch_interval" flag="-sse-batch-interval" env="CETACEAN_SSE_BATCH_INTERVAL" default="100ms">
  SSE event batching window (Go duration).
</ConfigParam>

<ConfigParam name="server.cors.origins" flag="-cors-origins" env="CETACEAN_CORS_ORIGINS">
  Allowed CORS origins (comma-separated or `*`). Unset = CORS disabled.
</ConfigParam>

<ConfigParam name="server.trusted_proxies" flag="-trusted-proxies" env="CETACEAN_TRUSTED_PROXIES">
  Trusted reverse proxy CIDRs/IPs (comma-separated). Enables real client IP resolution.
</ConfigParam>

<ConfigParam name="storage.snapshot" flag="-snapshot" env="CETACEAN_SNAPSHOT" default="true">
  Enable disk persistence of swarm state.
</ConfigParam>

<ConfigParam name="storage.data_dir" flag="-data-dir" env="CETACEAN_DATA_DIR" default="./data">
  Directory for snapshot file.
</ConfigParam>

<ConfigParam name="tls.cert" flag="-tls-cert" env="CETACEAN_TLS_CERT">
  Server certificate path (PEM).
</ConfigParam>

<ConfigParam name="tls.key" flag="-tls-key" env="CETACEAN_TLS_KEY">
  Server private key path (PEM).
</ConfigParam>

<ConfigParam name="config" flag="-config" env="CETACEAN_CONFIG">
  Path to TOML config file.
</ConfigParam>
```

Keep the existing blockquote note about TLS cert and key below these cards.

- [ ] **Step 3: Replace the Authentication and Authorization Settings table**

Replace the single-row auth mode table with:

```mdx
<ConfigParam name="auth.mode" flag="-auth-mode" env="CETACEAN_AUTH_MODE" default="none">
  Auth provider: `none`, [`oidc`](#oidc), [`tailscale`](#tailscale), [`cert`](#client-certificates), [`headers`](#trusted-proxy-headers).
</ConfigParam>
```

- [ ] **Step 4: Replace the OIDC table**

Replace the OIDC parameter table with:

```mdx
<ConfigParam name="auth.oidc.issuer" flag="-auth-oidc-issuer" env="CETACEAN_AUTH_OIDC_ISSUER" required>
  OIDC issuer URL.
</ConfigParam>

<ConfigParam name="auth.oidc.client_id" flag="-auth-oidc-client-id" env="CETACEAN_AUTH_OIDC_CLIENT_ID" required>
  OAuth 2.0 client ID.
</ConfigParam>

<ConfigParam name="auth.oidc.client_secret" flag="-auth-oidc-client-secret" env="CETACEAN_AUTH_OIDC_CLIENT_SECRET" required>
  OAuth 2.0 client secret.
</ConfigParam>

<ConfigParam name="auth.oidc.redirect_url" flag="-auth-oidc-redirect-url" env="CETACEAN_AUTH_OIDC_REDIRECT_URL" required>
  Callback URL (HTTPS required, loopback exempt).
</ConfigParam>

<ConfigParam name="auth.oidc.scopes" flag="-auth-oidc-scopes" env="CETACEAN_AUTH_OIDC_SCOPES" default="openid,profile,email">
  Comma-separated scopes.
</ConfigParam>

<ConfigParam name="auth.oidc.session_key" flag="-auth-oidc-session-key" env="CETACEAN_AUTH_OIDC_SESSION_KEY" default="random">
  Hex-encoded 32-byte HMAC key; random per-process if unset.
</ConfigParam>
```

- [ ] **Step 5: Replace the Tailscale table**

Replace the Tailscale parameter table with:

```mdx
<ConfigParam name="auth.tailscale.mode" flag="-auth-tailscale-mode" env="CETACEAN_AUTH_TAILSCALE_MODE" default="local">
  `local` or `tsnet`.
</ConfigParam>

<ConfigParam name="auth.tailscale.authkey" flag="-auth-tailscale-authkey" env="CETACEAN_AUTH_TAILSCALE_AUTHKEY">
  Auth key for node enrollment (tsnet only).
</ConfigParam>

<ConfigParam name="auth.tailscale.hostname" flag="-auth-tailscale-hostname" env="CETACEAN_AUTH_TAILSCALE_HOSTNAME" default="cetacean">
  Tailscale node hostname.
</ConfigParam>

<ConfigParam name="auth.tailscale.state_dir" flag="-auth-tailscale-state-dir" env="CETACEAN_AUTH_TAILSCALE_STATE_DIR">
  State directory for tsnet.
</ConfigParam>

<ConfigParam name="auth.tailscale.capability" flag="-auth-tailscale-capability" env="CETACEAN_AUTH_TAILSCALE_CAPABILITY">
  App capability key for group extraction.
</ConfigParam>
```

- [ ] **Step 6: Replace the Client Certificates table**

Replace the Client Certificates parameter table with:

```mdx
<ConfigParam name="auth.cert.ca" flag="-auth-cert-ca" env="CETACEAN_AUTH_CERT_CA" required>
  Path to CA bundle (PEM).
</ConfigParam>
```

Keep the existing blockquote note about `-tls-cert` and `-tls-key` below.

- [ ] **Step 7: Replace the Trusted Proxy Headers table**

Replace the Trusted Proxy Headers parameter table with:

```mdx
<ConfigParam name="auth.headers.subject" flag="-auth-headers-subject" env="CETACEAN_AUTH_HEADERS_SUBJECT" required>
  Header name for subject.
</ConfigParam>

<ConfigParam name="auth.headers.name" flag="-auth-headers-name" env="CETACEAN_AUTH_HEADERS_NAME">
  Header name for display name.
</ConfigParam>

<ConfigParam name="auth.headers.email" flag="-auth-headers-email" env="CETACEAN_AUTH_HEADERS_EMAIL">
  Header name for email.
</ConfigParam>

<ConfigParam name="auth.headers.groups" flag="-auth-headers-groups" env="CETACEAN_AUTH_HEADERS_GROUPS">
  Header name for groups (comma-separated).
</ConfigParam>

<ConfigParam name="auth.headers.secret_header" flag="-auth-headers-secret-header" env="CETACEAN_AUTH_HEADERS_SECRET_HEADER">
  Header name for shared secret.
</ConfigParam>

<ConfigParam name="auth.headers.secret_value" flag="-auth-headers-secret-value" env="CETACEAN_AUTH_HEADERS_SECRET_VALUE">
  Secret value (required if secret header set).
</ConfigParam>

<ConfigParam name="auth.headers.trusted_proxies" flag="-auth-headers-trusted-proxies" env="CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES" deprecated>
  Use `server.trusted_proxies` instead.
</ConfigParam>
```

Keep the existing blockquote note below.

- [ ] **Step 8: Verify the import path resolves correctly**

The import `from '../website/src/components/ConfigParam.astro'` is relative from `docs/` to `website/src/`. Astro content collections resolve imports relative to the content file. Verify this works by building:

```bash
cd website && npm run build
```

If the import path doesn't resolve, adjust to use the `@/` alias instead:

```mdx
import ConfigParam from '@/components/ConfigParam.astro'
```

The `@/` alias maps to `website/src/` in the Astro project. Content collection files may or may not support this alias — the build will tell us which form works.

- [ ] **Step 9: Visually verify the configuration page**

```bash
cd website && npm run dev
```

Open `http://localhost:4321/configuration/` and verify:
- Cards render with TOML key headings, badges, descriptions, and metadata grids
- Required badges appear in red on the correct parameters
- Deprecated badge appears on `auth.headers.trusted_proxies`
- Default values display correctly
- The health check endpoints table and operations level matrix still render as regular tables
- Dark mode renders correctly (toggle theme)
- No horizontal scrolling — cards fit within the prose width

- [ ] **Step 10: Commit**

```bash
git add docs/configuration.mdx
git rm docs/configuration.md
git commit -m "feat(website): replace config parameter tables with ConfigParam cards"
```

---

### Task 4: Simplify Authentication Page

Remove the duplicated parameter tables from `authentication.md` and replace them with links to the Configuration reference.

**Files:**
- Modify: `docs/authentication.md`

- [ ] **Step 1: Replace the OIDC Configuration table**

In the `#### Configuration` section under `### OIDC`, replace the parameter table with:

```markdown
See [OIDC configuration](configuration#oidc) for all parameters.
```

Keep everything else in the OIDC section (Browser Flow, Machine Flow, Session Persistence, Logout, IdP Setup Examples).

- [ ] **Step 2: Replace the Tailscale Configuration table**

In the `#### Configuration` section under `### Tailscale`, replace the parameter table with:

```markdown
See [Tailscale configuration](configuration#tailscale) for all parameters.
```

Keep the "Choosing a Mode" comparison table — it's genuinely tabular (comparing two modes side by side), not a parameter list.

- [ ] **Step 3: Replace the Client Certificates Configuration table**

In the `#### Configuration` section under `### Client Certificates (mTLS)`, replace the parameter table (which has cert CA + TLS cert + TLS key) with:

```markdown
See [Client certificate configuration](configuration#client-certificates) for CA settings and [TLS settings](configuration#general-settings) for server certificate and key.
```

Keep the code example and identity extraction explanation below.

- [ ] **Step 4: Replace the Trusted Proxy Headers Configuration table**

In the `#### Configuration` section under `### Trusted Proxy Headers`, replace the parameter table with:

```markdown
See [Trusted proxy header configuration](configuration#trusted-proxy-headers) for all parameters.
```

Keep the security explanation, code examples, and proxy configuration examples.

- [ ] **Step 5: Verify links resolve correctly**

```bash
cd website && npm run dev
```

Open `http://localhost:4321/authentication/` and click each "See ... configuration" link. Verify they navigate to the correct section on the Configuration page with the parameter cards visible.

- [ ] **Step 6: Commit**

```bash
git add docs/authentication.md
git commit -m "docs: replace duplicated auth parameter tables with links to config reference"
```

---

### Task 5: Final Verification

- [ ] **Step 1: Full build**

```bash
cd website && npm run build
```

Expected: Clean build, no warnings about missing pages or broken links.

- [ ] **Step 2: Visual check of all affected pages**

```bash
cd website && npm run dev
```

Check:
- `http://localhost:4321/configuration/` — all parameter cards, tables that stayed as tables, section anchors
- `http://localhost:4321/authentication/` — links to config reference work, no orphaned table fragments
- Search still works (the Pagefind index includes content from ConfigParam cards)

- [ ] **Step 3: Commit any fixes if needed**
