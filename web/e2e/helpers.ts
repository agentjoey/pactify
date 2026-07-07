import { expect, type Page, type Locator } from "@playwright/test";

// Shared e2e helpers.

export const PROJECT_ID = "p1";

// resetServer restores the mock server's seed state. Call in beforeEach so each
// test starts from the same fixture.
export async function resetServer(page: Page) {
  const r = await page.request.post("/__test/reset");
  expect(r.ok()).toBeTruthy();
}

// gotoApp loads the SPA and waits for the app shell to mount. View-agnostic:
// callers navigate to the lens they need.
export async function gotoApp(page: Page) {
  await page.goto("/");
  await page.locator('[data-testid="app-root"]').waitFor();
}

// centerOf returns the viewport-space center of a locator's bounding box.
export async function centerOf(loc: Locator): Promise<{ x: number; y: number }> {
  const b = await loc.boundingBox();
  if (!b) throw new Error("no bounding box for locator");
  return { x: b.x + b.width / 2, y: b.y + b.height / 2 };
}
