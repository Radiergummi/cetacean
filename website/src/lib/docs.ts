import { getCollection } from "astro:content";
import { resolve } from "node:path";

export const docsDir = resolve("../docs");

export async function getDocPaths() {
  const docs = await getCollection("docs");
  return docs.filter((doc) => doc.data.category !== "overview");
}
