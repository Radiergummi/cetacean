import { readFile } from "node:fs/promises";
import { join } from "node:path";
import type { APIContext } from "astro";
import { docsDir, getDocPaths, operationsLevels } from "../lib/docs";
import { markdownResponse } from "../lib/markdown";

export async function getStaticPaths() {
  const docs = await getDocPaths();

  return docs.map(({ filePath, id: slug }) => ({
    params: { slug },
    props: { filePath },
  }));
}

const attributePattern = /(\w+)(?:=(?:"([^"]*)"|\{([^}]*)}))?/g;

function attributesOf(source: string): Record<string, string> {
  const attributes: Record<string, string> = {};

  for (const [, name, quoted, braced] of source.matchAll(attributePattern)) {
    attributes[name] = quoted ?? braced ?? "true";
  }

  return attributes;
}

/**
 * Renders the components an `.mdx` doc uses back to Markdown. This route is
 * what the Copy page action, the View as Markdown link and the AI-app links
 * all serve, so a reader who follows one must get the page rather than its
 * source: without this, the configuration reference hands over an import
 * statement and a wall of JSX.
 *
 * Only `ConfigParam` and `McpTool` need handling — everything else in the docs
 * is plain Markdown, and the remark plugins in `astro.config.ts` deliberately
 * keep it that way. A component added here without a case below degrades to its
 * own tags, which is what the raw route did for every component before.
 *
 * Each case renders back to the Markdown the doc held before the component
 * replaced it, so a reader who has only ever seen this route sees no change.
 */
function toMarkdown(content: string): string {
  return (
    content
      .replace(/^import\s[^\n]*\n/gm, "")
      // A tool's arguments render under its prose, where the card puts them,
      // rather than after the first paragraph, where the source used to.
      .replace(
        /<McpTool([^>]*)>\s*<Fragment slot="arguments">([\s\S]*?)<\/Fragment>([\s\S]*?)<\/McpTool>/g,
        (_, rawAttributes: string, args: string, body: string) => {
          const { name, level } = attributesOf(rawAttributes);

          return [
            `#### \`${name}\``,
            body.trim(),
            `**Arguments** — ${args.trim()}`,
            `**Level** — ${level} (${operationsLevels[Number(level)]})`,
          ].join("\n\n");
        },
      )
      // The cards marker, back in the form a Markdown reader knows it by.
      .replace(/^\{\/\* cards \*\/}$/gm, "<!-- cards -->")
      .replace(
        /<ConfigParam([^>]*)>([\s\S]*?)<\/ConfigParam>/g,
        (_, rawAttributes: string, body: string) => {
          const {
            name,
            flag,
            env,
            default: fallback,
            required,
            deprecated,
          } = attributesOf(rawAttributes);

          const badges = [required && "**Required.**", deprecated && "**Deprecated.**"].filter(
            Boolean,
          );
          const facts = [
            flag && `- flag: \`${flag}\``,
            env && `- env: \`${env}\``,
            fallback !== undefined && `- default: \`${fallback}\``,
          ].filter(Boolean);

          const description = [...badges, body.trim()].filter(Boolean).join(" ");

          return [`### \`${name}\``, description, facts.join("\n")].filter(Boolean).join("\n\n");
        },
      )
      .replace(/\n{3,}/g, "\n\n")
  );
}

/**
 * A component with no case in `toMarkdown` degrades to its own tags, and this
 * route serves `text/markdown` to readers and AI clients that cannot tell the
 * difference. The build is static, so failing here fails `astro build` rather
 * than shipping JSX as prose.
 */
function assertRendered(markdown: string, path: string): string {
  // A placeholder like `<Hostname>` inside a code span or fence is prose the
  // docs are free to write, not a component that failed to render.
  const prose = markdown.replace(/```[\s\S]*?```/g, "").replace(/`[^`\n]*`/g, "");
  const leftover = [...new Set(prose.match(/<[A-Z][A-Za-z]*/g) ?? [])];

  if (leftover.length > 0) {
    throw new Error(`${path}: no Markdown fallback for ${leftover.join(", ")}`);
  }

  return markdown;
}

export async function GET({ params, props, site }: APIContext) {
  const path: string = props.filePath ?? join(docsDir, "not-found");

  let content: string;

  // Only the read may legitimately fail. Rendering happens outside the catch,
  // so an unhandled component fails `astro build` instead of being swallowed
  // into the 404 this returns for a missing file.
  try {
    content = await readFile(path, "utf-8");
  } catch {
    return new Response("Not found", { status: 404 });
  }

  return markdownResponse(
    path.endsWith(".mdx") ? assertRendered(toMarkdown(content), path) : content,
    `/${params.slug}`,
    site,
  );
}
