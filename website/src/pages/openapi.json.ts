import type { APIContext } from "astro";
import { buildSiteSpec } from "@/lib/openapi";

/**
 * Only `astro dev` sees the headers a route sets here.
 *
 * A static build keeps a response's body and discards everything else: Astro
 * retains `response.headers` only for an adapter declaring `staticHeaders`, and
 * this site has no adapter — it writes `dist/openapi.json` and GitHub Pages
 * decides how to serve it, typing the file from its own extension map and
 * attaching its own `Cache-Control` (and an `Access-Control-Allow-Origin: *`,
 * which is why nothing here needs to ask for one). So `Content-Type` is set for
 * the dev server's benefit and nothing else is set at all, rather than writing
 * down a caching and CORS policy that no deployed request will ever see.
 */
export async function GET({ site }: APIContext) {
  return new Response(JSON.stringify(await buildSiteSpec(site), null, 2), {
    headers: { "Content-Type": "application/json" },
  });
}
