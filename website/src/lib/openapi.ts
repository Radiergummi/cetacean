import { latestVersion } from "./docs";
import { allPages, siteResources } from "./manifest";

interface Route {
  path: string;
  mediaType: string;
  summary: string;
  description: string;
}

/**
 * `get` plus the path's words, so every operation has a stable, unique id
 * without a table to maintain: `/api/errors.md` is `getApiErrorsMd`.
 */
function operationId(path: string): string {
  const words = path.split(/[^A-Za-z0-9]+/).filter(Boolean);

  if (words.length === 0) {
    return "getHome";
  }

  return "get" + words.map((word) => word[0].toUpperCase() + word.slice(1)).join("");
}

function pathItem({ description, mediaType, path, summary }: Route) {
  return {
    get: {
      operationId: operationId(path),
      summary,
      description,
      responses: {
        "200": {
          description: summary,
          content: {
            [mediaType]: {
              schema: { type: mediaType.includes("json") ? "object" : "string" },
            },
          },
        },
      },
    },
  };
}

/**
 * An OpenAPI description of **this site** — every URL it serves and what comes
 * back — as distinct from `/api/openapi.yaml`, which describes the Cetacean REST
 * API this site documents. A quarter of agents reportedly orient themselves by
 * probing `/openapi.json`, and what they should find there is the origin they
 * are actually talking to, not the product it writes about.
 *
 * Built from the same manifest as `llms.txt`, so the two cannot disagree about
 * which pages exist.
 */
export async function buildSiteSpec(site: URL | undefined) {
  const routes: Route[] = [];

  for (const { description, markdown, path, title } of await allPages()) {
    routes.push({ path, mediaType: "text/html", summary: title, description });

    if (markdown) {
      routes.push({
        path: markdown,
        mediaType: "text/markdown",
        summary: `${title} (Markdown)`,
        description: `${description} The Markdown representation of ${path}.`,
      });
    }
  }

  for (const { description, mediaType, path, title } of siteResources) {
    routes.push({ path, mediaType, summary: title, description });
  }

  routes.push({
    path: "/demo/",
    mediaType: "text/html",
    summary: "Live demo",
    description: "The Cetacean dashboard running against a mocked cluster, in the browser.",
  });

  const paths: Record<string, unknown> = {};

  for (const route of routes) {
    paths[route.path] = pathItem(route);
  }

  return {
    openapi: "3.1.0",
    info: {
      title: "Cetacean documentation site",
      version: latestVersion(),
      description:
        "The URLs served by the Cetacean documentation site. Every page has a Markdown " +
        "representation at the same path with `.md` appended, and a summary of the whole site " +
        "is at `/llms.txt`.\n\nThis document describes the documentation site itself. The REST " +
        "API of Cetacean — the product documented here — is described separately by " +
        "`/api/openapi.yaml`.",
      "x-canonical": new URL("/openapi.json", site).href,
    },
    externalDocs: {
      description: "llms.txt",
      url: new URL("/llms.txt", site).href,
    },
    servers: [{ url: site ? site.origin : "/" }],
    paths,
  };
}
