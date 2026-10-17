import { codeSplitting } from "./vite.chunks";
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
        codeSplitting,
      },
    },
  },
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
    },
  },
});
