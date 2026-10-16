import { test, expect } from "@playwright/test";

for (const width of [1440, 375]) {
  test(`Routing edits and saves configuration at ${width}px from the final build`, async ({
    page,
    request,
  }) => {
    await request.post("/__test/reset");
    await page.setViewportSize({ width, height: 1000 });
    await page.goto("/#/jobs");
    await page.getByText("Routing", { exact: true }).click();
    await expect(page.getByText("@fast", { exact: true })).toBeVisible();
    const noOverflow = () =>
      page.evaluate(() => document.documentElement.scrollWidth <= innerWidth);
    expect(await noOverflow()).toBe(true);
    await page.getByRole("button", { name: "Edit", exact: true }).click();
    await page.getByRole("button", { name: "Add alias", exact: true }).click();
    await page.getByLabel("Alias 2 name", { exact: true }).fill("mobile-test");
    await page
      .getByLabel("Alias 2 harness", { exact: true })
      .selectOption("opencode");
    await page
      .getByLabel("Alias 2 model", { exact: true })
      .fill("model/" + "long-name".repeat(15));
    await page.getByLabel("Alias 2 effort", { exact: true }).fill("low");
    await page
      .getByRole("button", { name: "Add route to codex", exact: true })
      .click();
    await page
      .getByLabel("codex route 3", { exact: true })
      .selectOption("@mobile-test");
    await page
      .getByRole("button", { name: "Move codex route 3 up", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Add quota group", exact: true })
      .click();
    await page
      .getByLabel("Quota group 2 name", { exact: true })
      .fill("shared-test");
    await page
      .getByLabel("Quota group 2 harness", { exact: true })
      .selectOption("opencode");
    await page
      .getByLabel("Quota group 2 model 1", { exact: true })
      .fill("model-one");
    await page
      .getByRole("button", { name: "Add model to quota group 2", exact: true })
      .click();
    await page
      .getByLabel("Quota group 2 model 2", { exact: true })
      .fill("model-two");
    expect(await noOverflow()).toBe(true);
    await test
      .info()
      .attach(`routing-edit-${width}px-final-build`, {
        body: await page.screenshot({ fullPage: true }),
        contentType: "image/png",
      });
    const saved = page.waitForResponse(
      (r) =>
        r.url().endsWith("/api/sub/router") && r.request().method() === "PUT",
    );
    await page.getByRole("button", { name: "Save", exact: true }).click();
    expect((await saved).status()).toBe(200);
    await expect(page.getByText("@mobile-test", { exact: true })).toBeVisible();
    await expect(
      page.getByText("@fast → @mobile-test → kimi", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("model-one, model-two", { exact: true }),
    ).toBeVisible();
    await page.reload();
    await page.getByText("Routing", { exact: true }).click();
    await expect(page.getByText("@mobile-test", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Clear", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    await page.getByRole("button", { name: "Clear", exact: true }).click();
    await dialog.getByRole("button", { name: "Clear", exact: true }).click();
    await expect(
      page.getByText("No exhausted routes.", { exact: true }),
    ).toBeVisible();
    expect(await noOverflow()).toBe(true);
    await test
      .info()
      .attach(`routing-saved-${width}px-final-build`, {
        body: await page.screenshot({ fullPage: true }),
        contentType: "image/png",
      });
  });
}

test("panel Routing is read-only", async ({ page }) => {
  await page.goto(`http://127.0.0.1:${process.env.PANEL_E2E_PORT || 4176}`);
  await page.getByText("Routing", { exact: true }).click();
  await expect(page.getByText("@fast", { exact: true })).toBeVisible();
  await expect(
    page.getByText(
      "Edit routing from the dashboard or with pactify sub router.",
      { exact: true },
    ),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Edit", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Clear", exact: true }),
  ).toHaveCount(0);
});
