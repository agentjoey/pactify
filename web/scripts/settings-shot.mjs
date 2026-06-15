import { chromium } from "@playwright/test";
const b = await chromium.launch();
const p = await b.newPage({ viewport: { width: 1200, height: 1000 }, deviceScaleFactor: 2 });
await p.goto("http://127.0.0.1:8080/", { waitUntil: "domcontentloaded" });
await p.waitForSelector('[data-testid="app-root"]'); await p.waitForTimeout(700);
await p.locator('[data-testid="sidebar-project-showcase"]').click(); await p.waitForTimeout(700);
// open Settings (Ops)
await p.locator('[title="Settings"], button:has-text("Settings")').first().click();
await p.waitForTimeout(900);
// expand manual-add if present
const man = p.locator('[data-testid="manual-add-toggle"]');
if (await man.count()) { await man.click(); await p.waitForTimeout(400); }
const roster = p.locator('[data-testid="ops-agent-roster"]');
if (await roster.count()) {
  await roster.scrollIntoViewIfNeeded(); await p.waitForTimeout(200);
  await roster.screenshot({ path: "/tmp/pactify-shots/settings-roster.png" });
  console.log("roster shot");
}
const cfg = p.locator('[data-testid="ops-agent-config"]');
if (await cfg.count()) {
  await cfg.scrollIntoViewIfNeeded(); await p.waitForTimeout(200);
  await cfg.screenshot({ path: "/tmp/pactify-shots/settings-config.png" });
  console.log("config shot");
}
await b.close();
