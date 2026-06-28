import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "path";

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  base: "/",
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  build: {
    outDir: "../internal/web/dist",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/api": "http://localhost:8080",
      "/auth": "http://localhost:8080",
    },
  },
  // @ts-expect-error vitest injects the test config at runtime; TS only sees vite's UserConfig
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: "./src/test/setup.ts",
  },
});
