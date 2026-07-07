// shots.mjs — UI self-review screenshot harness.
//
// Captures each dashboard view to PNGs so the developer/agent can VISUALLY review
// the rendered UI instead of working blind. Point it at a running `pactify serve`
// (real data, the launchd default :17082) or a `vite` dev server.
//
// Usage:
//   node web/scripts/shots.mjs                 # all views → /tmp/pactify-shots
//   node web/scripts/shots.mjs board           # just one view
//   SHOT_BASE=http://localhost:5173 SHOT_OUT=./shots node web/scripts/shots.mjs
import { chromium } from "@playwright/test";
import { mkdir } from "node:fs/promises";

const BASE = process.env.SHOT_BASE || "http://127.0.0.1:17082";
const OUT = process.env.SHOT_OUT || "/tmp/pactify-shots";

// Views consolidation (2026-07): the Board is the only lens; Settings is a
// modal (⚙). Older 7-view
// keymap is gone — keep this list in sync with shell/Toolbar LENSES.
const VIEWS = [
  ["board", "1"],
];

async function main() {
  const only = process.argv[2]; // optional single view name
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
  await page.waitForTimeout(1200);

  for (const [name, key] of VIEWS) {
    if (only && only !== name) continue;
    await page.keyboard.press(key);
    await page.waitForTimeout(1000); // let view-enter + data settle
    await page.screenshot({ path: `${OUT}/${name}.png` });
    console.log(`shot: ${name}`);
  }

  // Settings modal (opened from the toolbar ⚙, not a lens key).
  if (!only || only === "settings") {
    const gear = page.locator('[data-testid="toolbar-settings"]');
    if (await gear.count()) {
      await gear.click();
      await page.waitForTimeout(700);
      await page.screenshot({ path: `${OUT}/settings.png` });
      console.log("shot: settings");
    }
  }

  await browser.close();
  console.log(`\nshots → ${OUT}`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
