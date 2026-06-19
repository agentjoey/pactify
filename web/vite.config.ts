/// <reference types="vitest/config" />
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: { outDir: "../internal/serve/dist", emptyOutDir: true },
  // Dev server proxies the API + SSE stream (both live under /api) to the running
  // `pactify serve` (launchd default :17082, override with PACTIFY_SERVE_URL), so
  // `vite` hot-reload hits the real backend WITHOUT the
  // build → go:embed dist → rebuild binary → restart launchd round-trip. Only the
  // embedded production build (npm run build) needs that chain.
  server: {
    proxy: {
      "/api": {
        target: process.env.PACTIFY_SERVE_URL || "http://127.0.0.1:17082",
        changeOrigin: true,
      },
    },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/setupTests.ts"],
    // e2e/ holds the Playwright acceptance suite, which has its own runner
    // (playwright.config.ts). Vitest's default glob would otherwise try to
    // execute those .spec.ts files and choke on Playwright's test API, so scope
    // the unit suite to src/ (default node_modules/dist excludes still apply).
    include: ["src/**/*.{test,spec}.{ts,tsx}"],
  },
});
