import { stringify } from "yaml";
import { generatedPage } from "./pages";

/**
 * A leading YAML frontmatter block, which every doc under `../docs` opens with
 * and none of the generated pages have.
 */
const frontmatterPattern = /^---\r?\n[\s\S]*?\r?\n---[^\S\r\n]*(?:\r?\n|$)/;

/**
 * Appends `canonical` to a block the document already has, leaving its own keys
 * and their formatting untouched — re-stringifying would rewrite `tags` out of
 * the flow style the docs write it in.
 */
function withCanonical(block: string, canonical: string): string {
  const lines = block.trimEnd().split("\n");

  lines.splice(lines.length - 1, 0, `canonical: ${canonical}`);

  return `${lines.join("\n")}\n`;
}

/**
 * A `text/markdown` response that names the page it represents.
 *
 * The HTML pages point here with `<link rel="alternate">`; this points back with
 * a canonical URL. It has to ride in the body: GitHub Pages serves static files
 * and offers no way to set a `Link` header, so a `rel="canonical"` header — the
 * usual way a non-HTML representation names its page — is not available.
 *
 * Frontmatter is where it goes, because that is where a Markdown document's
 * metadata goes and every reader that matters parses it. It has to be the
 * *first* thing in the body, too: frontmatter is only frontmatter at offset 0,
 * and a doc served by `[...slug].md.ts` is the raw file, which already opens
 * with a block of its own. Put anything in front of that and a reader stops
 * stripping it, leaving `title:`/`category:`/`tags:` to render as a thematic
 * break and a setext heading on every doc page.
 *
 * A page with no block of its own gets one built from the title and description
 * `generatedPages` already holds for it, so every Markdown representation this
 * site serves describes itself the same way.
 */
export function markdownResponse(body: string, path: string, site: URL | undefined): Response {
  const canonical = new URL(path, site).href;
  const trimmed = body.trimStart();
  const own = trimmed.match(frontmatterPattern)?.[0];

  let frontmatter: string;

  if (own) {
    frontmatter = withCanonical(own, canonical);
  } else {
    const { title, description } = generatedPage(path);

    frontmatter = `---\n${stringify({ title, description, canonical })}---\n`;
  }

  const prose = trimmed.slice(own?.length ?? 0).trimStart();

  return new Response(`${frontmatter}\n${prose}`, {
    headers: {
      // Only `astro dev` honours this. A static build discards a response's
      // headers unless the adapter opts into `staticHeaders`, and there is no
      // adapter here — GitHub Pages types `dist/*.md` from its own extension
      // map. See the note in `pages/openapi.json.ts`.
      "Content-Type": "text/markdown; charset=utf-8",
    },
  });
}
