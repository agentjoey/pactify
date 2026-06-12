/// <reference types="vitest/config" />
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: { outDir: "../internal/serve/dist", emptyOutDir: true },
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
