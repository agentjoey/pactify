import { chromium } from "@playwright/test";
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1280, height: 800 }, deviceScaleFactor: 1.5 });
await page.goto("http://127.0.0.1:7777/", { waitUntil: "domcontentloaded" });
await page.waitForSelector('[data-testid="app-root"]', { timeout: 15000 });
await page.waitForTimeout(1500);
await page.screenshot({ path: "/tmp/pactify-shots/app-canvas.png" });
console.log("shot: app-canvas");
// board lens (key 2)
await page.keyboard.press("2"); await page.waitForTimeout(700);
await page.screenshot({ path: "/tmp/pactify-shots/app-board.png" });
console.log("shot: app-board");
// collapse sidebar
await page.locator('button[aria-label="hide sidebar"]').click(); await page.waitForTimeout(450);
await page.screenshot({ path: "/tmp/pactify-shots/app-collapsed.png" });
console.log("shot: app-collapsed");
await browser.close();
