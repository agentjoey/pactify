import { test, expect } from "@playwright/test";

async function openJobs(page: import("@playwright/test").Page) {
  await page.addInitScript(() => localStorage.setItem("pactify:lens", "jobs"));
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Delegated jobs", exact: true })).toBeVisible();
}

for (const width of [1440, 390]) {
  test(`Jobs board merges chains, opens a drawer and switches runs at ${width}px`, async ({ page }) => {
    await page.setViewportSize({width, height: 900});
    await openJobs(page);
    const running = page.getByRole("region", {name: "RUNNING", exact: true});
    const attention = page.getByRole("region", {name: "NEEDS ATTENTION", exact: true});
    const succeeded = page.getByRole("region", {name: "SUCCEEDED", exact: true});
    const failed = page.getByRole("region", {name: "FAILED / CANCELED", exact: true});
    await expect(running.locator("article")).toHaveCount(1);
    await expect(running.locator("article")).toContainText("2 runs");
    await expect(running.locator("article")).toContainText("rework 1");
    await expect(attention.locator("article")).toHaveCount(2);
    await expect(failed.locator("article")).toContainText("Canceled experiment");
    const fallback = succeeded.locator("article").filter({hasText: "Recover a quota-limited task"});
    await expect(fallback).toContainText("fallback → opencode");
    await expect(fallback).toContainText("2 runs");
    await expect(page.getByRole("link", {name: "Fallback attempt", exact: true})).toHaveCount(0);
    await running.getByRole("link", {name: "AJ-659 · Local read-only job panel"}).click();
    const drawer = page.getByRole("dialog", {name: "Job details"});
    await expect(drawer).toBeVisible();
    await expect(page).toHaveURL(/#\/jobs\/job-review$/);
    await drawer.getByRole("navigation", {name: "Run timeline"}).getByRole("link", {name: /^job-panel ·/}).click();
    await expect(drawer.getByRole("heading", {name: "AJ-659 · Local read-only job panel", exact: true})).toBeVisible();
    await expect(drawer.getByRole("log")).toContainText("message Tests passed.");
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    if (width === 390) {
      const box = await drawer.boundingBox();
      expect(box?.width).toBe(390);
    }
    await test.info().attach(`jobs-board-drawer-${width}px-final-build`, {body: await page.screenshot({fullPage: true}), contentType: "image/png"});
    await drawer.getByRole("link", {name: "Close job details"}).click();
    await expect(drawer).toHaveCount(0);
    await expect(page).toHaveURL(/#\/jobs$/);
    await fallback.getByRole("link", {name: "Recover a quota-limited task"}).click();
    await expect(page).toHaveURL(/#\/jobs\/job-fallback$/);
    await expect(drawer.getByRole("navigation", {name: "Run timeline"}).getByRole("link")).toHaveCount(2);
    await page.keyboard.press("Escape");
    await expect(drawer).toHaveCount(0);
    await test.info().attach(`jobs-board-${width}px-final-build`, {body: await page.screenshot({fullPage: true}), contentType: "image/png"});
  });
}

for (const width of [1440, 375]) {
  test(`Jobs usage shows totals, refreshes slowly and stays within the page at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.clock.install();
    const requests: string[] = [];
    page.on("request", request => {
      if (new URL(request.url()).pathname === "/api/sub/cost") requests.push(request.url());
    });
    await openJobs(page);
    const card = page.getByRole("region", { name: "Usage · last 7 days", exact: true });
    await expect(card.getByRole("row", { name: /codex/ })).toContainText("subscription");
    await expect(card.getByRole("row", { name: /codex/ })).toContainText("12,345");
    await expect(card.getByRole("row", { name: /opencode/ })).toContainText("$0.125000");
    await expect(card.getByRole("row", { name: /custom/ })).toContainText("unknown");
    await expect(card.getByRole("row", { name: /Total/ })).toContainText("4 (2 ok / 2 failed)");
    await expect(card).toContainText("* includes 1 jobs with partial usage");
    await expect(card).toContainText(/Exhausted: codex\/gpt-5 \(resets in \d+h \d+m\)/);
    expect(requests).toHaveLength(1);
    expect(new URL(requests[0]).searchParams.toString()).toBe("since=7d&by=harness");
    await page.clock.fastForward(10000);
    expect(requests).toHaveLength(1);
    const refreshed = page.waitForResponse(response => new URL(response.url()).pathname === "/api/sub/cost");
    await page.clock.fastForward(21000);
    await refreshed;
    expect(requests).toHaveLength(2);
    await card.scrollIntoViewIfNeeded();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    const scroll = card.getByRole("region", { name: "Usage by harness" });
    if (width < 640) {
      expect(await scroll.evaluate(node => node.scrollWidth > node.clientWidth)).toBe(true);
      await scroll.evaluate(node => { node.scrollLeft = node.scrollWidth; });
      expect(await scroll.evaluate(node => node.scrollLeft)).toBeGreaterThan(0);
    }
    await test.info().attach(`jobs-usage-${width}px-final-build`, { body: await card.screenshot(), contentType: "image/png" });
  });
}

test("Jobs list remains available when usage fails", async ({ page }) => {
  await page.route("**/api/sub/cost?*", route => route.fulfill({ status: 503, body: "unavailable" }));
  await openJobs(page);
  await expect(page.getByText("Usage unavailable", { exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "AJ-659 · Local read-only job panel" })).toBeVisible();
});

test("dashboard Jobs lens persists and shows all repositories, filters and details", async ({ page }) => {
  await page.goto("/");
  await page.getByTestId("lens-jobs").click();
  await expect(page.getByTestId("lens-jobs")).toHaveAttribute("aria-pressed", "true");
  await expect(page.locator("article").first()).toContainText("AJ-659 · Local read-only job panel");
  await expect(page.getByLabel("Needs attention").first()).toContainText("Worktree");
  await page.reload();
  await expect(page.getByTestId("lens-jobs")).toHaveAttribute("aria-pressed", "true");
  await page.getByLabel("Harness", { exact: true }).selectOption("codex");
  await expect(page.getByRole("link", { name: "Evaluate resume for large tasks" })).toHaveCount(0);
  await page.getByRole("link", { name: "AJ-659 · Local read-only job panel" }).click();
  await page.getByRole("navigation", { name: "Run timeline" }).getByRole("link", { name: /^job-panel ·/ }).click();
  await expect(page.getByRole("heading", { name: "Result summary" })).toBeVisible();
  await expect(page.getByRole("log")).toContainText("message Tests passed.");
  await expect(page.getByText("Finished", { exact: true })).toBeVisible();
  await page.locator("summary").filter({ hasText: "a/internal/subpanel/server.go" }).click();
  await expect(page.getByText("+func Listen(port int)")).toBeVisible();
  await page.getByRole("navigation", { name: "Run timeline" }).getByRole("link", { name: /^job-review ·/ }).click();
  await expect(page.getByRole("heading", { name: "Panel acceptance and visual checks", exact: true })).toBeVisible();
  await page.getByRole("link", { name: "Close job details" }).click();
  await expect(page.getByRole("heading", { name: "Delegated jobs", exact: true })).toBeVisible();
});

for (const theme of ["light", "dark"]) {
  test(`Jobs with ${theme} system preference wraps mobile content and follows logs`, async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 812 });
    await page.addInitScript(() => localStorage.setItem("pactify:lens", "dashboard"));
    await page.goto("/");
    await page.getByLabel("View", { exact: true }).selectOption("jobs");
    await page.emulateMedia({ colorScheme: theme as "light" | "dark" });
    await expect(page.getByRole("link", { name: "AJ-659 · Local read-only job panel" })).toBeVisible();
    const noOverflow = () => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth);
    expect(await noOverflow()).toBe(true);
    await page.evaluate(() => document.fonts.ready);
    await test.info().attach(`jobs-list-${theme}-mobile`, { body: await page.screenshot({ fullPage: true }), contentType: "image/png" });
    const title = "LongUnbrokenTitle".repeat(10);
    await page.route("**/api/sub/jobs/job-panel", async (route) => {
      const response = await route.fetch();
      const detail = await response.json();
      detail.job.title = title;
      detail.job.repo = "/workspace/" + "repository".repeat(30);
      await route.fulfill({ json: detail });
    });
    await page.route("**/api/sub/jobs/job-panel/logs", (route) => route.fulfill({
      contentType: "text/event-stream",
      body: Array.from({ length: 1100 }, (_, i) => `data: "message ${i}"\n\n`).join("") + "event: done\ndata: {}\n\n",
    }));
    await page.getByRole("link", { name: "AJ-659 · Local read-only job panel" }).click();
    await page.getByRole("navigation", { name: "Run timeline" }).getByRole("link", { name: /^job-panel ·/ }).click();
    await expect(page.getByRole("heading", { name: title, exact: true })).toBeVisible();
    await expect(page.getByRole("log")).toContainText("message 1099");
    expect(await page.getByRole("log").evaluate((node) => Math.abs(node.scrollHeight - node.clientHeight - node.scrollTop) <= 1)).toBe(true);
    expect(await noOverflow()).toBe(true);
    await test.info().attach(`jobs-detail-${theme}-mobile`, { body: await page.screenshot({ fullPage: true }), contentType: "image/png" });
  });
}

test("Jobs works without registered pact projects and handles empty/error states", async ({ page }) => {
  await page.route("**/api/projects", (route) => route.fulfill({ json: [] }));
  await page.route("**/api/sub/jobs?all=1", (route) => route.fulfill({ json: [] }));
  await openJobs(page);
  await expect(page.getByText("No running jobs")).toBeVisible();
  await page.unroute("**/api/sub/jobs?all=1");
  await page.route("**/api/sub/jobs?all=1", (route) => route.fulfill({ status: 503, body: "unavailable" }));
  await expect(page.getByRole("alert")).toContainText("503");
  await page.goto("/#/jobs/missing");
  await expect(page.getByRole("alert")).toContainText("404");
});

for (const theme of ["light", "dark"]) {
  test(`Jobs desktop with ${theme} system preference from embedded build`, async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 1000 });
    await openJobs(page);
    await page.emulateMedia({ colorScheme: theme as "light" | "dark" });
    await page.getByRole("link", { name: "AJ-659 · Local read-only job panel" }).waitFor();
    await page.evaluate(() => document.fonts.ready);
    await test.info().attach(`jobs-list-${theme}-desktop`, { body: await page.screenshot({ fullPage: true }), contentType: "image/png" });
    await page.getByRole("link", { name: "AJ-659 · Local read-only job panel" }).click();
    await page.getByRole("navigation", { name: "Run timeline" }).getByRole("link", { name: /^job-panel ·/ }).click();
    await expect(page.getByText("Finished", { exact: true })).toBeVisible();
    await test.info().attach(`jobs-detail-${theme}-desktop`, { body: await page.screenshot({ fullPage: true }), contentType: "image/png" });
  });
}

for (const width of [1440, 390]) {
  test(`Jobs hash links load directly and follow hashchange at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.addInitScript(() => {
      localStorage.setItem("pactify:lens", "board");
      localStorage.setItem("pactify-jobs-theme", "light");
    });
    await page.emulateMedia({ colorScheme: "light" });
    const noOverflow = () => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth);
    const assertJobsChrome = async () => {
      await expect(page.getByLabel("Theme", { exact: true })).toHaveCount(0);
      await expect(page.getByTestId("view-jobs").locator("header")).toHaveCount(0);
      expect(await page.evaluate(() => getComputedStyle(document.body).backgroundColor)).toBe("rgb(10, 14, 20)");
      expect(await page.evaluate(() => getComputedStyle(document.documentElement).colorScheme)).toBe("dark");
      expect(await noOverflow()).toBe(true);
    };
    await page.goto("/#/jobs");
    await expect(page.getByRole("heading", { name: "Delegated jobs", exact: true })).toBeVisible();
    await assertJobsChrome();
    await page.goto("/#/jobs/job-panel");
    await expect(page.getByRole("dialog").getByRole("heading", { name: "AJ-659 · Local read-only job panel", exact: true })).toBeVisible();
    await page.reload();
    await expect(page.getByRole("log")).toContainText("message Tests passed.");
    await assertJobsChrome();
    if (width < 640) await page.getByLabel("View", { exact: true }).selectOption("board");
    else await page.getByTestId("lens-board").click();
    await expect(page.getByTestId("view-jobs")).toHaveCount(0);
    expect(await page.evaluate(() => location.hash)).toBe("");
    expect(await page.evaluate(() => getComputedStyle(document.body).backgroundColor)).toBe("rgb(10, 14, 20)");
    await page.evaluate(() => { location.hash = "/jobs/job-review"; });
    await expect(page.getByRole("heading", { name: "Panel acceptance and visual checks", exact: true })).toBeVisible();
    await assertJobsChrome();
    await page.evaluate(() => { location.hash = "/jobs/job-panel"; });
    await expect(page.getByRole("dialog").getByRole("heading", { name: "AJ-659 · Local read-only job panel", exact: true })).toBeVisible();
    await page.getByRole("link", { name: "Close job details" }).click();
    await expect(page).toHaveURL(/#\/jobs$/);
    await expect(page.getByRole("heading", { name: "Delegated jobs", exact: true })).toBeVisible();
    await assertJobsChrome();
    await test.info().attach(`jobs-deep-links-${width}px`, { body: await page.screenshot({ fullPage: true }), contentType: "image/png" });
  });
}
