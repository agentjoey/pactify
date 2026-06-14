import { chromium } from "@playwright/test";
const b = await chromium.launch();
const p = await b.newPage({ viewport: { width: 1280, height: 800 }, deviceScaleFactor: 1.5 });
await p.goto("http://127.0.0.1:8080/", { waitUntil: "domcontentloaded" });
await p.waitForSelector('[data-testid="app-root"]'); await p.waitForTimeout(1000);
const sc = p.locator('[data-testid="sidebar-project-showcase"]'); if (await sc.count()) { await sc.click(); await p.waitForTimeout(1000); }
const office = p.locator('[data-testid="mode-office"]'); if (await office.count()) { await office.click(); await p.waitForTimeout(1000); }
await p.screenshot({ path: "/tmp/pactify-shots/office-2.5d.png" });
console.log("office shot"); await b.close();
