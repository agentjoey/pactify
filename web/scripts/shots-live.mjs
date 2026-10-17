// Final embedded dashboard and panel builds, isolated mock, no daemon/harness.
import { mkdir } from "node:fs/promises";
import { resolve } from "node:path";
import { chromium } from "@playwright/test";
import { startMock } from "../e2e/mock-server.mjs";
const output = resolve(process.env.LIVE_SHOTS_DIR || "/tmp/pactify-shots/live");
await mkdir(output, { recursive: true });
let browser;
try {
  browser = await chromium.launch();
  for (const panel of [false, true]) {
    const { server, baseURL } = await startMock(0, panel);
    try {
      for (const [device, width, height] of [
        ["desktop", 1440, 1100],
        ["mobile", 375, 812],
      ]) {
        const page = await browser.newPage({
          viewport: { width, height },
          colorScheme: "dark",
          reducedMotion: "reduce",
        });
        await page.goto(`${baseURL}/#/dashboard`);
        await page.getByText("3 observed calls").waitFor();
        await page.evaluate(() => document.fonts.ready);
        if (
          await page.evaluate(
            () => document.documentElement.scrollWidth > innerWidth,
          )
        )
          throw new Error("Horizontal overflow");
        const prefix = panel ? "panel" : "web";
        const capture = async (view, fullPage) => {
          const path = resolve(output, `${prefix}-${view}-${device}.png`);
          await page.screenshot({ path, fullPage });
          console.log(path);
        };
        await capture("dashboard", true);
        await page.goto(`${baseURL}/#/jobs/job-review`);
        const drawer = page.getByRole("dialog", { name: "Job details" });
        await drawer.getByRole("tab", { name: "Live", exact: true }).click();
        await drawer.getByText("3 observed calls").waitFor();
        await drawer
          .getByRole("heading", { name: "Session log", exact: true })
          .waitFor();
        await drawer.evaluate(async (element) => {
          element.scrollTop = 0;
          await new Promise(requestAnimationFrame);
        });
        await capture("detail", false);
        await drawer.evaluate(async (element) => {
          element.scrollTop = element.scrollHeight;
          await new Promise(requestAnimationFrame);
        });
        await capture("detail-bottom", false);
        await page.close();
      }
    } finally {
      server.closeAllConnections();
      await new Promise((r) => server.close(r));
    }
  }
} finally {
  await browser?.close();
}
