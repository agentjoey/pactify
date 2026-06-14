import { chromium } from "@playwright/test";
const b = await chromium.launch();
const p = await b.newPage({ viewport: { width: 1320, height: 820 }, deviceScaleFactor: 1.5 });
await p.goto("http://127.0.0.1:7777/", { waitUntil: "domcontentloaded" });
await p.waitForSelector('[data-testid="app-root"]'); await p.waitForTimeout(1000);
const sc = p.locator('[data-testid="sidebar-project-showcase"]'); if (await sc.count()) { await sc.click(); await p.waitForTimeout(1200); }
await p.keyboard.press("2"); await p.waitForTimeout(800); // board
const card = p.locator('[data-testid="board-pulse"], .task-card').first();
// click t1 (accepted) card to open inspector
const t1 = p.locator('text=t1').first();
if (await t1.count()) { await t1.click(); await p.waitForTimeout(900); }
await p.screenshot({ path: "/tmp/pactify-shots/inspector-duration.png" });
console.log("inspector shot"); await b.close();
