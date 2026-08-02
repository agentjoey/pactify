import { test, expect } from "@playwright/test";
import { resetServer, gotoApp } from "./helpers";

test.beforeEach(async ({ page }) => {
  await resetServer(page);
  await gotoApp(page);
});

test("add-project: browse folders, init a new repo, and see it grouped in the project menu", async ({ page }) => {
  // Open the add-project wizard from the ProjectMenu header dropdown.
  await page.getByTestId("project-menu-trigger").click();
  await page.getByTestId("project-menu").waitFor();
  await page.getByTestId("project-menu-add").click();

  const wizard = page.getByTestId("add-project-wizard");
  await wizard.waitFor();

  // Fill a group name.
  await wizard.getByTestId("wizard-group").fill("demo-group");

  // The FolderPicker boots at /tmp (mock server's default). Select /tmp/new.
  const picker = wizard.getByTestId("folder-picker");
  await picker.getByTestId("folder-entry-new-checkbox").click();

  // Advance to the roster step.
  await wizard.getByRole("button", { name: "Next" }).click();
  await wizard.getByTestId("wizard-step2").waitFor();
  await expect(wizard.getByTestId("wizard-folder-new")).toContainText("new project");

  // Submit: the mock server inits the project and registers it.
  await wizard.getByRole("button", { name: "Submit" }).click();
  await expect(wizard.getByTestId("wizard-results")).toContainText("✓");

  // Close the wizard; the parent refreshes the project list.
  await wizard.getByRole("button", { name: "Done" }).click();
  await expect(wizard).toBeHidden();

  // Verify the new project appears grouped in the ProjectMenu dropdown.
  await page.getByTestId("project-menu-trigger").click();
  const menu = page.getByTestId("project-menu");
  await menu.waitFor();
  // The group header text and the new project row should both be visible.
  await expect(menu.getByText("demo-group")).toBeVisible();
  await expect(menu.getByText("new")).toBeVisible();
});

// De-quarantined 2026-08-03 (was flaky since 2026-07-13): the old version polled
// the UI and re-clicked the trigger on every iteration, which TOGGLES the menu —
// the assertion raced its own re-open. Now it waits for the DELETE response, then
// closes and reopens once from a known state.
test("delete-project: removes a project from the project menu", async ({ page }) => {
  // Register the dialog handler BEFORE triggering the delete so it intercepts
  // the window.confirm that onDeleteProject fires.
  page.on("dialog", (d) => d.accept());

  // The fixture holds exactly one project, and deleting the last one drops the
  // app to its empty state — the menu and its trigger stop existing, so
  // "reopen and check p1 is gone" cannot be asserted. Register a second project
  // so this test exercises what it is named for: removal FROM the menu.
  await page.request.post("/api/registry", { data: { name: "p2", path: "/tmp/p2" } });
  await page.reload();
  await page.locator('[data-testid="app-root"]').waitFor();

  await page.getByTestId("project-menu-trigger").click();
  await page.getByTestId("project-menu").waitFor();

  const deleted = page.waitForResponse(
    (r) => r.url().includes("/api/registry/p1") && r.request().method() === "DELETE",
  );
  await page.getByRole("button", { name: "delete p1" }).click();
  await deleted;

  // Dismiss via the component's own outside-mousedown handler rather than a
  // coordinate guess, so the menu is definitively closed before reopening.
  await page.evaluate(() =>
    document.body.dispatchEvent(new MouseEvent("mousedown", { bubbles: true })),
  );
  await expect(page.getByTestId("project-menu")).toHaveCount(0);

  await page.getByTestId("project-menu-trigger").click();
  await page.getByTestId("project-menu").waitFor();
  await expect(page.getByRole("button", { name: "delete p1" })).toHaveCount(0);
});
