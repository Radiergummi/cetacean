import { existsSync, readFileSync } from "node:fs";

const sitemap = new URL("../dist/sitemap-0.xml", import.meta.url);

/** Every published page, by path, read from the build the suite runs against. */
export function publishedPaths(): string[] {
  if (!existsSync(sitemap)) {
    throw new Error("no build to test; run `pnpm --filter website build` first");
  }

  return Array.from(
    readFileSync(sitemap, "utf-8").matchAll(/<loc>([^<]+)<\/loc>/g),
    ([, url]) => new URL(url).pathname,
  );
}

export const widths = [320, 375, 768, 1280] as const;
