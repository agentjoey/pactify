import { chromium } from "@playwright/test";
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1280, height: 760 }, deviceScaleFactor: 1.5 });
await page.goto("http://127.0.0.1:7777/?shell", { waitUntil: "domcontentloaded" });
await page.waitForTimeout(1200);
await page.screenshot({ path: "/tmp/pactify-shots/shell.png" });
console.log("shot: shell");
await browser.close();
