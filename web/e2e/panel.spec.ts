import { test, expect } from "@playwright/test";
test.use({ baseURL: `http://127.0.0.1:${process.env.PANEL_E2E_PORT || 4176}` });
for (const width of [1440, 390])
  test(`panel shares three pages and hides all writes at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    await page.goto("/#/jobs");
    await expect(
      page.getByRole("heading", { name: "Jobs", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: /^(Cancel|Resume|Apply|Discard)$/ }),
    ).toHaveCount(0);
    await page
      .getByRole("region", { name: "In progress", exact: true })
      .getByRole("link")
      .click();
    const modal = page.getByRole("dialog", { name: "Job details" });
    await modal.getByRole("tab", { name: "Logs", exact: true }).click();
    await expect(modal.getByRole("log")).toContainText("message Tests passed.");
    await expect(
      modal.getByRole("button", { name: /^(Cancel|Resume|Apply|Discard)$/ }),
    ).toHaveCount(0);
    if (width === 390) {
      const box = await modal.boundingBox();
      expect(Math.abs(box!.width - 390)).toBeLessThanOrEqual(1);
      expect(Math.abs(box!.height - 844)).toBeLessThanOrEqual(1);
    }
    await page.keyboard.press("Escape");
    await expect(modal).toHaveCount(0);
    for (const route of ["dashboard", "settings"]) {
      await page.goto("/#/" + route);
      if (route === "dashboard")
        await expect(
          page.getByRole("img", { name: "Token usage · last 14 days" }),
        ).toBeVisible();
      else {
        await expect(
          page.getByText("Edit settings from the dashboard or the CLI.", {
            exact: true,
          }),
        ).toBeVisible();
        await expect(page.getByText("@fast", { exact: true })).toBeVisible();
        await expect(
          page.getByRole("button", { name: /Save|Edit|Clear|Refresh models/ }),
        ).toHaveCount(0);
        // Only the mobile page selector remains; harness settings are data.
        await expect(
          page.locator(".settings-content select, .settings-content input"),
        ).toHaveCount(0);
      }
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth - innerWidth,
        ),
      ).toBeLessThanOrEqual(1);
      await test
        .info()
        .attach(`panel-${route}-${width}-final-build`, {
          body: await page.screenshot({ fullPage: true }),
          contentType: "image/png",
        });
    }
  });
test("panel exchanges launch metadata and keeps bearer authentication after refresh", async ({
  page,
}) => {
  await page.route("**/?token=one-time-launch", async (route) => {
    const response = await route.fetch();
    await route.fulfill({
      response,
      body: (await response.text()).replace(
        "</head>",
        '<meta name="pactify-session" content="tab-session"></head>',
      ),
    });
  });
  const authorizedJobs = () =>
    page.waitForRequest(
      (r) =>
        r.url().includes("/api/jobs?all=1") &&
        r.headers().authorization === "Bearer tab-session",
    );
  let jobs = authorizedJobs();
  await page.goto("/?token=one-time-launch");
  await jobs;
  await expect(page).toHaveURL(/\/$/);
  jobs = authorizedJobs();
  await page.reload();
  await jobs;
  await page
    .getByRole("region", { name: "In progress", exact: true })
    .getByRole("link")
    .click();
  const logs = page.waitForRequest(
    (r) =>
      r.url().endsWith("/api/jobs/job-review/logs") &&
      r.headers().authorization === "Bearer tab-session",
  );
  await page.getByRole("tab", { name: "Logs", exact: true }).click();
  await logs;
  await expect(page.getByRole("log")).toContainText("message Tests passed.");
});
test("panel wraps long content and restores background scrolling", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const title = "LongUnbrokenTaskTitle".repeat(6);
  await page.route("**/api/jobs/job-review", async (route) => {
    const response = await route.fetch();
    const detail = await response.json();
    detail.job.title = title;
    detail.job.repo = "/workspace/" + "repository".repeat(18);
    detail.markdown = Array.from(
      { length: 300 },
      (_, i) => `Result line ${i}`,
    ).join("\n");
    await route.fulfill({ json: detail });
  });
  await page.goto("/");
  const overflow = await page.evaluate(() => [
    document.documentElement.style.overflow,
    document.body.style.overflow,
  ]);
  await page
    .getByRole("region", { name: "In progress", exact: true })
    .getByRole("link")
    .click();
  const modal = page.getByRole("dialog", { name: "Job details" });
  await expect(
    modal.getByRole("heading", { name: title, exact: true }),
  ).toBeVisible();
  await expect(modal.getByText(/Result line 299/)).toBeAttached();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth - innerWidth,
    ),
  ).toBeLessThanOrEqual(1);
  await modal.evaluate((node) => {
    node.scrollTop = 500;
  });
  expect(await modal.evaluate((node) => node.scrollTop)).toBeGreaterThan(0);
  await page.keyboard.press("Escape");
  await expect(modal).toHaveCount(0);
  expect(
    await page.evaluate(() => [
      document.documentElement.style.overflow,
      document.body.style.overflow,
    ]),
  ).toEqual(overflow);
});
