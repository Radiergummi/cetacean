import { defineMiddleware } from "astro:middleware";
import { readFileSync, existsSync } from "node:fs";
import { resolve } from "node:path";

const spaPath = [
  resolve("public/demo/index.html"),
  resolve("../website/public/demo/index.html"),
].find(existsSync) ?? "";

function readSpaHTML(): string {
  if (!spaPath) return "";
  try {
    return readFileSync(spaPath, "utf-8");
  } catch {
    return "";
  }
}

function isSPARoute(pathname: string): boolean {
  if (pathname === "/demo/") {
    return true;
  }

  return (
    pathname.startsWith("/demo/") &&
    !pathname.startsWith("/demo/assets/") &&
    !pathname.endsWith(".js") &&
    !pathname.endsWith(".css") &&
    !pathname.endsWith(".svg") &&
    !pathname.endsWith(".png") &&
    !pathname.endsWith(".ico") &&
    !pathname.endsWith(".json") &&
    !pathname.endsWith(".html")
  );
}

/**
 * Serve the demo SPA for /demo/ and all deep links under /demo/*.
 * Static assets (JS, CSS, images, service worker) pass through to Astro/Vite.
 */
export const onRequest = defineMiddleware(({ request, url }, next) => {
  if (url.pathname === "/demo") {
    return Response.redirect(new URL("/demo/", url), 302);
  }

  const html = readSpaHTML();
  if (html && isSPARoute(url.pathname) && request.headers.get("accept")?.includes("text/html")) {
    return new Response(html, {
      status: 200,
      headers: { "Content-Type": "text/html; charset=utf-8" },
    });
  }

  return next();
});
