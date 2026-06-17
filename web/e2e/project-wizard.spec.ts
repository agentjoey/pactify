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

test("delete-project: removes a project from the project menu", async ({ page }) => {
  // Register the dialog handler BEFORE triggering the delete so it intercepts
  // the window.confirm that onDeleteProject fires.
  page.on("dialog", (d) => d.accept());

  // Open the ProjectMenu and click the delete button for p1.
  await page.getByTestId("project-menu-trigger").click();
  await page.getByTestId("project-menu").waitFor();
  await page.getByRole("button", { name: "delete p1" }).click();

  // After the confirm dialog auto-accepts and the registry DELETE resolves,
  // the project list refreshes. Re-open the menu and assert p1 is gone.
  await expect
    .poll(async () => {
      // Re-open the dropdown on each poll iteration if it closed.
      const isOpen = await page.getByTestId("project-menu").isVisible().catch(() => false);
      if (!isOpen) await page.getByTestId("project-menu-trigger").click();
      return page.getByRole("button", { name: "delete p1" }).isVisible().catch(() => false);
    }, { timeout: 5000 })
    .toBe(false);
});
