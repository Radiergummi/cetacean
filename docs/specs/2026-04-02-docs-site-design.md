# Docs Site Design

Build a static documentation site using Astro that visually matches the Cetacean dashboard. Replaces the current
Jekyll + GitHub Pages minimal theme.

## Structure

```
website/                     # Astro project
  src/
    layouts/
      DocsLayout.astro       # Sidebar + TOC + content area
    components/
      Sidebar.astro          # Left sidebar navigation grouped by category
      TableOfContents.astro   # Right-side in-page heading links
      Header.astro           # Top bar: logo, Docs/API/GitHub links, theme toggle
      Hero.astro             # Homepage hero: logo, tagline, CTA buttons
      ThemeToggle.astro      # Light/dark toggle, persists to localStorage
      Prose.astro            # Wrapper applying typography styles to Markdown content
    pages/
      index.astro            # Homepage with Hero
      [...slug].astro        # Dynamic route rendering docs content
    styles/
      global.css             # Imports Cetacean's CSS variables, Tailwind base
  content.config.ts          # Astro content collection pointing at ../docs/*.md
  astro.config.ts
  tailwind.config.ts
  package.json
```

Content lives in the existing `docs/` directory — plain Markdown with frontmatter. The Astro project in `website/`
consumes it via a content collection. No content is duplicated.

## Layout

Three-column layout matching option A from brainstorming:

- **Left sidebar** (fixed, ~200px): Navigation grouped by `category` frontmatter. Groups: "Guide" and "Reference".
  Active page highlighted. Collapsible on mobile (hamburger menu).
- **Center content** (~720px max-width): Rendered Markdown with prose typography.
- **Right TOC** (fixed, ~160px): Auto-generated from page headings (h2/h3). Active heading highlighted on scroll.
  Hidden on narrow viewports.

## Homepage

Hero layout with:
- Cetacean logo/icon
- Project name and tagline (from `docs/index.md` frontmatter description)
- Two CTA buttons: "Get Started" (links to getting-started) and "GitHub" (external)
- Below the hero, the sidebar is present for navigation

## Theming

Import Cetacean's design tokens from `frontend/src/index.css`:
- oklch color variables (background, foreground, card, muted, border, accent, link, etc.)
- Geist Variable font (`@fontsource-variable/geist`)
- Border radius tokens
- Dark mode via `.dark` class on `<html>`

The docs site has its own `tailwind.config.ts` that maps these CSS variables to Tailwind's color/font/radius scales,
same approach as the dashboard's `@theme inline` block.

Theme toggle in the header cycles light/dark/system, persisted to localStorage. Respects `prefers-color-scheme` on
first visit.

## Content Pipeline

Astro content collection in `content.config.ts`:

```ts
import { defineCollection, z } from "astro:content";
import { glob } from "astro/loaders";

const docs = defineCollection({
  loader: glob({ pattern: "*.md", base: "../docs" }),
  schema: z.object({
    title: z.string(),
    description: z.string(),
    category: z.enum(["overview", "guide", "reference"]),
    tags: z.array(z.string()),
  }),
});

export const collections = { docs };
```

The `[...slug].astro` page fetches entries from the collection, renders them with `DocsLayout`, and generates static
pages.

## Sidebar Order

Hardcoded order within each group (not alphabetical — editorial ordering matters):

**Guide:** Getting Started, Monitoring, Authentication, Authorization, Dashboard, Integrations, Recommendations

**Reference:** Configuration, API Reference

The `overview` category (index.md) is not shown in the sidebar — it's the homepage.

## Code Blocks

Astro's built-in Shiki integration. Use a neutral theme that works in both light and dark modes (e.g., `github-dark`
for dark, `github-light` for light). Code blocks get a copy button via a small client-side script.

## Prose Styling

The `Prose` component applies Tailwind Typography-like styles to rendered Markdown:
- Headings, paragraphs, lists, blockquotes, tables, code blocks, horizontal rules
- Link styling using `--link` color variable
- Table styling matching Cetacean's alternating row pattern
- Proper spacing and max-width for readability

Rather than using `@tailwindcss/typography`, hand-write the prose styles to match the dashboard's exact aesthetic
(border colors, table striping, code block backgrounds, etc.).

## Changelog

The build step copies `CHANGELOG.md` into the content collection (same approach as the current Jekyll workflow). It
appears in the sidebar under a standalone "Changelog" link below the groups.

## Deployment

GitHub Actions workflow replaces the current Jekyll one:

```yaml
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: 22
      - run: cd website && npm ci && npm run build
      - uses: actions/upload-pages-artifact@v3
        with:
          path: website/dist
  deploy:
    needs: build
    runs-on: ubuntu-latest
    environment:
      name: github-pages
      url: ${{ steps.deployment.outputs.page_url }}
    steps:
      - id: deployment
        uses: actions/deploy-pages@v4
```

Triggered on pushes to main that change `docs/**`, `website/**`, `CHANGELOG.md`, or the workflow file.

## Not in Scope

- Search (10 pages — sidebar is sufficient)
- Versioned docs (single version, matches main branch)
- Blog or announcement section
- Screenshot/image embedding (can add later)
- RSS/Atom feed
