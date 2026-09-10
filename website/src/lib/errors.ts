import catalog from "../data/errors.json" with { type: "json" };

/**
 * One well-known error, as `internal/api` defines it. Mirrors the Go
 * `ErrorDef` struct's JSON tags.
 */
export interface ErrorDef {
  code: string;
  title: string;
  status: number;
  description: string;
  suggestion: string;
}

/**
 * A three-letter code prefix and the domain it names.
 */
export interface ErrorDomain {
  prefix: string;
  label: string;
}

/**
 * Written by `scripts/dump-errors` into `src/data/errors.json` before every dev
 * server and every build, so the published reference cannot fall behind the
 * registry the server answers with. The file is gitignored; if it is missing,
 * run `npm run sync-assets`.
 */
export const errorDomains: ErrorDomain[] = catalog.domains;

export const errorDefs: ErrorDef[] = catalog.errors;

/**
 * The errors belonging to one domain, in the order the catalog listed them —
 * which is by code, so a domain's entries are alphabetical within it.
 */
export function errorsInDomain(prefix: string): ErrorDef[] {
  return errorDefs.filter(({ code }) => code.startsWith(prefix));
}

/**
 * The error reference as Markdown, for `/api/errors.md`.
 *
 * The intro is written again here rather than shared with `api/errors.astro`:
 * that page's version is JSX carrying two links, and one paragraph in two
 * hand-written forms is cheaper than a renderer that produces both.
 */
export function errorsMarkdown(): string {
  const sections = errorDomains.map(({ label, prefix }) => {
    const entries = errorsInDomain(prefix).map(({ code, description, status, suggestion, title }) =>
      [
        `### \`${code}\` — ${title}`,
        `**Status** — ${status}`,
        description,
        suggestion && `**Resolution** — ${suggestion}`,
      ]
        .filter(Boolean)
        .join("\n\n"),
    );

    return [`## ${prefix}: ${label}`, ...entries].join("\n\n");
  });

  return (
    [
      "# Error Reference",
      'A domain-specific error carries a stable code as the last path segment of the `type` field in its [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problem document — `"type": "/api/errors/SVC001"` is the entry below. Generic HTTP errors use `about:blank` and have no code.',
      "A running Cetacean serves the same catalog at `GET /api/errors`, and one entry at `GET /api/errors/{code}`. This page is generated from the same source, so the two cannot disagree.",
      ...sections,
    ].join("\n\n") + "\n"
  );
}
