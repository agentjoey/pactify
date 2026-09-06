// shot-settings-narrow.mjs — 视觉门：Settings 外壳在窄屏下的布局，取自最终 build。
// 该外壳的断点缺陷（固定 250px 侧栏把内容区挤到 ~140px）此前无任何覆盖。
import { spawn } from "node:child_process";
import { mkdir } from "node:fs/promises";
import { chromium } from "@playwright/test";
const PORT = Number(process.env.SHOT_PORT || 4188), BASE = `http://localhost:${PORT}`;
const OUT = process.env.SHOT_OUT || "/tmp/pactify-shots";
await mkdir(OUT, { recursive: true });
const srv = spawn("node", ["e2e/mock-server.mjs"], { env: { ...process.env, PORT: String(PORT) }, stdio: "ignore" });
try {
  for (let i = 0; ; i++) { try { await fetch(BASE + "/api/projects"); break; } catch { if (i > 50) throw new Error("no server"); await new Promise(r => setTimeout(r, 200)); } }
  const b = await chromium.launch();
  for (const [name, w, h] of [["390", 390, 844], ["768", 768, 900], ["1440", 1440, 900]]) {
    const p = await b.newPage({ viewport: { width: w, height: h }, deviceScaleFactor: 2 });
    await p.goto(BASE, { waitUntil: "domcontentloaded" });
    await p.locator('[data-testid="app-root"]').waitFor();
    await p.getByTestId("toolbar-settings").click();
    await p.getByTestId("settings-nav").waitFor();
    await p.waitForTimeout(500);
    const m = await p.evaluate(() => {
      const nav = document.querySelector('[data-testid="settings-nav"]').getBoundingClientRect();
      const panel = document.querySelector('[data-testid="settings-modal"],[data-testid="settings-view"]').getBoundingClientRect();
      // 内容区 = 面板宽减去（横排时的）侧栏宽
      const stacked = nav.width > panel.width * 0.9;
      return { viewport: window.innerWidth, navW: Math.round(nav.width),
               contentW: Math.round(stacked ? panel.width : panel.width - nav.width),
               stacked, hScroll: document.documentElement.scrollWidth > window.innerWidth };
    });
    console.log(`  ${name}px → 侧栏 ${m.navW}px · 内容区 ${m.contentW}px · ${m.stacked ? "纵向堆叠" : "横向双栏"} · 横滚 ${m.hScroll}`);
    await p.screenshot({ path: `${OUT}/settings-${name}.png`, fullPage: false });
    await p.close();
  }
  await b.close();
} finally { srv.kill(); }
