# API Explorer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Embed the Scalar OpenAPI browser on the docs website at `/api/explorer` and trim the existing API page into a focused guide.

**Architecture:** A new Astro page loads Scalar from CDN against the OpenAPI spec (copied into `public/` at build time). The existing `docs/api.md` is trimmed to remove endpoint tables that Scalar renders interactively, keeping conceptual guide material.

**Tech Stack:** Astro 6, Scalar API Reference (CDN), existing DocsLayout

---

### Task 1: Build-Time Spec Copy and Gitignore

**Files:**
- Modify: `website/package.json`
- Modify: `website/.gitignore`

- [ ] **Step 1: Add pre-scripts to package.json**

In `website/package.json`, add `prebuild` and `predev` scripts that copy the OpenAPI spec:

```json
"prebuild": "cp ../api/openapi.yaml public/openapi.yaml",
"predev": "cp ../api/openapi.yaml public/openapi.yaml",
```

These go before the existing `postbuild` script in the `scripts` object.

- [ ] **Step 2: Add gitignore entry**

Append to `website/.gitignore`:

```
# OpenAPI spec (copied from api/openapi.yaml at build time)
public/openapi.yaml
```

- [ ] **Step 3: Verify the copy works**

```bash
cd website && npm run prebuild && ls -la public/openapi.yaml
```

Expected: File exists, same size as `../api/openapi.yaml`.

- [ ] **Step 4: Verify the build still works**

```bash
cd website && npm run build
```

Expected: Clean build. The `prebuild` script runs automatically before `astro build`.

- [ ] **Step 5: Commit**

```bash
git add website/package.json website/.gitignore
git commit -m "build(website): copy OpenAPI spec into public/ at build time"
```

---

### Task 2: Create the Scalar Explorer Page

**Files:**
- Create: `website/src/pages/api/explorer.astro`

- [ ] **Step 1: Create the explorer page**

Create `website/src/pages/api/explorer.astro`:

```astro
---
import Head from "../../components/Head.astro";
import Header from "../../components/Header.astro";
import Sidebar from "../../components/Sidebar.astro";
import "../../styles/global.css";
---
<html lang="en">
  <head>
    <Head title="API Reference" description="Interactive API reference for the Cetacean REST API." />
    <style>
      .scalar-container {
        flex: 1;
        min-width: 0;
        min-height: calc(100vh - 4rem);
      }
    </style>
  </head>
  <body class="min-h-screen">
    <Header currentPath="/api/explorer" />
    <div class="mx-auto flex max-w-[90rem]">
      <Sidebar currentSlug="api/explorer" />
      <div class="scalar-container">
        <script
          id="api-reference"
          data-url="/openapi.yaml"
          type="application/json"
        >
          {JSON.stringify({
            theme: "none",
            hideDownloadButton: false,
            hideDarkModeToggle: true,
          })}
        </script>
        <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
      </div>
    </div>
  </body>
</html>
```

This page uses a custom layout (not DocsLayout) because Scalar needs the full content width — no `.prose` max-width, no table-of-contents sidebar. It still includes the Header and Sidebar for consistent navigation.

The `theme: "none"` setting tells Scalar not to apply its own theme, which lets it inherit page styles. `hideDarkModeToggle: true` avoids a second theme toggle (the site already has one).

- [ ] **Step 2: Verify the page builds**

```bash
cd website && npm run build
```

Expected: Clean build, the explorer page appears in the output.

- [ ] **Step 3: Verify the page renders**

```bash
cd website && npm run dev
```

Open `http://localhost:4321/api/explorer` (or whichever port). Verify:
- Scalar loads and shows the OpenAPI spec
- The sidebar is visible with navigation
- The header is present
- Scalar fills the content area

- [ ] **Step 4: Commit**

```bash
git add website/src/pages/api/explorer.astro
git commit -m "feat(website): add Scalar API explorer page at /api/explorer"
```

---

### Task 3: Update Navigation

**Files:**
- Modify: `website/src/lib/navigation.ts`

- [ ] **Step 1: Update the sidebar navigation**

In `website/src/lib/navigation.ts`, change the Reference section items from:

```ts
{ slug: "api", title: "API Reference" },
{ slug: "api/schema", title: "Schema Reference" },
```

to:

```ts
{ slug: "api", title: "API Guide" },
{ slug: "api/explorer", title: "API Reference" },
{ slug: "api/schema", title: "Schema Reference" },
```

- [ ] **Step 2: Verify the build**

```bash
cd website && npm run build
```

- [ ] **Step 3: Commit**

```bash
git add website/src/lib/navigation.ts
git commit -m "feat(website): add API Reference (explorer) to sidebar navigation"
```

---

### Task 4: Trim the API Guide

Remove the endpoint reference tables from `docs/api.md` (Scalar covers these) and add a callout linking to the explorer.

**Files:**
- Modify: `docs/api.md`

- [ ] **Step 1: Change the page title**

In the frontmatter of `docs/api.md`, change:

```yaml
title: API Reference
```

to:

```yaml
title: API Guide
```

Also change the first heading from `# Cetacean API Reference` to `# Cetacean API Guide`.

- [ ] **Step 2: Replace the Endpoint Reference section with a callout**

Delete everything from line 494 (`## Endpoint Reference`) through line 1047 (the end of the `### API Documentation` code block, just before `## Rate Limits`).

In its place, add:

```markdown
## Endpoints

For the complete endpoint reference with request/response schemas and try-it-out, see the interactive [API Reference](api/explorer).
```

Keep everything below intact: `## Rate Limits`, `## Self-Discovery`, `## Request ID`.

- [ ] **Step 3: Update the OpenAPI spec reference at the top**

Near the top of the file (line 17), change:

```markdown
The machine-readable OpenAPI spec is available at [`/api`](#api-documentation).
```

to:

```markdown
The machine-readable OpenAPI spec is available at `/api` (JSON). For an interactive endpoint browser, see the [API Reference](api/explorer).
```

- [ ] **Step 4: Verify the build**

```bash
cd website && npm run build
```

Expected: Clean build. The API Guide page is shorter but retains all conceptual content.

- [ ] **Step 5: Visually verify**

```bash
cd website && npm run dev
```

Check:
- `http://localhost:4321/api` — title is "API Guide", no endpoint tables, callout link to explorer works
- `http://localhost:4321/api/explorer` — Scalar loads with full endpoint reference
- Rate Limits, Self-Discovery, and Request ID sections still render at the bottom of the guide
- Sidebar shows "API Guide", "API Reference", "Schema Reference" in order

- [ ] **Step 6: Commit**

```bash
git add docs/api.md
git commit -m "docs: trim API page to guide, link to Scalar explorer for endpoint reference"
```
