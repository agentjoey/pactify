// One-off: regenerate /tmp/pactify-shots/dispatch-review-tier.png from the
// FINAL built dist (web/e2e/mock-server.mjs serves ../internal/serve/dist).
import { spawn } from "node:child_process";
import { chromium } from "@playwright/test";

const WEB = "/Users/xtation/AgentWorks/Code_Claude/pactify/web";
const BASE = "http://localhost:4173";

const srv = spawn("node", ["e2e/mock-server.mjs"], { cwd: WEB, stdio: "ignore" });
try {
  // wait for the mock server
  for (let i = 0; ; i++) {
    try {
      await fetch(BASE + "/api/projects");
      break;
    } catch {
      if (i > 50) throw new Error("mock server did not start");
      await new Promise((r) => setTimeout(r, 200));
    }
  }
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2 });
  await page.goto(BASE, { waitUntil: "domcontentloaded" });
  await page.locator('[data-testid="app-root"]').waitFor();
  await page.request.post(BASE + "/__test/reset");
  await page.getByTestId("lens-cockpit").click();
  await page.getByTestId("toolbar-dispatch").click();
  await page.getByTestId("dispatch-panel").waitFor();
  await page.getByTestId("dispatch-goal").fill("add 2fa");
  await page.getByTestId("dispatch-feature").fill("add-2fa");
  await page.getByTestId("dispatch-generate").click();
  await page.getByTestId("dispatch-review").waitFor();
  await page.waitForTimeout(800);
  await page.screenshot({ path: "/tmp/pactify-shots/dispatch-review-tier.png" });
  await browser.close();
  console.log("shot: /tmp/pactify-shots/dispatch-review-tier.png");
} finally {
  srv.kill();
}
