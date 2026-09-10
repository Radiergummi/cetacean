import { readFile } from "node:fs/promises";
import type { APIContext } from "astro";
import { changelogPath } from "@/lib/docs";
import { markdownResponse } from "@/lib/markdown";

export async function GET({ site }: APIContext) {
  return markdownResponse(await readFile(changelogPath, "utf-8"), "/changelog", site);
}
