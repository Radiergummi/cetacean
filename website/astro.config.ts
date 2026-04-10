import { defineConfig } from "astro/config";
import tailwindcss from "@tailwindcss/vite";
import { visit } from "unist-util-visit";

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
    plugins: [tailwindcss()],
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
