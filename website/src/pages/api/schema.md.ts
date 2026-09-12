import type { APIContext } from "astro";
import { markdownResponse } from "@/lib/markdown";
import { schemaMarkdown } from "@/lib/schema";

export function GET({ site }: APIContext) {
  return markdownResponse(schemaMarkdown(), "/api/schema", site);
}
