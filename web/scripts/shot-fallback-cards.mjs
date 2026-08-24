// shot-fallback-cards.mjs — screenshot of the Dashboard's fallback stack with
// SEVERAL pending proposals (one card per proposal), taken from the FINAL built
// dist.
//
// Same contract as scripts/shot-dispatch-review.mjs and deliberately not merged
// with scripts/fallback-card-shot.mjs: that one intercepts the API against an
// already-running serve, whose long-lived daemon may be serving a STALE dist.
// This spawns e2e/mock-server.mjs, which serves ../internal/serve/dist directly
// and answers the REAL endpoints (no route interception), so the shot proves the
// built bundle renders the real list response.
//
// Usage:
//   cd web && npm run build && node scripts/shot-fallback-cards.mjs
//   SHOT_OUT=./shot.png SHOT_PORT=4200 node scripts/shot-fallback-cards.mjs
import { spawn } from "node:child_process";
import { mkdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { chromium } from "@playwright/test";

const __dirname = dirname(fileURLToPath(import.meta.url));
const WEB = join(__dirname, "..");
const OUT = process.env.SHOT_OUT || "/tmp/pactify-shots/fallback-cards.png";
// NOT 4173: that's playwright.config.ts's webServer port, and its
// reuseExistingServer (!CI) would collide with or silently attach to a
// concurrently running `playwright test`.
const PORT = Number(process.env.SHOT_PORT || 4175);
const BASE = `http://localhost:${PORT}`;

// Two independently paused features — what --max-concurrency > 1 produces, and
// the case a single card could not honestly represent.
const proposals = [
  {
    scope: "add-2fa",
    task: "add-2fa-otp",
    seat: "build2",
    fromRole: "frontend",
    toRole: "frontend-cheap",
    reason: "worker run: run timeout (--run-timeout) exceeded",
  },
  {
    scope: "billing-webhooks",
    task: "billing-webhooks-retry",
    seat: "build3",
    fromRole: "backend",
    toRole: "backend-local",
    reason:
      "worker run: provider returned 429 (quota exhausted)\nno output written to the worktree",
  },
];

const srv = spawn("node", ["e2e/mock-server.mjs"], {
  cwd: WEB,
  env: { ...process.env, PORT: String(PORT) },
  stdio: "ignore",
});
try {
  for (let i = 0; ; i++) {
    try {
      await fetch(BASE + "/api/projects");
      break;
    } catch {
      if (i > 50) throw new Error("mock server did not start");
      await new Promise((r) => setTimeout(r, 200));
    }
  }
  await mkdir(dirname(OUT), { recursive: true });
  const browser = await chromium.launch();
  const page = await browser.newPage({
    viewport: { width: 1440, height: 900 },
    deviceScaleFactor: 2,
  });
  // Reset BEFORE goto (e2e/helpers.ts order): the app reads state on boot, so a
  // post-goto reset would leave the first render on stale state.
  await page.request.post(BASE + "/__test/reset");
  await page.goto(BASE, { waitUntil: "domcontentloaded" });
  await page.locator('[data-testid="app-root"]').waitFor();
  await page.getByTestId("lens-dashboard").click();
  // Armed AFTER the dashboard is up, which is also the real sequence: the
  // escalation happens while the operator is already watching the run.
  await page.request.post(BASE + "/__test/fallback", { data: proposals });
  const cards = page.getByTestId("fallback-card");
  await cards.nth(proposals.length - 1).waitFor({ timeout: 20_000 });
  await page.waitForTimeout(400);

  await page.screenshot({ path: OUT });
  const stack = await page.$('[data-testid="fallback-cards"]');
  const crop = OUT.replace(/\.png$/, "-crop.png");
  if (stack) await stack.screenshot({ path: crop });
  await browser.close();
  console.log(`shot: ${OUT} (+ ${crop})`);
} finally {
  srv.kill();
}
