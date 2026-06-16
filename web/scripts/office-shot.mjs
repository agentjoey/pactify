import { chromium } from "@playwright/test";
const b = await chromium.launch();
const p = await b.newPage({ viewport: { width: 1280, height: 800 }, deviceScaleFactor: 1.5 });
await p.goto("http://127.0.0.1:7777/", { waitUntil: "domcontentloaded" });
await p.waitForSelector('[data-testid="app-root"]'); await p.waitForTimeout(1500);
await p.screenshot({ path: "/tmp/pactify-shots/app-canvas2.png" });
const office = p.locator('[data-testid="mode-office"]');
if (await office.count()) { await office.click(); await p.waitForTimeout(900); await p.screenshot({ path: "/tmp/pactify-shots/app-office.png" }); console.log("office shot"); }
console.log("done"); await b.close();
