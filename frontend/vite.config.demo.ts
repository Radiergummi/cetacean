import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import path from "path";
import { defineConfig } from "vite";

export default defineConfig({
  base: "/demo/",
  plugins: [react(), tailwindcss()],
  build: {
    outDir: "../website/public/demo",
    emptyOutDir: false,
    rolldownOptions: {
      input: path.resolve(import.meta.dirname, "demo.html"),
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
          if (id.includes("node_modules/@xyflow/") || id.includes("node_modules/elkjs/")) {
            return "vendor-topology";
          }
        },
      },
    },
  },
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
    },
  },
});
