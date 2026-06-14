// shots.mjs — UI self-review screenshot harness.
//
// Captures each dashboard view (and a few notable states) to PNGs so the
// developer/agent can VISUALLY review the rendered UI instead of working blind.
// Point it at a running `pactify serve` (real data) or the e2e mock-server.
//
// Usage:
//   node web/scripts/shots.mjs                 # → /tmp/pactify-shots, BASE=127.0.0.1:7777
//   SHOT_BASE=http://localhost:5173 SHOT_OUT=./shots node web/scripts/shots.mjs
import { chromium } from "@playwright/test";
import { mkdir } from "node:fs/promises";

const BASE = process.env.SHOT_BASE || "http://127.0.0.1:7777";
const OUT = process.env.SHOT_OUT || "/tmp/pactify-shots";

const VIEWS = [
  ["kanban", "1"],
  ["canvas", "2"],
  ["ops", "3"],
  ["live", "4"],
  ["plan", "5"],
  ["setup", "6"],
  ["recipes", "7"],
];

async function main() {
  await mkdir(OUT, { recursive: true });
  const browser = await chromium.launch();
  const page = await browser.newPage({
    viewport: { width: 1440, height: 900 },
    deviceScaleFactor: 2, // crisp retina shots for review
  });

  // domcontentloaded (not networkidle): the dashboard holds an SSE connection
  // open, so the network is never idle.
  await page.goto(BASE, { waitUntil: "domcontentloaded" });
  await page.waitForSelector('[data-testid="app-root"]', { timeout: 15000 });
  await page.waitForTimeout(1000);

  for (const [name, key] of VIEWS) {
    await page.keyboard.press(key);
    await page.waitForTimeout(900); // let view-enter + data settle
    await page.screenshot({ path: `${OUT}/${name}.png` });
    console.log(`shot: ${name}`);
  }

  // Notable state: Recipes with a goal typed (live expand preview).
  await page.keyboard.press("7");
  await page.waitForTimeout(400);
  const goal = page.locator('[data-testid="recipe-goal"]');
  if (await goal.count()) {
    await goal.fill("给 relay 加重试上限可配");
    await page.waitForTimeout(1000);
    await page.screenshot({ path: `${OUT}/recipes-filled.png` });
    console.log("shot: recipes-filled");
  }

  // Notable state: Ops scrolled down to the Agent config panel.
  await page.keyboard.press("3");
  await page.waitForTimeout(600);
  const acfg = page.locator('[data-testid="ops-agent-config"]');
  if (await acfg.count()) {
    await acfg.scrollIntoViewIfNeeded();
    await page.waitForTimeout(300);
    await page.screenshot({ path: `${OUT}/ops-agent-config.png` });
    console.log("shot: ops-agent-config");
  }

  await browser.close();
  console.log(`\n${VIEWS.length + 2} shots → ${OUT}`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
