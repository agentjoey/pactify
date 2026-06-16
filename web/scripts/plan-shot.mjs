import { chromium } from "@playwright/test";
const b = await chromium.launch();
const p = await b.newPage({ viewport: { width: 1320, height: 820 }, deviceScaleFactor: 1.5 });
await p.goto("http://127.0.0.1:7777/", { waitUntil: "domcontentloaded" });
await p.waitForSelector('[data-testid="app-root"]'); await p.waitForTimeout(1000);
const sc = p.locator('[data-testid="sidebar-project-showcase"]'); if (await sc.count()) { await sc.click(); await p.waitForTimeout(1200); }
const plan = p.locator('[data-testid="mode-plan"]'); if (await plan.count()) { await plan.click(); await p.waitForTimeout(1200); }
await p.screenshot({ path: "/tmp/pactify-shots/seed-plan.png" }); console.log("seed-plan");
await b.close();
