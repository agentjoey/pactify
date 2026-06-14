import { chromium } from "@playwright/test";
import { mkdir } from "node:fs/promises";
const OUT = "/tmp/pactify-shots";
await mkdir(OUT, { recursive: true });
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 980, height: 1000 }, deviceScaleFactor: 1.5 });
await page.goto("http://127.0.0.1:7777/?gallery", { waitUntil: "domcontentloaded" });
await page.waitForTimeout(1200);
const titles = ["Badge", "Card", "节点卡 · 连接 + 关系", "Skeleton · 骨架屏", "画布效果 · 借鉴 Stitch"];
let i = 0;
for (const t of titles) {
  const h = page.locator("h2", { hasText: t }).first();
  if (await h.count()) {
    const sec = h.locator("xpath=ancestor::section[1]");
    await sec.scrollIntoViewIfNeeded();
    await page.waitForTimeout(400);
    await sec.screenshot({ path: `${OUT}/fix-${i}.png` });
    console.log("shot:", t);
  } else { console.log("MISSING:", t); }
  i++;
}
await browser.close();
