// cd web && npm run build:panel && node scripts/shots-panel.mjs
// Uses a new random port and the final internal/subpanel/dist build, never a daemon.
import { mkdtemp } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { chromium } from "@playwright/test";
import { startPanelMock } from "../e2e/panel-mock.mjs";

const output = await mkdtemp(join(tmpdir(), "pactify-panel-shots-"));
const { server, baseURL } = await startPanelMock();
let browser;
try {
  browser = await chromium.launch();
  for (const theme of ["light", "dark"]) {
    for (const [device, width, height] of [
      ["desktop", 1440, 1000],
      ["mobile", 375, 812],
    ]) {
      const context = await browser.newContext({
        viewport: { width, height },
        colorScheme: theme,
        deviceScaleFactor: 1,
      });
      const page = await context.newPage();
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.goto(baseURL);
      await page
        .getByRole("link", { name: "Example-659 · Local read-only job panel" })
        .waitFor();
      await page.evaluate(() => document.fonts.ready);
      for (const view of ["list", "detail"]) {
        if (view === "detail") {
          await page
            .getByRole("link", { name: "Example-659 · Local read-only job panel" })
            .click();
          await page.getByRole("tab", {name: "Logs", exact: true}).click();
          await page.getByText("Finished", { exact: true }).waitFor();
        }
        if (
          await page.evaluate(
            () => document.documentElement.scrollWidth > window.innerWidth,
          )
        )
          throw new Error(`horizontal overflow: ${theme} ${device} ${view}`);
        if (errors.length) throw new Error(errors.join("\n"));
        const path = join(output, `${view}-${theme}-${device}.png`);
        await page.screenshot({ path, fullPage: true });
        console.log(path);
      }
      await context.close();
    }
  }
} finally {
  await browser?.close();
  server.closeAllConnections();
  await new Promise((resolve) => server.close(resolve));
}
