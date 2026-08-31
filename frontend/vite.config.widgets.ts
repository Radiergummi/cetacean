import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import fs from "node:fs";
import path from "node:path";
import { defineConfig } from "vite";
import { viteSingleFile } from "vite-plugin-singlefile";

const widgetsDir = path.resolve(import.meta.dirname, "src/widgets");

/**
 * Every directory under src/widgets holding an index.html is a widget. Reading
 * the directory rather than listing names here means adding a widget is one
 * directory and no config edit, and the Go side discovers them the same way —
 * by reading the build output — so the two cannot fall out of step.
 */
function widgetEntries(): Record<string, string> {
  return Object.fromEntries(
    fs
      .readdirSync(widgetsDir, { withFileTypes: true })
      .filter((entry) => entry.isDirectory())
      .map(({ name }) => [name, path.join(widgetsDir, name, "index.html")])
      .filter(([, entry]) => fs.existsSync(entry)),
  );
}

/**
 * Builds the MCP Apps widgets: one self-contained HTML file per widget in
 * dist-widgets/, which main.go embeds and internal/mcp/ui.go serves as
 * ui://cetacean/<name> resources.
 *
 * An MCP App resource is a single text payload with no base URL, so nothing may
 * load from a relative path — viteSingleFile inlines every script and
 * stylesheet into the HTML. This is a separate target from the dashboard build
 * because the output shape is fundamentally different: many one-file documents
 * rather than one app with hashed chunks.
 */
export default defineConfig({
  base: "./",
  // Rooting at the widgets directory keeps the emitted paths flat —
  // dist-widgets/<name>/index.html — rather than mirroring src/widgets/ into
  // the output. internal/mcp/ui.go reads those directory names as widget names.
  root: widgetsDir,
  plugins: [react(), tailwindcss(), viteSingleFile()],
  build: {
    outDir: path.resolve(import.meta.dirname, "dist-widgets"),
    emptyOutDir: true,
    // Chunking would produce files a widget cannot reach; keep it all inline.
    cssCodeSplit: false,
    assetsInlineLimit: Number.MAX_SAFE_INTEGER,
    rolldownOptions: {
      input: widgetEntries(),
    },
  },
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
    },
  },
});
