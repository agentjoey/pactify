/// <reference types="vitest/config" />
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

const serveTarget = process.env.PACTIFY_SERVE_URL || "http://127.0.0.1:7777";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: "../internal/serve/dist",
    emptyOutDir: true,
    rollupOptions: {
      output: {
        manualChunks(id) {
          // Vendor split: keep the heavy dependencies out of the main entry chunk
          // so the initial dashboard load stays small.
          if (
            id.includes("node_modules/react/") ||
            id.includes("node_modules/react-dom/")
          ) {
            return "react-vendor";
          }
        },
      },
    },
  },
  // Dev server proxies the API + SSE stream (both live under /api) to the running
  // `pactify serve` (default :7777, override with PACTIFY_SERVE_URL), so
  // `vite` hot-reload hits the real backend WITHOUT the
  // build → go:embed dist → rebuild binary → restart server round-trip. Only the
  // embedded production build (npm run build) needs that chain.
  server: {
    proxy: {
      "/api": {
        target: serveTarget,
        changeOrigin: true,
        configure(proxy) {
          proxy.on("proxyReq", (proxyReq, req) => {
            // The browser talks to Vite; serve checks the backend's exact origin.
            if (req.headers.origin !== undefined) {
              proxyReq.setHeader("Origin", new URL(serveTarget).origin);
            }
          });
        },
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
    // unit suites to src/ and the standalone panel/ (default excludes still apply).
    include: ["src/**/*.{test,spec}.{ts,tsx}", "panel/**/*.test.{ts,tsx}"],
  },
});
