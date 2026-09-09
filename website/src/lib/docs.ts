import { getCollection } from "astro:content";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

export { slugify } from "./slug";

export const docsDir = resolve("../docs");
export const changelogPath = resolve("../CHANGELOG.md");

/**
 * The most recent released version, read from the changelog at build time so
 * the site cannot fall behind a release.
 */
export function latestVersion(): string {
  const raw = readFileSync(changelogPath, "utf-8");
  const match = raw.match(/^## \[(?!Unreleased)([^\]]+)]\s*-\s*\d{4}-\d{2}-\d{2}/m);

  if (!match) {
    throw new Error("no released version found in CHANGELOG.md");
  }

  return `v${match[1]}`;
}

/**
 * The operations levels, by index: a tool's `level` is its position here. Shared
 * by the tool cards and the raw Markdown route, which would otherwise each keep
 * their own copy of the four names.
 */
export const operationsLevels = ["read-only", "operational", "configuration", "impactful"] as const;

export async function getDocPaths() {
  const docs = await getCollection("docs");

  return docs.filter(({ data: { category } }) => category !== "overview");
}
