import { getCollection } from "astro:content";
import { sidebarGroups } from "./navigation";
import { generatedPage } from "./pages";

export interface SitePage {
  path: string;
  title: string;
  description: string;
  markdown?: string;
}

export interface SiteSection {
  label: string;
  pages: SitePage[];
}

/**
 * Every documentation page, grouped and ordered the way the sidebar presents
 * them, with the home page ahead of the rest.
 *
 * The sidebar is the order a reader is offered, so it is the order an agent is
 * offered too. A sidebar entry that is neither a doc in the collection nor a
 * page in `generatedPages` throws, so a page cannot be added to the navigation
 * and silently left out of `llms.txt` and `openapi.json`.
 */
export async function siteSections(): Promise<SiteSection[]> {
  const docs = await getCollection("docs");
  const bySlug = new Map(docs.map((doc) => [doc.id, doc]));

  const sections = sidebarGroups.map(({ items, label }) => ({
    label,
    pages: items.map(({ slug }): SitePage => {
      const doc = bySlug.get(slug);

      if (!doc) {
        return generatedPage(`/${slug}`);
      }

      return {
        path: `/${slug}`,
        title: doc.data.title,
        description: doc.data.description,
        markdown: `/${slug}.md`,
      };
    }),
  }));

  sections[0].pages.unshift(generatedPage("/"));

  return sections;
}

/**
 * Pages the sidebar does not carry. The changelog is reached from the header,
 * and reads as reference an agent can skip rather than documentation.
 */
export const optionalPages: SitePage[] = [generatedPage("/changelog")];

/**
 * Every page this site serves, in the order it presents them. `openapi.json`
 * enumerates this; `llms.txt` renders the same pages grouped.
 */
export async function allPages(): Promise<SitePage[]> {
  const sections = await siteSections();

  return [...sections.flatMap(({ pages }) => pages), ...optionalPages];
}

/**
 * One machine-readable resource this site serves.
 */
export interface SiteResource {
  path: string;
  mediaType: string;
  title: string;
  description: string;
}

/**
 * The resources that are not pages: feeds, specs, and the crawler files.
 *
 * `/api/openapi.yaml` is the API of the product being documented; the site's own
 * description lives at the root, in both formats, so an agent probing either
 * `/openapi.json` or `/openapi.yaml` learns what this origin actually serves
 * rather than reading 94 Swarm endpoints as though they were here.
 */
export const siteResources: SiteResource[] = [
  {
    path: "/openapi.json",
    mediaType: "application/json",
    title: "This site's OpenAPI description",
    description: "Every URL this documentation site serves, and what it responds with.",
  },
  {
    path: "/openapi.yaml",
    mediaType: "application/yaml",
    title: "This site's OpenAPI description, as YAML",
    description: "The same description as /openapi.json.",
  },
  {
    path: "/api/openapi.yaml",
    mediaType: "application/yaml",
    title: "Cetacean API specification",
    description:
      "The OpenAPI specification of the Cetacean REST API — the product this site documents, not this site.",
  },
  {
    path: "/api/schema.jsonld",
    mediaType: "application/ld+json",
    title: "JSON-LD context",
    description: "The context document Cetacean API responses reference.",
  },
  {
    path: "/changelog.atom",
    mediaType: "application/atom+xml",
    title: "Release feed",
    description: "Cetacean releases, as an Atom feed.",
  },
  {
    path: "/llms.txt",
    mediaType: "text/plain",
    title: "llms.txt",
    description: "This site's contents, as llmstxt.org describes.",
  },
  {
    path: "/sitemap-index.xml",
    mediaType: "application/xml",
    title: "Sitemap index",
    description:
      "Points at the sitemap files listing every page, each with its last-modified date.",
  },
  {
    path: "/robots.txt",
    mediaType: "text/plain",
    title: "robots.txt",
    description: "Crawl policy. Everything here is open to crawlers.",
  },
];
