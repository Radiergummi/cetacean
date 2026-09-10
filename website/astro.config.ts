import { defineConfig } from "astro/config";
import { type RehypePlugins, type RemarkPlugins, unified } from "@astrojs/markdown-remark";
import sitemap from "@astrojs/sitemap";
import mdx from "@astrojs/mdx";
import tailwindcss from "@tailwindcss/vite";
import { visit } from "unist-util-visit";
import { rehypeMermaid } from "@/lib/mermaid-diagrams.ts";
import { lastModifiedFor } from "@/lib/pages.ts";
import { slugify } from "@/lib/slug.ts";
import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import sirv from "sirv";
import type { Element, ElementContent, Properties } from "hast";
import type {
  Code,
  Html,
  Paragraph,
  PhrasingContent,
  Root,
  RootContent,
  TableCell,
  TableRow,
} from "mdast";
import type { Node } from "unist";
import type { AstroIntegration } from "astro";
import type { Plugin as VitePlugin } from "vite";

const sseGrammar = JSON.parse(readFileSync(resolve("src/lib/sse.tmLanguage.json"), "utf-8"));

/**
 * Fails the build if no sitemap was written.
 *
 * `@astrojs/sitemap` wraps the whole `serialize` pass in a try/catch: a throw
 * from `lastModifiedFor` is handed to `logger.error` and the hook returns
 * early, so `astro build` still exits 0 with no `sitemap-index.xml` in `dist/`
 * — while `robots.txt`, `/llms.txt` and `/openapi.json` all keep advertising a
 * URL that now 404s. The throw over an undateable page is only an invariant if
 * something outside that catch notices, which is this. It runs after the
 * sitemap integration because integration hooks run in declaration order.
 */
function sitemapRequired(): AstroIntegration {
  return {
    name: "cetacean:sitemap-required",
    hooks: {
      "astro:build:done": ({ dir }) => {
        if (!existsSync(new URL("sitemap-index.xml", dir))) {
          throw new Error(
            "no sitemap was written; @astrojs/sitemap logged the reason above and swallowed it",
          );
        }
      },
    },
  };
}

/** Serve dist/pagefind/ during dev so search works after a build. */
function pagefindDevPlugin(): VitePlugin {
  const pagefindDir = resolve("dist/pagefind");
  return {
    name: "pagefind-dev",
    configureServer(server) {
      if (existsSync(pagefindDir)) {
        server.middlewares.use("/pagefind", sirv(pagefindDir, { dev: true }));
      }
    },
  };
}

function remarkDocsLinks() {
  return (tree: Root) => {
    visit(tree, "link", (node) => {
      if (
        typeof node.url === "string" &&
        !node.url.startsWith("http") &&
        !node.url.startsWith("//")
      ) {
        node.url = node.url.replace(/\.mdx?(#|$)/, "$1");
      }
    });
  };
}

const defaultTabLabels: Record<string, string> = {
  http: "HTTP",
  bash: "cURL",
  sh: "cURL",
  shell: "Shell",
  javascript: "JavaScript",
  js: "JavaScript",
  typescript: "TypeScript",
  ts: "TypeScript",
  python: "Python",
  go: "Go",
  json: "JSON",
  yaml: "YAML",
};

function remarkCodeTabs() {
  return (tree: Root) => {
    const { children } = tree;
    let index = 0;
    let tabGroupCount = 0;

    while (index < children.length) {
      if (!isTabCode(children[index])) {
        index++;

        continue;
      }

      const group: Code[] = [];

      while (index < children.length) {
        const node = children[index];

        if (!isTabCode(node)) {
          break;
        }

        group.push(node);

        index++;
      }

      if (group.length < 2) {
        stripTabMeta(group[0]);

        continue;
      }

      const labels = group.map((node) => {
        const language = node.lang ?? "";
        const label = parseTabLabel(node.meta) || defaultTabLabels[language] || language || "Code";

        stripTabMeta(node);

        return label;
      });

      const tabGroupId = `tabs-${tabGroupCount++}`;
      const replacement: RootContent[] = [];
      const buttons = labels
        .map(
          (label, index) =>
            `<button role="tab" class="code-tabs-button${index === 0 ? " active" : ""}" data-tab="${index}" aria-selected="${index === 0}" aria-controls="${tabGroupId}-panel-${index}" id="${tabGroupId}-tab-${index}">${label}</button>`,
        )
        .join("");

      replacement.push(
        html(`<div class="code-tabs"><div class="code-tabs-bar" role="tablist">${buttons}</div>`),
      );

      for (let j = 0; j < group.length; j++) {
        replacement.push(
          html(
            `<div class="code-tab-panel${j === 0 ? " active" : ""}" data-tab="${j}" role="tabpanel" id="${tabGroupId}-panel-${j}" aria-labelledby="${tabGroupId}-tab-${j}">`,
          ),
        );
        replacement.push(group[j]);
        replacement.push(html("</div>"));
      }

      replacement.push(html("</div>"));

      const start = index - group.length;

      children.splice(start, group.length, ...replacement);

      index = start + replacement.length;
    }
  };
}

function isTabCode(node: RootContent | undefined): node is Code {
  return node?.type === "code" && typeof node.meta === "string" && /\btab\b/.test(node.meta);
}

function parseTabLabel(meta: string | null | undefined) {
  const match = meta?.match(/tab="([^"]+)"/);

  return match?.[1] ?? null;
}

function stripTabMeta(node: Code) {
  node.meta = node.meta?.replace(/\s*\btab(?:="[^"]*")?/g, "").trim() || null;
}

function html(value: string): Html {
  return { type: "html", value };
}

/**
 * GitHub-style alerts. A blockquote whose first line is `[!NOTE]`, `[!TIP]`,
 * `[!WARNING]` or `[!CAUTION]` becomes a callout panel. The syntax renders as
 * an ordinary blockquote on GitHub and in the raw `.md` route, and the plugin
 * runs over both `.md` and `.mdx`, so no doc needs an import.
 *
 * Icons are Lucide (ISC), the set the components render through `@lucide/astro`.
 * A Lucide icon is more than one path, so each entry carries the whole node
 * list rather than a single `d`.
 */
const calloutKinds = {
  note: {
    label: "Note",
    icon: [
      ["circle", { cx: "12", cy: "12", r: "10" }],
      ["path", { d: "M12 16v-4" }],
      ["path", { d: "M12 8h.01" }],
    ],
  },
  tip: {
    label: "Tip",
    icon: [
      [
        "path",
        {
          d: "M15 14c.2-1 .7-1.7 1.5-2.5 1-.9 1.5-2.2 1.5-3.5A6 6 0 0 0 6 8c0 1 .2 2.2 1.5 3.5.7.7 1.3 1.5 1.5 2.5",
        },
      ],
      ["path", { d: "M9 18h6" }],
      ["path", { d: "M10 22h4" }],
    ],
  },
  warning: {
    label: "Warning",
    icon: [
      ["path", { d: "m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3" }],
      ["path", { d: "M12 9v4" }],
      ["path", { d: "M12 17h.01" }],
    ],
  },
  caution: {
    label: "Caution",
    icon: [
      [
        "path",
        {
          d: "M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z",
        },
      ],
      ["path", { d: "M12 8v4" }],
      ["path", { d: "M12 16h.01" }],
    ],
  },
} satisfies Record<string, { label: string; icon: [string, Properties][] }>;

type CalloutKind = keyof typeof calloutKinds;

const calloutPattern = new RegExp(
  `^\\[!(${Object.keys(calloutKinds).join("|")})\\][ \\t]*\\n?`,
  "i",
);

function remarkCallouts() {
  return (tree: Root) => {
    visit(tree, "blockquote", (node) => {
      const paragraph = node.children[0];

      if (paragraph?.type !== "paragraph") {
        return;
      }

      const text = paragraph.children[0];

      if (text?.type !== "text") {
        return;
      }

      const match = text.value.match(calloutPattern);

      if (!match) {
        return;
      }

      const kind = match[1].toLowerCase() as CalloutKind;
      text.value = text.value.slice(match[0].length);

      if (!text.value) {
        paragraph.children.shift();
      }

      if (paragraph.children.length === 0) {
        node.children.shift();
      }

      node.data = {
        hName: "div",
        hProperties: { className: ["callout", `callout-${kind}`] },
      };
      node.children.unshift(calloutHeader(kind));
    });
  };
}

function calloutHeader(kind: CalloutKind): Paragraph {
  const { label, icon } = calloutKinds[kind];

  return {
    type: "paragraph",
    children: [],
    data: {
      hName: "div",
      hProperties: { className: ["callout-header"] },
      hChildren: [
        element(
          "svg",
          {
            className: ["callout-icon"],
            xmlns: "http://www.w3.org/2000/svg",
            fill: "none",
            viewBox: "0 0 24 24",
            strokeWidth: "2",
            strokeLinecap: "round",
            strokeLinejoin: "round",
            stroke: "currentColor",
            ariaHidden: "true",
          },
          icon.map(([tagName, properties]) => element(tagName, properties)),
        ),
        element("span", { className: ["callout-label"] }, [{ type: "text", value: label }]),
      ],
    },
  };
}

function element(
  tagName: string,
  properties: Properties,
  children: ElementContent[] = [],
): Element {
  return { type: "element", tagName, properties, children };
}

/**
 * A `paragraph` carrying `hName` is a container for whatever hast element it
 * names, and so holds whatever that element holds — which is not the phrasing
 * content mdast's own type for a paragraph demands. `mdast-util-to-hast` reads
 * `data` and never measures the tree against that type, so the widening is the
 * whole of the difference, and it lives here rather than at every call site.
 */
function container(hName: string, children: RootContent[], properties?: Properties): Paragraph {
  return {
    type: "paragraph",
    data: properties ? { hName, hProperties: properties } : { hName },
    children: children as unknown as PhrasingContent[],
  };
}

/**
 * A two-column table is a description list in a table costume: the term column
 * is squeezed to a few characters while the second wraps at half measure, and
 * no reader ever compares one row against another. Comparison is what a table
 * is for, and it needs a third column. So every two-column table renders as a
 * description list, and three or more columns are left alone.
 *
 * Length picks the density, not the container. Sorting the docs' two-column
 * tables by mean cell length gives a continuum, not two groups — `Resource |
 * Sortable fields` and `Resource | Fields` sit next to each other in the API
 * reference and differ only in how long the values run — so any cutoff between
 * "table" and "list" renders neighbours in two different shapes and reads as a
 * bug. A cutoff between two densities of the same list does not.
 *
 * The source stays an ordinary GFM table, so GitHub and the raw `.md` route are
 * unaffected, and the plugin runs over both `.md` and `.mdx`, so no doc needs
 * an import.
 */
const compactCellLength = 40;

function remarkDefinitionTables() {
  return (tree: Root) => {
    visit(tree, "table", (node, index, parent) => {
      if (!parent || index === undefined || node.children.length < 2) {
        return;
      }

      const [header, ...rows] = node.children;

      if (header.children.length !== 2) {
        return;
      }

      if (rows.some(({ children }) => children.length !== 2)) {
        return;
      }

      const total = rows.reduce((sum, { children }) => sum + nodeText(children[1]).length, 0);
      const compact = total / rows.length < compactCellLength;

      // `visit` types the parent as every node that could hold this one, and
      // TypeScript will not splice into the union of their children arrays as
      // one, though each of them holds the content this writes.
      (parent.children as RootContent[]).splice(
        index,
        1,
        definitionCaption(header, compact),
        definitionList(rows, compact),
      );

      return index + 2;
    });
  };
}

function nodeText(node: RootContent): string {
  if ("value" in node) {
    return node.value;
  }

  return ("children" in node ? node.children : []).map(nodeText).join("");
}

/** The column headings, kept as an eyebrow so the transform loses no wording. */
function definitionCaption(header: TableRow, compact: boolean): Paragraph {
  return container(
    "div",
    header.children.map((cell) => container("span", cell.children)),
    { className: ["definition-caption", ...(compact ? ["is-compact"] : [])] },
  );
}

function definitionList(rows: TableRow[], compact: boolean): Paragraph {
  return container(
    "dl",
    rows.map((row) =>
      container(
        "div",
        [container("dt", row.children[0].children), container("dd", row.children[1].children)],
        { className: ["definition-row"] },
      ),
    ),
    { className: ["definition-list", ...(compact ? ["is-compact"] : [])] },
  );
}

/**
 * A table marked `<!-- cards -->` renders as a card per row: the first cell
 * becomes the title, the second the description, and any further cells become
 * labelled facts under it, headed by their column name.
 *
 * MDX has no HTML comments, so an `.mdx` doc writes the same marker as an
 * expression comment instead. `isCardsMarker` reads both, and a doc that
 * changes format keeps its cards.
 *
 * This one is opted into rather than detected. Three-column tables split into
 * cards and genuine matrices with nothing to tell them apart mechanically —
 * the authentication guide compares two Tailscale modes in the same shape the
 * MCP reference uses to list tools, and every rule that catches the one
 * catches the other. The marker is a comment either way, so the source stays a
 * plain GFM table that GitHub and the raw `.md` route render as they always
 * did, and the author decides.
 *
 * Rows gain an ID from their title, which a table row cannot have, so a tool
 * or a finding can be linked to directly.
 */
function remarkCardTables() {
  return (tree: Root) => {
    visit(tree, isCardsMarker, (_node, index, parent) => {
      if (!parent || index === undefined) {
        return;
      }

      const table = parent.children[index + 1];

      if (table?.type !== "table" || table.children.length < 2) {
        return;
      }

      const [header, ...rows] = table.children;

      // A card reads one cell per column out of every row, so a table whose
      // rows are not all as wide as its header would index past the end of
      // one. Leave it as a table, the way `remarkDefinitionTables` does with a
      // shape it cannot render, rather than failing the build on a stray pipe.
      if (header.children.length < 2) {
        return;
      }
      if (rows.some(({ children }) => children.length !== header.children.length)) {
        return;
      }

      (parent.children as RootContent[]).splice(index, 2, cardList(header, rows));

      return index + 1;
    });
  };
}

/**
 * The marker in the only form each format has for it: an HTML comment in
 * Markdown, and the expression an MDX doc's comment parses to. Both node types
 * carry their source in `value`, so the value is the whole of the test.
 *
 * The parameter is a bare unist `Node` rather than an mdast `RootContent`
 * because `mdxFlowExpression` is not one — it comes from `mdast-util-mdx`, and
 * against `RootContent` the check for it is a comparison with no overlap. It is
 * also what `visit` wants: a `Test` takes a `Node`.
 */
function isCardsMarker(node: Node): boolean {
  if (node.type !== "html" && node.type !== "mdxFlowExpression") {
    return false;
  }

  const value = "value" in node ? String(node.value).trim() : "";

  return value === "<!-- cards -->" || value === "/* cards */";
}

function cardList({ children }: TableRow, rows: TableRow[]): Paragraph {
  const labels = children.map((cell) => nodeText(cell));

  return container(
    "div",
    rows.map(({ children }) => card(labels, children, descriptionColumn(rows))),
    { className: ["card-list"] },
  );
}

/**
 * The description is the column that reads longest, not the one that comes
 * second: the MCP reference heads its prompts with four short columns before
 * the prose. Length is measured over rendered text, so a column of links is
 * judged by what a reader sees rather than by the URLs behind it.
 */
function descriptionColumn(rows: TableRow[]): number {
  let column = 1;
  let longest = 0;

  for (let candidate = 1; candidate < rows[0].children.length; candidate++) {
    const total = rows.reduce((sum, row) => sum + nodeText(row.children[candidate]).length, 0);

    if (total > longest) {
      longest = total;
      column = candidate;
    }
  }

  return column;
}

function card(labels: string[], cells: TableCell[], description: number): Paragraph {
  const [title] = cells;
  const children: Paragraph[] = [container("div", title.children, { className: ["card-title"] })];

  if (nodeText(cells[description]).trim()) {
    children.push(
      container("div", cells[description].children, { className: ["card-description"] }),
    );
  }

  const facts = cells
    .map((cell, column) => ({ cell, column }))
    .filter(({ cell, column }) => column !== 0 && column !== description && nodeText(cell).trim());

  if (facts.length) {
    children.push(
      container(
        "div",
        facts.flatMap(({ cell, column }) => [
          container("span", [{ type: "text", value: labels[column] }], {
            className: ["card-label"],
          }),
          container("span", cell.children, { className: ["card-value"] }),
        ]),
        { className: ["card-facts"] },
      ),
    );
  }

  return container("div", children, { className: ["card"], id: slugify(nodeText(title)) });
}

function remarkStripTitle() {
  return (tree: Root) => {
    const index = tree.children.findIndex((node) => node.type === "heading" && node.depth === 1);

    if (index !== -1) {
      tree.children.splice(index, 1);
    }
  };
}

/**
 * Shared by `.md` and `.mdx`: MDX inherits the plugins from `markdown.processor`
 * rather than taking its own copy, so the two routes cannot render a doc
 * differently.
 */
const remarkPlugins: RemarkPlugins = [
  remarkCodeTabs,
  remarkCallouts,
  remarkCardTables,
  remarkDefinitionTables,
  remarkDocsLinks,
  remarkStripTitle,
];

const rehypePlugins: RehypePlugins = [rehypeMermaid];

export default defineConfig({
  site: "https://cetacean.mazetti.me",
  srcDir: "./src",
  trailingSlash: "never",
  build: { format: "file" },
  prefetch: true,
  integrations: [
    // Every entry carries the commit date of the file behind it. `serialize` is
    // synchronous, so the date is read with `execFileSync`; and an unmapped URL
    // throws rather than losing its `lastmod`, so a new page cannot ship
    // looking as though it never changes. `sitemapRequired` is what turns that
    // throw into a failed build, and has to follow this entry to see its work.
    sitemap({ serialize: (item) => ({ ...item, lastmod: lastModifiedFor(item.url) }) }),
    sitemapRequired(),
    mdx(),
  ],
  vite: {
    plugins: [tailwindcss(), pagefindDevPlugin()],
    // Fail on a taken port rather than quietly moving to the next one. A stray
    // `astro preview` holding 4321 otherwise pushes the dev server to 4322
    // while the browser stays on 4321, reading a static `dist/` build that no
    // edit ever reaches. `strictPort` is Vite's, not Astro's: Astro forwards
    // only host, port, headers and open from its own `server` block.
    server: { strictPort: true },
    preview: { strictPort: true },
  },
  markdown: {
    processor: unified({ remarkPlugins, rehypePlugins }),
    // Shiki runs ahead of every user rehype plugin, so `mermaid` has to be kept
    // out of its hands for `rehypeMermaid` to see an untouched code block.
    syntaxHighlight: { type: "shiki", excludeLangs: ["mermaid"] },
    shikiConfig: {
      themes: {
        light: "github-light",
        dark: "github-dark",
      },
      langs: ["json", sseGrammar],
    },
  },
});
