import { chromium } from "@playwright/test";
const b = await chromium.launch();
const p = await b.newPage({ viewport: { width: 1000, height: 900 }, deviceScaleFactor: 2 });
await p.goto("http://localhost:4321/introduction/", { waitUntil: "networkidle" });
await p.waitForTimeout(500);
await p.screenshot({ path: "/tmp/pactify-shots/intro-full.png", fullPage: true });
console.log("intro full shot");
await b.close();
