import { test, expect } from "@playwright/test";
import { resetServer, gotoApp } from "./helpers";

test.beforeEach(async ({ page }) => {
  await resetServer(page);
  await gotoApp(page);
});

test("dispatch: goal → generate → review → dispatch", async ({ page }) => {
  // Dispatch (◉) lives in the Cockpit lens — Toolbar renders toolbar-dispatch
  // only when lens === "cockpit". Switch to it first (mirrors App.test.tsx).
  await page.getByTestId("lens-cockpit").click();
  await page.getByTestId("toolbar-dispatch").click();
  await page.getByTestId("dispatch-panel").waitFor();
  await page.getByTestId("dispatch-goal").fill("add 2fa");
  await page.getByTestId("dispatch-feature").fill("add-2fa");
  await page.getByTestId("dispatch-generate").click();
  await page.getByTestId("dispatch-review").waitFor();
  await expect(page.getByText("add-2fa-otp")).toBeVisible();
  await page.getByTestId("dispatch-confirm").click();
  await page.getByTestId("dispatch-done").waitFor();
});
