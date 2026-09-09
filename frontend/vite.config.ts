import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import path from "path";
import { defineConfig } from "vitest/config";

export default defineConfig({
  base: "./",
  plugins: [react(), tailwindcss()],
  build: {
    rolldownOptions: {
      output: {
        manualChunks(id) {
          if (
            id.includes("node_modules/react-dom/") ||
            id.includes("node_modules/react/") ||
            id.includes("node_modules/react-router")
          ) {
            return "vendor-react";
          }
          if (
            id.includes("node_modules/chart.js/") ||
            id.includes("node_modules/react-chartjs-2/") ||
            id.includes("node_modules/chartjs-plugin-zoom/")
          ) {
            return "vendor-charts";
          }
          // ELK is loaded on demand by lib/layoutElk.ts and must stay in its
          // own chunk — grouping it with React Flow put half a megabyte in
          // front of the first rendered node.
          if (id.includes("node_modules/elkjs/")) {
            return "vendor-elk";
          }
          if (id.includes("node_modules/@xyflow/")) {
            return "vendor-topology";
          }

          return undefined;
        },
      },
    },
  },
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
    },
  },
  server: {
    proxy: {
      "^/(nodes|services|tasks|configs|secrets|networks|volumes|stacks|search|events|topology|cluster|swarm|plugins|disk-usage|history|notifications|prometheus|metrics|recommendations|api|-|debug)":
        {
          target: "http://localhost:9000",
          bypass(req) {
            // Let the SPA handle browser navigations (Accept: text/html)
            if (req.headers.accept?.includes("text/html")) {
              return "/index.html";
            }

            return undefined;
          },
        },
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    exclude: ["e2e/**", "node_modules/**"],
  },
});
