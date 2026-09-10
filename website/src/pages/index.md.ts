import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import type { APIContext } from "astro";
import { markdownResponse } from "@/lib/markdown";

/**
 * The home page as Markdown. `[...slug].md.ts` cannot serve it: it drops
 * `category: overview` from its routes, and loosening that would also mint a
 * duplicate `/index` HTML page beside `/`.
 */
export async function GET({ site }: APIContext) {
  const body = await readFile(resolve("../docs/index.md"), "utf-8");

  return markdownResponse(body, "/", site);
}
