/**
 * Reads the base path from the <base> tag injected by the Go server.
 * Returns "" when running at root, or "/cetacean" (no trailing slash)
 * when running under a sub-path.
 *
 * Only reads from an explicit <base href="..."> element — ignores
 * document.baseURI which falls back to the page URL when no <base> tag
 * exists (causing incorrect paths in the Vite dev server).
 */
function detectBasePath(): string {
  try {
    const baseElement = document.querySelector("base");

    if (!baseElement) {
      return "";
    }

    const base = new URL(baseElement.href, window.location.origin);

    return base.pathname.replace(/\/+$/, "");
  } catch {
    return "";
  }
}

const cachedBasePath = detectBasePath();

/**
 * The configured base path, e.g. "" or "/cetacean".
 * No trailing slash.
 */
export const basePath = cachedBasePath;

/**
 * Prepends the base path to an absolute path.
 * apiPath("/nodes") → "/cetacean/nodes" (or "/nodes" at root).
 */
export function apiPath(path: string): string {
  if (!cachedBasePath) {
    return path;
  }

  return cachedBasePath + path;
}

/**
 * Removes the base path from an absolute path a response carries, so a server
 * running under a sub-path yields the same route the SPA uses.
 * stripBasePath("/cetacean/nodes/n1") → "/nodes/n1".
 */
export function stripBasePath(path: string): string {
  if (cachedBasePath && path.startsWith(cachedBasePath + "/")) {
    return path.slice(cachedBasePath.length);
  }

  return path;
}
