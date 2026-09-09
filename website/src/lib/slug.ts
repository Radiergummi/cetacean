/**
 * The anchor id for a heading or a card title.
 *
 * A leaf module of its own because both sides of the build need it and only one
 * of them can import the other: `astro.config.ts` gives every card table row an
 * id, `changelog.astro` gives every release one, and the two must agree or a
 * link written against one resolves against nothing. `lib/docs.ts` cannot host
 * it — it imports `astro:content`, which the config module cannot resolve.
 */
export function slugify(text: string): string {
  return text.toLowerCase().replace(/\W+/g, "-").replace(/^-|-$/g, "");
}
