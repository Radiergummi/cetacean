import { defineConfig } from "astro/config";
import sitemap from "@astrojs/sitemap";
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
        node.url = node.url.replace(/\.md(#|$)/, "$1");
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
        const label = parseTabLabel(node.meta) || defaultTabLabels[node.lang] || node.lang || "Code";
        stripTabMeta(node);
        return label;
      });

      const replacement = [];
      const buttons = labels
        .map(
          (label, idx) =>
            `<button role="tab" class="code-tabs-button${idx === 0 ? " active" : ""}" data-tab="${idx}">${label}</button>`,
        )
        .join("");

      replacement.push(html(`<div class="code-tabs"><div class="code-tabs-bar" role="tablist">${buttons}</div>`));

      for (let j = 0; j < group.length; j++) {
        replacement.push(html(`<div class="code-tab-panel${j === 0 ? " active" : ""}" data-tab="${j}">`));
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

function remarkStripTitle() {
  return (tree) => {
    const index = tree.children.findIndex((node) => node.type === "heading" && node.depth === 1);

    if (index !== -1) {
      tree.children.splice(index, 1);
    }
  };
}

export default defineConfig({
  site: "https://cetacean.dev",
  srcDir: "./src",
  trailingSlash: "always",
  prefetch: true,
  integrations: [sitemap()],
  vite: {
    plugins: [tailwindcss(), pagefindDevPlugin()],
  },
  markdown: {
    remarkPlugins: [remarkCodeTabs, remarkDocsLinks, remarkStripTitle],
    shikiConfig: {
      themes: {
        light: "github-light",
        dark: "github-dark",
      },
      langs: ["json", sseGrammar],
    },
  },
});
