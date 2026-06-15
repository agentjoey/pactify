import { chromium } from "@playwright/test";
const b = await chromium.launch();
const p = await b.newPage({ viewport: { width: 1280, height: 820 }, deviceScaleFactor: 2 });
await p.goto("http://127.0.0.1:8080/", { waitUntil: "domcontentloaded" });
await p.waitForSelector('[data-testid="app-root"]'); await p.waitForTimeout(800);
await p.locator('[data-testid="sidebar-project-showcase"]').click(); await p.waitForTimeout(800);

// Setup view via sidebar function rail (RailIcon title / FooterItem label)
const setupNav = p.locator('[title="Setup"], button:has-text("Setup")').first();
if (await setupNav.count()) { await setupNav.click(); await p.waitForTimeout(900); }
let sv = p.locator('[data-testid="setup-view"]');
if (await sv.count()) {
  await sv.screenshot({ path: "/tmp/pactify-shots/logocard-setup.png" });
  console.log("setup card shot");
} else { console.log("setup-view not found"); }

// Ops view → AgentConfig (Settings)
const opsNav = p.locator('[title="Settings"], button:has-text("Settings")').first();
if (await opsNav.count()) { await opsNav.click(); await p.waitForTimeout(900); }
const ac = p.locator('[data-testid="ops-agent-config"]');
if (await ac.count()) {
  await ac.scrollIntoViewIfNeeded(); await p.waitForTimeout(300);
  await ac.screenshot({ path: "/tmp/pactify-shots/logocard-opsconfig.png" });
  console.log("ops config shot");
} else { console.log("ops-agent-config not found"); }
await b.close();
