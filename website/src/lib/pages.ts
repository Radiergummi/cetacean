import { existsSync } from "node:fs";
import { resolve } from "node:path";
import { getLastModifiedSync, isShallowSync } from "./git";

/**
 * A page this site serves that is not a doc in the content collection: the home
 * page, the changelog, and the three references generated from Go source and
 * from `lib/schema.ts`.
 *
 * The title and description are the page's own copy — the `.astro` pages read
 * them from here rather than declaring them a second time, so `llms.txt` and
 * `openapi.json` cannot describe a page differently from the page itself.
 */
export interface GeneratedPage {
  path: string;
  title: string;
  description: string;

  /**
   * The Markdown representation of this page, if it has one. `/api/explorer` is
   * the exception: the machine-readable form of a spec playground is the spec.
   */
  markdown?: string;

  /**
   * The file whose last commit dates this page, relative to the repository
   * root. For a generated page that is the source it is generated from, not the
   * `.astro` file that renders it — a wording change to the page's own prose is
   * not what a reader means by "when did this last change".
   */
  sourcePath: string;
}

export const generatedPages: GeneratedPage[] = [
  {
    path: "/",
    title: "Home",
    description: "Real-time observability and management dashboard for Docker Swarm Mode clusters.",
    markdown: "/index.md",
    sourcePath: "docs/index.md",
  },
  {
    path: "/changelog",
    title: "Changelog",
    description: "Release history",
    markdown: "/changelog.md",
    sourcePath: "CHANGELOG.md",
  },
  {
    path: "/api/explorer",
    title: "API Reference",
    description: "Interactive API reference for the Cetacean REST API.",
    sourcePath: "api/openapi.yaml",
  },
  {
    path: "/api/schema",
    title: "Schema Reference",
    description: "JSON-LD vocabulary for the Cetacean API: types, properties, and their IRIs.",
    markdown: "/api/schema.md",
    sourcePath: "website/src/lib/schema.ts",
  },
  {
    path: "/api/errors",
    title: "Error Reference",
    description:
      "Every well-known error code the Cetacean API returns, with its HTTP status, what it means, and how to resolve it.",
    markdown: "/api/errors.md",
    sourcePath: "internal/api/errors.go",
  },
];

export function generatedPage(path: string): GeneratedPage {
  const page = generatedPages.find((candidate) => candidate.path === path);

  if (!page) {
    throw new Error(`no generated page for ${path}`);
  }

  return page;
}

/**
 * The Markdown representation of a generated page, for the pages that have one.
 */
export function markdownFor(path: string): string {
  const { markdown } = generatedPage(path);

  if (!markdown) {
    throw new Error(`${path} has no Markdown representation`);
  }

  return markdown;
}

const docsDir = resolve("../docs");

/**
 * The source file behind a doc slug, relative to the repository root. The
 * extension is the doc's own — `configuration` and `mcp-tools` reach for
 * components and are `.mdx`, the rest are `.md` — and is probed rather than
 * read from the content collection because `astro.config.ts` runs before the
 * content layer exists and still needs to date every page.
 */
function docsSourcePath(slug: string): string | null {
  for (const extension of [".md", ".mdx"]) {
    if (existsSync(resolve(docsDir, slug + extension))) {
      return `docs/${slug}${extension}`;
    }
  }

  return null;
}

/**
 * The source file behind a rendered path, relative to the repository root.
 */
export function sourceForPath(pathname: string): string {
  const page = generatedPages.find((candidate) => candidate.path === pathname);

  if (page) {
    return page.sourcePath;
  }

  const source = docsSourcePath(pathname.replace(/^\//, ""));

  if (!source) {
    throw new Error(`no source file for ${pathname}; add it to generatedPages`);
  }

  return source;
}

/**
 * The `lastmod` for one sitemap entry. Throwing rather than omitting the date is
 * the point: a page added without a source mapping fails `astro build` instead
 * of shipping a sitemap entry that never appears to change. `sitemapRequired`
 * in `astro.config.ts` is what makes a throw here fail the build — the sitemap
 * integration itself catches and logs whatever `serialize` raises.
 */
export function lastModifiedFor(url: string): string {
  if (isShallowSync()) {
    throw new Error(
      "a shallow clone dates every page from its one commit; check out with fetch-depth: 0",
    );
  }

  const { pathname } = new URL(url);
  const source = sourceForPath(pathname === "/" ? "/" : pathname.replace(/\/$/, ""));
  const modified = getLastModifiedSync(source);

  if (!modified) {
    throw new Error(`no commit in this clone touches ${source}; commit it before building`);
  }

  return modified.toISOString();
}
