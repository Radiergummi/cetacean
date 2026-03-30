/**
 * Reads the base path from the <base> tag injected by the Go server.
 * Returns "" when running at root, or "/cetacean" (no trailing slash)
 * when running under a sub-path.
 */
function detectBasePath(): string {
  try {
    const base = new URL(document.baseURI);
    const path = base.pathname.replace(/\/+$/, "");

    return path;
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
