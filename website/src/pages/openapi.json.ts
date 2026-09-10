import type { APIContext } from "astro";
import { buildSiteSpec } from "@/lib/openapi";

export async function GET({ site }: APIContext) {
  return new Response(JSON.stringify(await buildSiteSpec(site), null, 2), {
    headers: {
      "Content-Type": "application/json",
      "Cache-Control": "public, max-age=86400",
      "Access-Control-Allow-Origin": "*",
    },
  });
}
