import { test, expect } from "@playwright/test";

test.use({ baseURL: `http://127.0.0.1:${process.env.PANEL_E2E_PORT || 4176}` });

test("panel list → detail → live readable logs and resume chain", async ({
  page,
}) => {
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Delegated jobs", exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("Needs attention").first()).toContainText(
    "Worktree",
  );
  await page.getByLabel("Harness", { exact: true }).selectOption("codex");
  await expect(
    page.getByRole("link", { name: "Evaluate resume for large tasks" }),
  ).toHaveCount(0);
  await page
    .getByRole("link", { name: "AJ-659 · Local read-only job panel" })
    .click();
    await page.getByRole("navigation", { name: "Run timeline" }).getByRole("link", { name: /^job-panel ·/ }).click();
  await expect(page.getByRole("heading", { name: "Task brief" })).toBeVisible();
  await expect(page.getByRole("log")).toContainText(
    "tool exec go test ./internal/subpanel/...",
  );
  await expect(page.getByText("Finished", { exact: true })).toBeVisible();
  await expect(page.getByRole("log")).toContainText("message Tests passed.");
  await page
    .getByText("a/internal/subpanel/server.go b/internal/subpanel/server.go", {
      exact: true,
    })
    .click();
  await expect(page.getByText("+func Listen(port int)")).toBeVisible();
  await expect(
    page.getByText("pactify sub apply 'job-panel'", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("navigation", { name: "Run timeline" })
    .getByRole("link", { name: /^job-review ·/ })
    .click();
  await expect(
    page.getByRole("heading", {
      name: "Panel acceptance and visual checks",
      exact: true,
    }),
  ).toBeVisible();
});

for (const theme of ["light", "dark"]) {
  test(`panel ${theme} mobile layout has no horizontal overflow`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 375, height: 812 });
    await page.goto("/");
    await page.getByLabel("Theme", { exact: true }).selectOption(theme);
    await expect(
      page.getByRole("link", { name: "AJ-659 · Local read-only job panel" }),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await page
      .getByRole("link", { name: "AJ-659 · Local read-only job panel" })
      .click();
    await page.getByRole("navigation", { name: "Run timeline" }).getByRole("link", { name: /^job-panel ·/ }).click();
    await expect(page.getByRole("log")).toContainText("message Tests passed.");
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
  });
}

test("panel mobile wraps long titles and repository paths", async ({
  page,
}) => {
  await page.setViewportSize({ width: 375, height: 812 });
  const title = "LongUnbrokenTaskTitle".repeat(6);
  await page.route("**/api/jobs/job-review", async (route) => {
    const response = await route.fetch();
    const detail = await response.json();
    detail.job.title = title;
    detail.job.repo = "/workspace/" + "repository".repeat(18);
    await route.fulfill({ json: detail });
  });
  await page.goto("/#/jobs/job-review");
  await expect(
    page.getByRole("heading", { name: title, exact: true }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});

test("panel uses the shared merged board and a full-screen drawer at 390px", async ({ page }) => {
  await page.setViewportSize({width: 390, height: 844});
  await page.goto("/");
  const running = page.getByRole("region", {name: "RUNNING", exact: true});
  await expect(running.locator("article")).toHaveCount(1);
  await expect(running.locator("article")).toContainText("2 runs");
  await running.getByRole("link").click();
  const drawer = page.getByRole("dialog", {name: "Job details"});
  await expect(drawer.getByRole("heading", {name: "Run timeline"})).toBeVisible();
  const box = await drawer.boundingBox();
  expect(box?.width).toBe(390);
  expect(box?.height).toBe(844);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await drawer.getByRole("link", {name: "Close job details"}).click();
  await expect(page).toHaveURL(/#\/jobs$/);
  await expect(drawer).toHaveCount(0);
});

test("panel exchanges launch metadata and keeps bearer authentication after refresh", async ({ page }) => {
  await page.route("**/?token=one-time-launch", async (route) => {
    const response = await route.fetch();
    const html = (await response.text()).replace(
      "</head>",
      '<meta name="pactify-session" content="tab-session"></head>',
    );
    await route.fulfill({ response, body: html });
  });
  const authorizedJobs = () => page.waitForRequest((request) =>
    request.url().includes("/api/jobs?all=1") &&
    request.headers().authorization === "Bearer tab-session",
  );
  let jobs = authorizedJobs();
  await page.goto("/?token=one-time-launch");
  await jobs;
  await expect(page).toHaveURL(/\/$/);
  jobs = authorizedJobs();
  await page.reload();
  await jobs;
  const logs = page.waitForRequest((request) =>
    request.url().endsWith("/api/jobs/job-panel/logs") &&
    request.headers().authorization === "Bearer tab-session",
  );
  await page.getByRole("link", { name: "AJ-659 · Local read-only job panel" }).click();
  await page.getByRole("navigation", { name: "Run timeline" }).getByRole("link", { name: /^job-panel ·/ }).click();
  await logs;
  await expect(page.getByRole("log")).toContainText("message Tests passed.");
});
