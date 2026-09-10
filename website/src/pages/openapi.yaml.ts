import type { APIContext } from "astro";
import { stringify } from "yaml";
import { buildSiteSpec } from "@/lib/openapi";

/**
 * The same document as `/openapi.json`. Both formats are served because an agent
 * probing an origin tries one or the other, and the root is where this site's
 * own description belongs — `/api/openapi.yaml` is the documented product's.
 *
 * The header is dev-only; see the note in `openapi.json.ts`.
 */
export async function GET({ site }: APIContext) {
  return new Response(stringify(await buildSiteSpec(site)), {
    headers: { "Content-Type": "application/yaml" },
  });
}
