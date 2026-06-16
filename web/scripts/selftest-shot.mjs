import { chromium } from "@playwright/test";
const b = await chromium.launch();
const p = await b.newPage({ viewport: { width: 1280, height: 800 }, deviceScaleFactor: 1.5 });
await p.goto("http://127.0.0.1:8080/", { waitUntil: "domcontentloaded" });
await p.waitForSelector('[data-testid="app-root"]'); await p.waitForTimeout(1000);
await p.locator('[data-testid="sidebar-project-showcase"]').click(); await p.waitForTimeout(1200);
// 1) inspector: click a task on canvas
await p.locator('text=t2').first().click(); await p.waitForTimeout(800);
await p.screenshot({ path: "/tmp/pactify-shots/st-inspector.png" }); console.log("inspector");
// close inspector, go office
await p.keyboard.press("Escape"); await p.waitForTimeout(300);
const office = p.locator('[data-testid="mode-office"]'); if (await office.count()) { await office.click(); await p.waitForTimeout(1000); }
await p.screenshot({ path: "/tmp/pactify-shots/st-office.png" }); console.log("office");
await b.close();
