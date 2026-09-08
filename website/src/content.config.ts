import { defineCollection, z } from "astro:content";
import { glob } from "astro/loaders";

const docs = defineCollection({
  loader: glob({
    pattern: ["*.{md,mdx}", "!test_*"],
    base: "../docs",
  }),
  schema: z.object({
    title: z.string(),
    description: z.string(),
    category: z.enum(["overview", "guide", "reference"]),
    tags: z.array(z.string()),
  }),
});

export const collections = { docs };
