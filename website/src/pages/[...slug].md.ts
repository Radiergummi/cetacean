import {readFile} from "node:fs/promises";
import {join} from "node:path";
import {docsDir, getDocPaths} from "../lib/docs";

export async function getStaticPaths() {
  const docs = await getDocPaths();

  return docs.map(({filePath, id: slug}) => ({
    params: {slug},
    props: {filePath},
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
  return content
    .replace(/^import\s[^\n]*\n/gm, "")
    // A tool's arguments render under its prose, where the card puts them,
    // rather than after the first paragraph, where the source used to.
    .replace(
      /<McpTool name="([^"]+)">\s*<Fragment slot="arguments">([\s\S]*?)<\/Fragment>([\s\S]*?)<\/McpTool>/g,
      (_, name: string, args: string, body: string) =>
        [`#### \`${name}\``, body.trim(), `**Arguments** — ${args.trim()}`].join("\n\n"),
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

        const description = [...badges, body.trim()]
          .filter(Boolean)
          .join(" ");

        return [`### \`${name}\``, description, facts.join("\n")]
          .filter(Boolean)
          .join("\n\n");
      },
    )
    .replace(/\n{3,}/g, "\n\n");
}

export async function GET({props}: { props: { filePath?: string } }) {
  const path = props.filePath ?? join(docsDir, "not-found");

  try {
    const content = await readFile(path, "utf-8");

    return new Response(
      path.endsWith(".mdx")
      ? toMarkdown(content)
      : content,
      {headers: {"Content-Type": "text/markdown; charset=utf-8"}},
    );
  } catch {
    return new Response("Not found", {status: 404});
  }
}
