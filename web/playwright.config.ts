import { defineConfig, devices } from "@playwright/test";

// Playwright acceptance gate for the canvas (spec §4). The mock server serves
// the built SPA from ../internal/serve/dist, so the caller MUST build first:
//   npm run e2e        → npm run build && playwright test
//   npm run e2e:test   → playwright test (dist assumed already built)
// CI builds in its own step then runs e2e:test via `npm run e2e`.
const PORT = 4173;

export default defineConfig({
  testDir: "e2e",
  // Real-browser geometry/timing is the whole point of this gate; keep it serial
  // and deterministic rather than racing several Chromium contexts on one mock.
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["html", { open: "never" }], ["list"]] : "list",
  use: {
    baseURL: `http://localhost:${PORT}`,
    trace: "on-first-retry",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command: "node e2e/mock-server.mjs",
    port: PORT,
    reuseExistingServer: !process.env.CI,
    timeout: 30_000,
  },
});
