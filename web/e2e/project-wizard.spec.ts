import { test, expect } from "@playwright/test";
import { resetServer, gotoApp } from "./helpers";

test.beforeEach(async ({ page }) => {
  await resetServer(page);
  await gotoApp(page);
});

test("add-project: browse folders, init a new repo, and see it grouped in the sidebar", async ({ page }) => {
  // Open the add-project wizard from the sidebar.
  await page.getByTestId("sidebar-add-project").click();
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
  const group = page.getByTestId("sidebar-group-demo-group");
  await expect(group).toBeVisible();
  await group.click();
  await expect(page.getByTestId("sidebar-project-new")).toBeVisible();
});

test("delete-project: removes a project from the sidebar", async ({ page }) => {
  // p1 is in the seed registry. Hover the row to reveal the delete button.
  const row = page.getByTestId(`sidebar-project-${"p1"}`);
  await row.hover();
  await page.getByTestId("sidebar-delete-p1").click();

  const modal = page.getByTestId("delete-project-modal");
  await modal.waitFor();
  await modal.getByTestId("delete-project-confirm").click();

  await expect(row).toBeHidden();
});
