import { getCollection } from "astro:content";
import { resolve } from "node:path";

export const docsDir = resolve("../docs");
export const changelogPath = resolve("../CHANGELOG.md");

export function slugify(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^\w]+/g, "-")
    .replace(/^-|-$/g, "");
}

export async function getDocPaths() {
  const docs = await getCollection("docs");
  return docs.filter((doc) => doc.data.category !== "overview");
}
