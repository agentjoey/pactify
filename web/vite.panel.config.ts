import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath, URL } from "node:url";

export default defineConfig({
  root: fileURLToPath(new URL("./panel", import.meta.url)),
  publicDir: fileURLToPath(new URL("./public", import.meta.url)),
  plugins: [react(), tailwindcss()],
  define: { "import.meta.env.PACTIFY_PANEL": JSON.stringify("true") },
  build: {
    outDir: fileURLToPath(
      new URL("../internal/subpanel/dist", import.meta.url),
    ),
    emptyOutDir: true,
  },
});
