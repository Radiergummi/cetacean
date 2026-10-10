import { precompress } from "./plugins/precompress.ts";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import path from "path";
import { defineConfig } from "vitest/config";

export default defineConfig({
  base: "./",
  plugins: [react(), tailwindcss(), precompress()],
  build: {
    rolldownOptions: {
      output: {
        codeSplitting: {
          // A group also takes the dependencies of what it captures, unless a
          // higher-priority group holds them first. React and the store shim the
          // shell shares must rank first, or they land in charts or topology and
          // every route preloads those.
          groups: [
            {
              name: "vendor-react",
              test: /node_modules\/(react|react-dom|react-router|use-sync-external-store)\//,
              priority: 3,
            },
            {
              name: "vendor-charts",
              test: /node_modules\/(chart\.js|react-chartjs-2|chartjs-plugin-zoom)\//,
              priority: 2,
            },
            // ELK is loaded on demand by lib/layoutElk.ts and must stay in its
            // own chunk — grouping it with React Flow put half a megabyte in
            // front of the first rendered node.
            { name: "vendor-elk", test: /node_modules\/elkjs\//, priority: 1 },
            { name: "vendor-topology", test: /node_modules\/@xyflow\//, priority: 1 },
          ],
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
