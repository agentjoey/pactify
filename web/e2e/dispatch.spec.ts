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
  // tierui-render: tier column (muted L2 + warn NO TIER) and the visible legend.
  await expect(page.getByTestId("tier-legend")).toContainText("L0 便宜 · L1 默认 · L2 复杂 · L3 高风险");
  await expect(page.getByTestId("tier-badge").nth(0)).toHaveText("L2");
  await expect(page.getByTestId("tier-badge").nth(1)).toHaveText("NO TIER");
  await expect(page.getByText("correctness · backend")).toBeVisible();
  await page.getByTestId("dispatch-confirm").click();
  await page.getByTestId("dispatch-done").waitFor();
});
