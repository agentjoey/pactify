import { chromium } from "@playwright/test";
const b = await chromium.launch();
const p = await b.newPage({ viewport: { width: 1280, height: 800 }, deviceScaleFactor: 1.6 });
await p.goto("http://127.0.0.1:8080/", { waitUntil: "domcontentloaded" });
await p.waitForSelector('[data-testid="app-root"]'); await p.waitForTimeout(1000);
await p.locator('[data-testid="sidebar-project-showcase"]').click(); await p.waitForTimeout(1000);
const plan = p.locator('[data-testid="mode-plan"]'); if (await plan.count()) { await plan.click(); await p.waitForTimeout(1200); }
await p.screenshot({ path: "/tmp/pactify-shots/edges-colored.png" });
console.log("edges shot"); await b.close();
