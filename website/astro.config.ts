import { defineConfig } from "astro/config";
import { unified } from "@astrojs/markdown-remark";
import sitemap from "@astrojs/sitemap";
import mdx from "@astrojs/mdx";
import tailwindcss from "@tailwindcss/vite";
import { visit } from "unist-util-visit";
import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import sirv from "sirv";

const sseGrammar = JSON.parse(readFileSync(resolve("src/lib/sse.tmLanguage.json"), "utf-8"));

/** Serve dist/pagefind/ during dev so search works after a build. */
function pagefindDevPlugin() {
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
  return (tree) => {
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

const defaultTabLabels = {
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
  return (tree) => {
    const { children } = tree;
    let i = 0;
    let tabGroupCount = 0;

    while (i < children.length) {
      if (!isTabCode(children[i])) {
        i++;
        continue;
      }

      const group = [];
      while (i < children.length && isTabCode(children[i])) {
        group.push(children[i]);
        i++;
      }

      if (group.length < 2) {
        stripTabMeta(group[0]);
        continue;
      }

      const labels = group.map((node) => {
        const label =
          parseTabLabel(node.meta) || defaultTabLabels[node.lang] || node.lang || "Code";
        stripTabMeta(node);
        return label;
      });

      const tabGroupId = `tabs-${tabGroupCount++}`;
      const replacement = [];
      const buttons = labels
        .map(
          (label, idx) =>
            `<button role="tab" class="code-tabs-button${idx === 0 ? " active" : ""}" data-tab="${idx}" aria-selected="${idx === 0}" aria-controls="${tabGroupId}-panel-${idx}" id="${tabGroupId}-tab-${idx}">${label}</button>`,
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

      const start = i - group.length;
      children.splice(start, group.length, ...replacement);
      i = start + replacement.length;
    }
  };
}

function isTabCode(node) {
  return node?.type === "code" && typeof node.meta === "string" && /\btab\b/.test(node.meta);
}

function parseTabLabel(meta) {
  const match = meta?.match(/tab="([^"]+)"/);
  return match?.[1] ?? null;
}

function stripTabMeta(node) {
  node.meta = node.meta.replace(/\s*\btab(?:="[^"]*")?/g, "").trim() || null;
}

function html(value) {
  return { type: "html", value };
}

/**
 * GitHub-style alerts. A blockquote whose first line is `[!NOTE]`, `[!TIP]`,
 * `[!WARNING]` or `[!CAUTION]` becomes a callout panel. The syntax renders as
 * an ordinary blockquote on GitHub and in the raw `.md` route, and the plugin
 * runs over both `.md` and `.mdx`, so no doc needs an import.
 *
 * Icon paths are Heroicons (MIT), matching the inline SVGs in the components.
 */
const calloutKinds = {
  note: {
    label: "Note",
    icon: "m11.25 11.25.041-.02a.75.75 0 0 1 1.063.852l-.708 2.836a.75.75 0 0 0 1.063.853l.041-.021M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Zm-9-3.75h.008v.008H12V8.25Z",
  },
  tip: {
    label: "Tip",
    icon: "M12 18v-5.25m0 0a6.01 6.01 0 0 0 1.5-.189m-1.5.189a6.01 6.01 0 0 1-1.5-.189m3.75 7.478a12.06 12.06 0 0 1-4.5 0m3.75 2.383a14.406 14.406 0 0 1-3 0M14.25 18v-.192c0-.983.658-1.823 1.508-2.316a7.5 7.5 0 1 0-7.517 0c.85.493 1.509 1.333 1.509 2.316V18",
  },
  warning: {
    label: "Warning",
    icon: "M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126ZM12 15.75h.007v.008H12v-.008Z",
  },
  caution: {
    label: "Caution",
    icon: "M12 9v3.75m0-10.036A11.959 11.959 0 0 1 3.598 6 11.99 11.99 0 0 0 3 9.75c0 5.592 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.75h-.152c-3.196 0-6.1-1.249-8.25-3.286Zm0 13.036h.008v.008H12v-.008Z",
  },
};

const calloutPattern = new RegExp(
  `^\\[!(${Object.keys(calloutKinds).join("|")})\\][ \\t]*\\n?`,
  "i",
);

function remarkCallouts() {
  return (tree) => {
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

      const kind = match[1].toLowerCase();
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

function calloutHeader(kind) {
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
            strokeWidth: "1.5",
            stroke: "currentColor",
            ariaHidden: "true",
          },
          [element("path", { strokeLinecap: "round", strokeLinejoin: "round", d: icon })],
        ),
        element("span", { className: ["callout-label"] }, [{ type: "text", value: label }]),
      ],
    },
  };
}

function element(tagName, properties, children = []) {
  return { type: "element", tagName, properties, children };
}

function remarkStripTitle() {
  return (tree) => {
    const index = tree.children.findIndex((node) => node.type === "heading" && node.depth === 1);

    if (index !== -1) {
      tree.children.splice(index, 1);
    }
  };
}

export default defineConfig({
  site: "https://cetacean.mazetti.me",
  srcDir: "./src",
  trailingSlash: "never",
  build: { format: "file" },
  prefetch: true,
  integrations: [
    sitemap(),
    mdx({ remarkPlugins: [remarkCodeTabs, remarkCallouts, remarkDocsLinks, remarkStripTitle] }),
  ],
  vite: {
    plugins: [tailwindcss(), pagefindDevPlugin()],
  },
  markdown: {
    processor: unified({
      remarkPlugins: [remarkCodeTabs, remarkCallouts, remarkDocsLinks, remarkStripTitle],
    }),
    shikiConfig: {
      themes: {
        light: "github-light",
        dark: "github-dark",
      },
      langs: ["json", sseGrammar],
    },
  },
});
