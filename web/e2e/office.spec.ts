import { test, expect } from "@playwright/test";
import { resetServer, gotoApp, centerOf, html5DragTo } from "./helpers";

// Office-mode regressions — bugs #3 (no authoring surface) and #4 (no zoom).
// Office is the DEFAULT landing mode, so no mode switch is needed.

test.beforeEach(async ({ page }) => {
  await resetServer(page);
  await gotoApp(page);
  // Confirm we are on the office surface (the default).
  await page.locator('[data-testid="office-view"]').waitFor();
});

// scaleOf reads the scale() factor off the react-flow viewport transform.
async function scaleOf(page: import("@playwright/test").Page): Promise<number> {
  return page.evaluate(() => {
    const vp = document.querySelector<HTMLElement>(".react-flow__viewport");
    const t = vp?.style.transform || "";
    const m = /scale\(([^)]+)\)/.exec(t);
    return m ? parseFloat(m[1]) : 1;
  });
}

// Bug #4: Office must zoom (wheel + HUD buttons), and fit must reset.
test("office-zoom: wheel + HUD buttons change viewport scale; fit resets", async ({ page }) => {
  // The HUD lives inside the office ReactFlow.
  await page.locator('[data-testid="canvas-hud"]').waitFor();
  // Let fitView settle so the baseline scale is stable.
  await page.waitForTimeout(400);

  const base = await scaleOf(page);

  // Wheel zoom at the canvas center (negative deltaY = zoom in for RF). Assert it
  // CHANGED the scale (direction not asserted: a small graph can fit near RF's
  // maxZoom, where zoom-in saturates — see the HUD assertions below for direction).
  const stage = page.locator('[data-testid="office-view"]');
  const c = await centerOf(stage);
  await page.mouse.move(c.x, c.y);
  await page.mouse.wheel(0, -400);
  await page.waitForTimeout(200);
  const afterWheel = await scaleOf(page);
  expect(afterWheel).not.toBeCloseTo(base, 3);

  // HUD direction test from a KNOWN mid baseline: zoom out twice off the (possibly
  // saturated) wheel scale so a subsequent zoom-in has headroom to increase.
  await page.getByRole("button", { name: "zoom out" }).click();
  await page.getByRole("button", { name: "zoom out" }).click();
  await page.waitForTimeout(200);
  const mid = await scaleOf(page);

  await page.getByRole("button", { name: "zoom in" }).click();
  await page.waitForTimeout(200);
  const zoomedIn = await scaleOf(page);
  expect(zoomedIn).toBeGreaterThan(mid);

  await page.getByRole("button", { name: "zoom out" }).click();
  await page.waitForTimeout(200);
  const zoomedOut = await scaleOf(page);
  expect(zoomedOut).toBeLessThan(zoomedIn);

  // Fit resets (the button is reachable and produces a finite, positive scale).
  await page.getByRole("button", { name: "fit view" }).click();
  await page.waitForTimeout(300);
  const fitScale = await scaleOf(page);
  expect(Number.isFinite(fitScale)).toBeTruthy();
  expect(fitScale).toBeGreaterThan(0);
});

// Bug #3: Office is the default mode and must offer authoring. Create a feature
// and a task, then dispatch the draft onto an idle desk via the dock → desk drag.
test("office-author: new feature + task, dock drag to idle desk opens dispatch", async ({ page }) => {
  // New Feature (shared toolbar) → fill slug id + branch → Add.
  await page.locator('[data-testid="canvas-toolbar"] button', { hasText: "Feature" }).click();
  await page.getByLabel("feature id").fill("f2");
  await page.getByLabel("feature branch").fill("feat-f2");
  await page.getByRole("button", { name: "Add" }).click();

  // New Task → id t9, feature f2.
  await page.locator('[data-testid="canvas-toolbar"] button', { hasText: "Task" }).click();
  const editor = page.locator('[data-testid="task-editor"]');
  await editor.waitFor();
  await editor.getByLabel("task id").fill("t9");
  await editor.getByLabel("feature").selectOption("f2");
  await editor.getByRole("button", { name: "Add draft" }).click();
  await editor.waitFor({ state: "hidden" });

  // The draft appears in the dock.
  const dockChip = page.locator('[data-testid="dock-t9"]');
  await dockChip.waitFor();

  // claude-opus is the idle desk (reviews nothing awaiting_review, owns nothing).
  const idleDesk = page.locator('[data-testid="desk-claude-opus"]');
  await idleDesk.waitFor();

  // HTML5 drag the dock chip onto the idle desk.
  await html5DragTo(page, dockChip, idleDesk);

  // DispatchModal opens with the dropped seat prefilled as owner.
  const modal = page.locator('[data-testid="dispatch-modal"]');
  await modal.waitFor();
  await expect(modal).toContainText("Dispatch · t9");
  // Owner is rendered read-only (prefilled from the drop target) = claude-opus.
  await expect(modal).toContainText("claude-opus");
});
