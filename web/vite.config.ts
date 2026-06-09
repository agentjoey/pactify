/// <reference types="vitest/config" />
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: { outDir: "../internal/serve/dist", emptyOutDir: true },
  test: { environment: "jsdom", globals: true, setupFiles: ["./src/setupTests.ts"] },
});
