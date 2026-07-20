/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";

// Glyphux admin shell (PRD §5.6 Surface 2). Built as a SPA, base path
// "/admin/" because the compiled assets are embedded via go:embed and
// served under that prefix by internal/adminui — every asset URL Vite
// emits must resolve correctly from there, not from the site root.
export default defineConfig({
  base: "/admin/",
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  build: {
    // Builds straight into internal/adminui's embed target — go:embed can
    // only reach a directory literally inside its own package (no ".."
    // patterns allowed), so this is the one outDir that lets `go build`
    // pick up fresh assets without a separate copy step. The output is
    // committed (see internal/adminui/dist) so `go build ./...` works for
    // anyone who hasn't run `npm run build` here — matching the "no Node
    // runtime required in production" promise (PRD §5.6). Re-run this
    // build and commit the result whenever admin-ui's source changes.
    outDir: "../internal/adminui/dist",
    emptyOutDir: true,
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
    css: true,
  },
});
