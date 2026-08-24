// fallback-card-shot.mjs — visual-gate fixture for the Dashboard fallback card.
//
// The card only renders when an env-class escalation has left a pending
// fallback proposal, which can't be triggered on demand (it needs a real agent
// to fail producing nothing). This intercepts the proposal API with a mock so
// the card's styling and its states can be visually reviewed. Point at a
// running serve (or the e2e mock server).
//
// Usage:
//   node web/scripts/fallback-card-shot.mjs            # → /tmp/pactify-shots/fallback-card*.png
//   SHOT_STATE=error node web/scripts/fallback-card-shot.mjs     # the failed-approval state
//   SHOT_STATE=loading node web/scripts/fallback-card-shot.mjs   # the in-flight state
//
// The read-only row of the state matrix has no shot: LocalServeSource always
// exposes approveFallback, so only a relay-backed source reaches that branch and
// this harness drives a local serve. It is covered by FallbackCard.test.tsx.
import { chromium } from "@playwright/test";
import { mkdir } from "node:fs/promises";

const BASE = process.env.SHOT_BASE || "http://127.0.0.1:17082";
const OUT = process.env.SHOT_OUT || "/tmp/pactify-shots";
const STATE = process.env.SHOT_STATE || "pending"; // pending | error | loading

// Wire shape = internal/serve's {"proposals": [...]}. For the multi-card view
// against the FINAL built bundle use scripts/shot-fallback-cards.mjs instead:
// this one intercepts the API against a long-running serve, which may be
// serving a stale dist.
const proposal = {
  scope: "p3",
  task: "p3-process-b",
  seat: "build2",
  fromRole: "frontend",
  toRole: "frontend-cheap",
  reason: "worker run: run timeout (--run-timeout) exceeded",
};

async function main() {
  await mkdir(OUT, { recursive: true });
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2 });

  await page.route("**/fallback-proposal", (r) => r.fulfill({ json: { proposals: [proposal] } }));
  if (STATE === "error") {
    // The approve path failing: the card must keep the proposal and show why.
    await page.route("**/fallback-proposal/approve", (r) =>
      r.fulfill({ status: 409, json: { error: "orchestrate is already running" } }),
    );
  } else if (STATE === "loading") {
    // Never fulfilled: the request stays in flight so the submitting state holds
    // still long enough to photograph.
    await page.route("**/fallback-proposal/approve", () => {});
  }

  await page.goto(BASE, { waitUntil: "domcontentloaded" });
  await page.waitForSelector('[data-testid="app-root"]', { timeout: 15000 });
  // The card lives on the Dashboard lens (Toolbar key "1"); Board is the default.
  await page.keyboard.press("1");
  await page.waitForTimeout(800);
  await page.waitForSelector('[data-testid="fallback-card"]', { timeout: 8000 });
  await page.waitForTimeout(500);

  if (STATE === "error") {
    await page.click('[data-testid="fallback-approve"]');
    await page.waitForSelector('[data-testid="fallback-error"]', { timeout: 5000 });
    await page.waitForTimeout(400);
  } else if (STATE === "loading") {
    await page.click('[data-testid="fallback-approve"]');
    await page.waitForSelector('[data-testid="fallback-approve"][disabled]', { timeout: 5000 });
    await page.waitForTimeout(400);
  }

  const name = STATE === "pending" ? "fallback-card" : `fallback-card-${STATE}`;
  await page.screenshot({ path: `${OUT}/${name}.png` });
  // A tight crop of the card itself, for reviewing type/spacing without hunting.
  const card = await page.$('[data-testid="fallback-card"]');
  if (card) await card.screenshot({ path: `${OUT}/${name}-crop.png` });
  await browser.close();
  console.log(`shot: ${name} -> ${OUT}/${name}.png (+ -crop.png)`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
