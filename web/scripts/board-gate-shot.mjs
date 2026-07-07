// board-gate-shot.mjs — visual-gate fixture for the Board run-rail review gate.
//
// The escalated / review-gate state only appears when a real orchestrate run
// hits its hard-gate, which can't be triggered on demand. This captures it by
// intercepting the orchestrate-status API with a mock escalated payload, so the
// review-gate styling (RunRail lane + five-action panel) can be visually
// reviewed. Point at a running serve (or the e2e mock server).
//
// Usage: node web/scripts/board-gate-shot.mjs  # → /tmp/pactify-shots/board-gate.png
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
    task: process.env.SHOT_TASK || "t2",
    feature: process.env.SHOT_FEATURE || "f1",
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
  // Single-view IA: the run rail renders on the Board when the driver reports
  // an escalated run — wait for the five-action review gate to mount.
  await page.waitForSelector('[data-testid="review-gate"]', { timeout: 5000 });
  await page.waitForTimeout(600);
  await page.screenshot({ path: `${OUT}/board-gate.png` });
  await browser.close();
  console.log(`shot: board-gate -> ${OUT}/board-gate.png`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
