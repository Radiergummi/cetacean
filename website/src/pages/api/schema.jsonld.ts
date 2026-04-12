import { buildContext } from "../../lib/schema";

export function GET() {
  return new Response(JSON.stringify(buildContext(), null, 2), {
    headers: {
      "Content-Type": "application/ld+json",
      "Cache-Control": "public, max-age=86400",
      "Access-Control-Allow-Origin": "*",
    },
  });
}
