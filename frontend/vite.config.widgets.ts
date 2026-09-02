import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import path from "node:path";
import { defineConfig } from "vite";
import { viteSingleFile } from "vite-plugin-singlefile";

const widgetsDir = path.resolve(import.meta.dirname, "src/widgets");

/**
 * The widget this invocation builds. scripts/build-widgets.mjs sets it once per
 * widget; building one by hand is `CETACEAN_WIDGET=topology npm run
 * build:widgets:one`.
 */
const widget = process.env.CETACEAN_WIDGET;

if (!widget) {
  throw new Error("CETACEAN_WIDGET is not set — run `npm run build:widgets` to build them all");
}

/**
 * Builds one MCP Apps widget: a self-contained HTML file at
 * dist-widgets/<name>/index.html, which main.go embeds and internal/mcp/ui.go
 * serves as ui://cetacean/<name>.
 *
 * An MCP App resource is a single text payload with no base URL, so nothing may
 * load from a relative path — viteSingleFile inlines every script and stylesheet
 * into the HTML. That is also why this builds one widget per invocation rather
 * than all of them at once: inlining requires code splitting to be off, and
 * Rolldown rejects that outright for a build with more than one input. Each
 * widget is an independent document anyway, with nothing to share across them.
 *
 * A separate target from the dashboard build because the output shape is
 * fundamentally different: many one-file documents rather than one app with
 * hashed chunks.
 */
export default defineConfig({
  base: "./",
  // Rooting at the widget's own directory keeps the emitted path flat —
  // dist-widgets/<name>/index.html — rather than mirroring src/widgets/ into
  // the output. internal/mcp/ui.go reads those directory names as widget names.
  root: path.join(widgetsDir, widget),
  plugins: [react(), tailwindcss(), viteSingleFile()],
  build: {
    outDir: path.resolve(import.meta.dirname, "dist-widgets", widget),
    // The build script clears dist-widgets once, before the first widget: each
    // invocation emptying its own output would be the same thing, but a stale
    // directory for a widget that has been deleted would survive.
    emptyOutDir: false,
    // Chunking would produce files a widget cannot reach; keep it all inline.
    cssCodeSplit: false,
    assetsInlineLimit: Number.MAX_SAFE_INTEGER,
  },
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
    },
  },
});
