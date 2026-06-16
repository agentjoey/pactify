import { chromium } from "@playwright/test";
import { mkdir } from "node:fs/promises";
const OUT = "/tmp/pactify-shots";
await mkdir(OUT, { recursive: true });
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1240, height: 900 }, deviceScaleFactor: 1.5 });
await page.goto("http://127.0.0.1:7777/", { waitUntil: "domcontentloaded" });
await page.waitForSelector('[data-testid="app-root"]', { timeout: 15000 });
await page.waitForTimeout(1000);
await page.keyboard.press("6"); // Setup
await page.waitForTimeout(1000);
await page.screenshot({ path: `${OUT}/setup.png` });
console.log("shot: setup");
// toggle worker off on one seat to capture the warning state
const t = page.locator('[data-testid="role-gemini-worker"]');
if (await t.count()) { await t.click(); await page.waitForTimeout(500);
  // also toggle opencode worker off so a real gap appears
  const t2 = page.locator('[data-testid="role-opencode-worker"]');
  if (await t2.count()) { await t2.click(); await page.waitForTimeout(500); }
  await page.screenshot({ path: `${OUT}/setup-warn.png` }); console.log("shot: setup-warn"); }
await browser.close();
