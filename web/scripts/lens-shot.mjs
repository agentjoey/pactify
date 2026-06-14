import { chromium } from "@playwright/test";
const b = await chromium.launch();
const p = await b.newPage({ viewport: { width: 1280, height: 800 }, deviceScaleFactor: 1.6 });
await p.goto("http://127.0.0.1:8080/", { waitUntil: "domcontentloaded" });
await p.waitForSelector('[data-testid="app-root"]'); await p.waitForTimeout(1000);
await p.locator('[data-testid="sidebar-project-showcase"]').click(); await p.waitForTimeout(1000);
const office = p.locator('[data-testid="mode-office"]'); if (await office.count()) { await office.click(); await p.waitForTimeout(1000); }
// click the Cost lens
const cost = p.locator('[data-testid="office-lens"] button', { hasText: "Cost" });
if (await cost.count()) { await cost.click(); await p.waitForTimeout(900); }
await p.screenshot({ path: "/tmp/pactify-shots/office-cost-lens.png", clip: { x: 820, y: 30, width: 460, height: 300 } });
console.log("lens shot"); await b.close();
