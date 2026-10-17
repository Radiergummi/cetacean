import { precompress } from "./plugins/precompress.ts";
import { codeSplitting } from "./vite.chunks";
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
        codeSplitting,
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
