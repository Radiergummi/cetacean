import type { APIContext } from "astro";
import { readFileSync } from "node:fs";
import { marked } from "marked";
import { changelogPath } from "../lib/docs";

interface Release {
  version: string;
  date: string;
  html: string;
}

async function parseReleases(raw: string, limit = 20): Promise<Release[]> {
  const sections = raw.split(/^## /m).slice(1);
  const releases: Release[] = [];

  for (const section of sections) {
    const headerMatch = section.match(/^\[([^\]]+)\]\s*-\s*(\d{4}-\d{2}-\d{2})/);
    if (!headerMatch || headerMatch[1] === "Unreleased") continue;

    const newlineIndex = section.indexOf("\n");
    const body = section.slice(newlineIndex + 1).trim();
    const html = await marked.parse(body);

    releases.push({ version: headerMatch[1], date: headerMatch[2], html });
    if (releases.length >= limit) break;
  }

  return releases;
}

function escapeCdata(html: string): string {
  return html.replace(/]]>/g, "]]]]><![CDATA[>");
}

export async function GET(context: APIContext) {
  const raw = readFileSync(changelogPath, "utf-8");
  const releases = await parseReleases(raw);
  const site = context.site?.origin ?? "https://cetacean.mazetti.me";
  const updated = releases[0]?.date || new Date().toISOString().slice(0, 10);

  const entries = releases.map((release) => `  <entry>
    <title>Cetacean ${release.version}</title>
    <id>tag:cetacean.mazetti.me,${release.date}:release/${release.version}</id>
    <link href="${site}/changelog" rel="alternate" />
    <updated>${release.date}T00:00:00Z</updated>
    <content type="html"><![CDATA[${escapeCdata(release.html)}]]></content>
  </entry>`).join("\n");

  const atom = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Cetacean Releases</title>
  <subtitle>Release notes for the Cetacean Docker Swarm dashboard</subtitle>
  <id>${site}/changelog</id>
  <link href="${site}/changelog" rel="alternate" />
  <link href="${site}/changelog.atom" rel="self" type="application/atom+xml" />
  <updated>${updated}T00:00:00Z</updated>
  <author><name>Cetacean</name></author>
${entries}
</feed>`;

  return new Response(atom, {
    headers: { "Content-Type": "application/atom+xml; charset=utf-8" },
  });
}
