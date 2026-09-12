import type { APIContext } from "astro";
import { optionalPages, siteResources, siteSections } from "@/lib/manifest";
import { generatedPage } from "@/lib/pages";

/**
 * `llms.txt`, as https://llmstxt.org describes it: an H1, a blockquote summary,
 * optional prose, then H2 sections of `- [name](url): notes` links.
 *
 * Generated from the same manifest as `openapi.json`, in the sidebar's order, so
 * it cannot fall behind the navigation. Links point at the Markdown
 * representation of each page rather than the HTML: an agent following one gets
 * the page, not a shell of markup around it.
 */
export async function GET({ site }: APIContext) {
  const url = (path: string) => new URL(path, site).href;
  const sections = await siteSections();

  const lines = [
    "# Cetacean",
    "",
    `> ${generatedPage("/").description}`,
    "",
    "Cetacean is a single Go binary that reads a Docker Swarm cluster through the Docker socket, caches its state in memory, and serves it as a live dashboard, a REST API and an MCP server.",
    "",
    "Every page on this site has a Markdown representation at the same path with `.md` appended, which is what the links below point at. The site's own URLs are described by an OpenAPI document at " +
      url("/openapi.json") +
      "; the REST API of Cetacean itself is described separately at " +
      url("/api/openapi.yaml") +
      ".",
  ];

  for (const { label, pages } of sections) {
    lines.push("", `## ${label}`, "");

    for (const { description, markdown, path, title } of pages) {
      lines.push(`- [${title}](${url(markdown ?? path)}): ${description}`);
    }
  }

  lines.push("", "## Optional", "");

  for (const { description, markdown, path, title } of optionalPages) {
    lines.push(`- [${title}](${url(markdown ?? path)}): ${description}`);
  }

  for (const { description, path, title } of siteResources) {
    // Not this file. An agent reading it has already found it.
    if (path === "/llms.txt") {
      continue;
    }

    lines.push(`- [${title}](${url(path)}): ${description}`);
  }

  return new Response(lines.join("\n") + "\n", {
    headers: { "Content-Type": "text/plain; charset=utf-8" },
  });
}
