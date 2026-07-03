// live-gate-shot.mjs — visual-gate fixture for the Live orchestrate review gate.
//
// The escalated / review-gate state only appears when a real orchestrate run
// hits its hard-gate, which can't be triggered on demand. This captures it by
// intercepting the orchestrate-status API with a mock escalated payload, so the
// dark review-gate styling can be visually reviewed. Point at a running serve.
//
// Usage: node web/scripts/live-gate-shot.mjs   # → /tmp/pactify-shots/live-gate.png
import { chromium } from "@playwright/test";
import { mkdir } from "node:fs/promises";

const BASE = process.env.SHOT_BASE || "http://127.0.0.1:17082";
const OUT = process.env.SHOT_OUT || "/tmp/pactify-shots";

const escalated = {
  present: true,
  status: {
    running: false,
    action: "stuck",
    phase: "escalated",
    escalated: true,
    reason: "⊘ Hard test gate failed — TestRetryCap FAIL (human decision required)",
    done: false,
    total: 4,
    accepted: 2,
    seat: "opencode",
    task: "t-harden",
    feature: "feat-rh",
  },
};

async function main() {
  await mkdir(OUT, { recursive: true });
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2 });
  // Inject the escalated state; no parallel run.
  await page.route("**/orchestrate/status", (r) => r.fulfill({ json: escalated }));
  await page.route("**/orchestrate/parallel", (r) => r.fulfill({ json: { present: false } }));

  await page.goto(BASE, { waitUntil: "domcontentloaded" });
  await page.waitForSelector('[data-testid="app-root"]', { timeout: 15000 });
  await page.waitForTimeout(1000);
  await page.keyboard.press("3"); // Live lens
  await page.waitForSelector('[data-testid="escalated-banner"]', { timeout: 5000 });
  await page.waitForTimeout(600);
  await page.screenshot({ path: `${OUT}/live-gate.png` });
  await browser.close();
  console.log(`shot: live-gate -> ${OUT}/live-gate.png`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
