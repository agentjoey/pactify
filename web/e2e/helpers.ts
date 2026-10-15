import { expect, type Page } from "@playwright/test";

// Shared e2e helpers.

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
