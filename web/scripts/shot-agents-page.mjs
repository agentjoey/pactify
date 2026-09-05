// shot-agents-page.mjs — 视觉门：Settings · Agents 合并页，取自最终 build。
// spawn e2e/mock-server.mjs（直服 ../internal/serve/dist），保证截的是已提交产物，
// 而不是常驻 daemon 可能提供的陈旧 dist。
import { spawn } from "node:child_process";
import { mkdir } from "node:fs/promises";
import { chromium } from "@playwright/test";
const PORT = Number(process.env.SHOT_PORT || 4182), BASE = `http://localhost:${PORT}`;
const OUT = process.env.SHOT_OUT || "/tmp/pactify-shots";
await mkdir(OUT, { recursive: true });
const srv = spawn("node", ["e2e/mock-server.mjs"], { env: { ...process.env, PORT: String(PORT) }, stdio: "ignore" });
try {
  for (let i = 0; ; i++) { try { await fetch(BASE + "/api/projects"); break; } catch { if (i > 50) throw new Error("no server"); await new Promise(r => setTimeout(r, 200)); } }
  const b = await chromium.launch();
  for (const [name, w, h] of [["desktop", 1440, 1100], ["mobile", 390, 900]]) {
    const p = await b.newPage({ viewport: { width: w, height: h }, deviceScaleFactor: 2 });
    await p.goto(BASE, { waitUntil: "domcontentloaded" });
    await p.locator('[data-testid="app-root"]').waitFor();
    await p.getByTestId("toolbar-settings").click();
    await p.getByTestId("agents-installed").waitFor();
    // 展开一行 + 跑一次失败的 Test，让证据覆盖关键状态而不是只有静默列表
    await p.getByTestId("agent-disclosure-codex-cli").click();
    await p.getByTestId("agent-test-kimi-cli").click();
    await p.getByTestId("agent-test-result-kimi-cli").waitFor();
    await p.waitForTimeout(400);
    if (name === "mobile") {
      const over = await p.evaluate(() => { const o = []; for (const el of document.querySelectorAll('[data-testid^="agent-row-"] *')) { const r = el.getBoundingClientRect(); if (r.right > window.innerWidth + 1) o.push(el.className || el.tagName); } return o.slice(0, 4); });
      console.log("窄屏溢出:", over.length ? over : "无");
    }
    await p.screenshot({ path: `${OUT}/agents-page-${name}.png`, fullPage: true });
    await p.close();
  }
  await b.close();
  console.log("视觉门截图完成");
} finally { srv.kill(); }
