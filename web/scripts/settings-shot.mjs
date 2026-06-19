import { chromium } from "@playwright/test";
import { mkdir } from "node:fs/promises";

const OUT = process.env.SHOT_OUT || "/tmp/pactify-shots";
const BASE = process.env.SHOT_BASE || "http://127.0.0.1:8080/";

await mkdir(OUT, { recursive: true });
const b = await chromium.launch();
const p = await b.newPage({ viewport: { width: 1200, height: 1000 }, deviceScaleFactor: 2 });
await p.goto(BASE, { waitUntil: "domcontentloaded" });
await p.waitForSelector('[data-testid="app-root"]');
await p.waitForTimeout(700);

// select the first project in the sidebar if one exists
const firstProject = p.locator('[data-testid^="sidebar-project-"]').first();
if (await firstProject.count()) {
  await firstProject.click();
  await p.waitForTimeout(700);
}

// open Settings (toolbar gear)
await p.locator('[title="Settings"], button:has-text("Settings")').first().click();
await p.waitForTimeout(900);

// Full modal screenshot (centered dark sheet + dimmed backdrop)
await p.locator('[data-testid="settings-modal"]').screenshot({ path: `${OUT}/settings-modal.png` });
console.log("settings modal shot");

await b.close();
