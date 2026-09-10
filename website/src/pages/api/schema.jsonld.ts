import { buildContext } from "@/lib/schema.ts";

/** The header is dev-only; see the note in `pages/openapi.json.ts`. */
export function GET() {
  return new Response(JSON.stringify(buildContext(), null, 2), {
    headers: { "Content-Type": "application/ld+json" },
  });
}
