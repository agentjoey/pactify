import { chromium } from "@playwright/test";
const b = await chromium.launch();
const p = await b.newPage({ viewport: { width: 1320, height: 820 }, deviceScaleFactor: 1.5 });
await p.goto("http://127.0.0.1:7777/", { waitUntil: "domcontentloaded" });
await p.waitForSelector('[data-testid="app-root"]'); await p.waitForTimeout(1200);
// switch to showcase project in the sidebar
const sc = p.locator('[data-testid="sidebar-project-showcase"]');
if (await sc.count()) { await sc.click(); await p.waitForTimeout(1400); }
await p.screenshot({ path: "/tmp/pactify-shots/seed-canvas.png" });
console.log("seed-canvas");
// office
const office = p.locator('[data-testid="mode-office"]');
if (await office.count()) { await office.click(); await p.waitForTimeout(1200); await p.screenshot({ path: "/tmp/pactify-shots/seed-office.png" }); console.log("seed-office"); }
// board
await p.keyboard.press("2"); await p.waitForTimeout(800);
await p.screenshot({ path: "/tmp/pactify-shots/seed-board.png" }); console.log("seed-board");
await b.close();
