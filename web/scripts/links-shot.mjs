import { chromium } from "@playwright/test";
const b = await chromium.launch();
const p = await b.newPage({ viewport: { width: 1280, height: 800 }, deviceScaleFactor: 1.5 });
await p.goto("http://127.0.0.1:8080/", { waitUntil: "domcontentloaded" });
await p.waitForSelector('[data-testid="app-root"]'); await p.waitForTimeout(1000);
const sc = p.locator('[data-testid="sidebar-project-showcase"]'); if (await sc.count()) { await sc.click(); await p.waitForTimeout(1000); }
const office = p.locator('[data-testid="mode-office"]'); if (await office.count()) { await office.click(); await p.waitForTimeout(1000); }
// click claude desk header (the dhead) to open Links
const desk = p.locator('[data-testid="desk-claude"] .dhead');
if (await desk.count()) { await desk.click(); await p.waitForTimeout(700); }
await p.screenshot({ path: "/tmp/pactify-shots/office-links.png" });
console.log("links shot", await p.locator('[data-testid="office-links"]').count()); await b.close();
