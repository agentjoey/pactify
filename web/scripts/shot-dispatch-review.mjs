// shot-dispatch-review.mjs — screenshot of the dispatch plan-review panel
// (tier badges), taken from the FINAL built dist.
//
// Unlike scripts/shots.mjs (which points at an already-running server — the
// long-lived daemon may serve a STALE dist), this spawns e2e/mock-server.mjs,
// which serves ../internal/serve/dist directly, so the shot is guaranteed to
// come from the committed build. Different contracts — do not merge the two.
//
// Usage:
//   cd web && npm run build && node scripts/shot-dispatch-review.mjs
//   SHOT_OUT=./shot.png SHOT_PORT=4200 node scripts/shot-dispatch-review.mjs
import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { chromium } from "@playwright/test";

const __dirname = dirname(fileURLToPath(import.meta.url));
const WEB = join(__dirname, "..");
const OUT = process.env.SHOT_OUT || "/tmp/pactify-shots/dispatch-review-tier.png";
// NOT 4173: that's playwright.config.ts's webServer port, and its
// reuseExistingServer (!CI) would collide with or silently attach to a
// concurrently running `playwright test`.
const PORT = Number(process.env.SHOT_PORT || 4174);
const BASE = `http://localhost:${PORT}`;

const srv = spawn("node", ["e2e/mock-server.mjs"], {
  cwd: WEB,
  env: { ...process.env, PORT: String(PORT) },
  stdio: "ignore",
});
try {
  // wait for the mock server
  for (let i = 0; ; i++) {
    try {
      await fetch(BASE + "/api/projects");
      break;
    } catch {
      if (i > 50) throw new Error("mock server did not start");
      await new Promise((r) => setTimeout(r, 200));
    }
  }
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2 });
  // Reset BEFORE goto (e2e/helpers.ts order): the app reads state on boot, so
  // a post-goto reset would leave the first render on stale state.
  await page.request.post(BASE + "/__test/reset");
  await page.goto(BASE, { waitUntil: "domcontentloaded" });
  await page.locator('[data-testid="app-root"]').waitFor();
  await page.getByTestId("lens-cockpit").click();
  await page.getByTestId("toolbar-dispatch").click();
  await page.getByTestId("dispatch-panel").waitFor();
  await page.getByTestId("dispatch-goal").fill("add 2fa");
  await page.getByTestId("dispatch-feature").fill("add-2fa");
  await page.getByTestId("dispatch-generate").click();
  await page.getByTestId("dispatch-review").waitFor();
  await page.waitForTimeout(800);
  await page.screenshot({ path: OUT });
  await browser.close();
  console.log(`shot: ${OUT}`);
} finally {
  srv.kill();
}
