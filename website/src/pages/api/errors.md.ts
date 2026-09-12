import type { APIContext } from "astro";
import { errorsMarkdown } from "@/lib/errors";
import { markdownResponse } from "@/lib/markdown";

export function GET({ site }: APIContext) {
  return markdownResponse(errorsMarkdown(), "/api/errors", site);
}
