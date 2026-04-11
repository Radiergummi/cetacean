import { defineConfig } from "astro/config";
import tailwindcss from "@tailwindcss/vite";
import { visit } from "unist-util-visit";
import { existsSync } from "node:fs";
import { resolve } from "node:path";
import sirv from "sirv";

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

function remarkStripTitle() {
  return (tree) => {
    const index = tree.children.findIndex((node) => node.type === "heading" && node.depth === 1);

    if (index !== -1) {
      tree.children.splice(index, 1);
    }
  };
}

export default defineConfig({
  srcDir: "./src",
  vite: {
    plugins: [tailwindcss(), pagefindDevPlugin()],
  },
  markdown: {
    remarkPlugins: [remarkDocsLinks, remarkStripTitle],
    shikiConfig: {
      themes: {
        light: "github-light",
        dark: "github-dark",
      },
    },
  },
});
