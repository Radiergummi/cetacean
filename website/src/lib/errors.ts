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
