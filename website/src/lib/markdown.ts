/**
 * A `text/markdown` response that names the page it represents.
 *
 * The HTML pages point here with `<link rel="alternate">`; this points back with
 * a canonical URL. It has to ride in the body: GitHub Pages serves static files
 * and offers no way to set a `Link` header, so a `rel="canonical"` header — the
 * usual way a non-HTML representation names its page — is not available. An
 * HTML comment renders as nothing wherever the Markdown is rendered, and
 * survives being pasted into an agent's context, which is where it matters.
 */
export function markdownResponse(body: string, path: string, site: URL | undefined): Response {
  const canonical = new URL(path, site).href;

  return new Response(`<!-- canonical: ${canonical} -->\n\n${body.trimStart()}`, {
    headers: { "Content-Type": "text/markdown; charset=utf-8" },
  });
}
