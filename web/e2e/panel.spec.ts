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
          page.getByText("pactify serve", {
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
  await page.route("**/api/jobs?all=1", async (route) => {
    const response = await route.fetch();
    const jobs = await response.json();
    jobs.find((job: {id: string}) => job.id === "job-panel").title = title;
    await route.fulfill({json: jobs});
  });
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
  await modal.getByRole("tab", {name:"Summary",exact:true}).click();
  await expect(modal.getByText(/Result line 299/)).toBeAttached();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth - innerWidth,
    ),
  ).toBeLessThanOrEqual(1);
  await modal.locator(".detail-scroll").evaluate((node) => {
    node.scrollTop = 500;
  });
  expect(await modal.locator(".detail-scroll").evaluate((node) => node.scrollTop)).toBeGreaterThan(0);
  await page.keyboard.press("Escape");
  await expect(modal).toHaveCount(0);
  expect(
    await page.evaluate(() => [
      document.documentElement.style.overflow,
      document.body.style.overflow,
    ]),
  ).toEqual(overflow);
});

for (const width of [1440, 390]) {
  test(`panel themes, copy commands and Live details at ${width}px`, async ({page, context}) => {
    await page.setViewportSize({width, height: 844});
    await context.grantPermissions(["clipboard-read", "clipboard-write"]);
    await page.emulateMedia({colorScheme:"light"});
    await page.goto("/#/jobs");
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
    const background = () => page.locator(".v2-app").evaluate(node => getComputedStyle(node).backgroundColor);
    const light = await background();
    await page.emulateMedia({colorScheme:"dark"});
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    expect(await background()).not.toBe(light);
    const attention = page.getByRole("region", {name:"Needs attention",exact:true});
    await expect(attention.getByRole("button", {name:"Copy pactify sub apply job-summary",exact:true})).toBeVisible();
    await attention.getByRole("button", {name:"Copy pactify sub apply job-summary",exact:true}).click();
    await expect(attention.getByRole("status")).toHaveText("Copied");
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe("pactify sub apply job-summary");
    await expect(page.getByRole("dialog")).toHaveCount(0);
    for (const card of await attention.locator("article").all()) {
      expect(await card.getByRole("button", {name:/^Copy pactify sub /}).count()).toBeLessThanOrEqual(2);
      await expect(card.getByText(/Run a command in your terminal|Ctrl\+D/)).toHaveCount(0);
    }
    const summaryCard = attention.locator("article").filter({hasText:"Share job summaries"});
    await expect(summaryCard.getByRole("button", {name:/^Copy pactify sub /})).toHaveCount(2);
    await expect(summaryCard.getByRole("button", {name:"Copy pactify sub discard job-summary",exact:true})).toBeVisible();
    await expect(summaryCard.getByRole("button", {name:"More in details",exact:true})).toHaveCount(0);
    const review = summaryCard.getByRole("button", {name:"Review",exact:true});
    await expect(review).toHaveAttribute("title","More in details");
    await review.click();
    const summaryDetail = page.getByRole("dialog", {name:"Job details"});
    const steps = summaryDetail.getByRole("region", {name:"Terminal commands",exact:true});
    await expect(steps.getByRole("button", {name:/^Copy pactify sub /})).toHaveCount(5);
    await expect(steps.getByText(/Run a command in your terminal/)).toHaveText("Run a command in your terminal. Resume: type instructions, then Ctrl+D.");
    await page.keyboard.press("Escape");
    await expect(summaryDetail).toHaveCount(0);
    await page.getByRole("region", {name:"In progress",exact:true}).getByRole("link").click();
    const modal = page.getByRole("dialog", {name:"Job details"});
    await expect(modal.getByRole("heading", {name:"Build a local job panel",exact:true})).toBeFocused();
    await expect(modal.getByText("Run 2 of 2", {exact:true})).toBeVisible();
    await expect(modal.getByRole("tab", {name:"Live",exact:true})).toHaveAttribute("aria-selected", "true");
    const tools = modal.locator(".live-tools");
    await tools.scrollIntoViewIfNeeded();
    await expect(tools).toBeInViewport();
    const box = await tools.boundingBox();
    const footer = await modal.locator(".detail-footer").boundingBox();
    expect(box!.y + box!.height).toBeLessThanOrEqual(footer!.y);
    const next = modal.getByRole("button", {name:"Copy pactify sub cancel job-review",exact:true});
    await next.scrollIntoViewIfNeeded();
    await expect(next).toBeVisible();
    await expect(page.getByRole("button", {name:/^(Cancel|Resume|Apply|Discard|Dismiss)$/})).toHaveCount(0);
    await page.keyboard.press("Escape");
    await expect(modal).toHaveCount(0);
  });

  test(`panel expired session stops polling at ${width}px`, async ({page}) => {
    await page.setViewportSize({width,height:844});
    await page.clock.install();
    let requests = 0;
    await page.route("**/api/**", route => {
      requests++;
      return route.fulfill({status:401, body:"Session expired"});
    });
    await page.goto("/#/dashboard");
    await expect(page.getByRole("heading",{name:"Session ended",exact:true})).toBeVisible();
    await expect(page.getByRole("button",{name:"Copy pactify sub panel",exact:true})).toBeVisible();
    await expect(page.getByRole("navigation")).toHaveCount(0);
    const count = requests;
    await page.clock.runFor(6000);
    expect(requests).toBe(count);
  });

  test(`panel onboarding, loading and 5xx states at ${width}px`, async ({page}) => {
    await page.setViewportSize({width,height:844});
    let release!: () => void;
    const loading = new Promise<void>(resolve => {release=resolve;});
    await page.route("**/api/jobs?all=1", async route => {
      await loading;
      await route.fulfill({json:[]});
    });
    await page.goto("/#/jobs");
    await expect(page.getByRole("status",{name:"Loading jobs",exact:true})).toBeVisible();
    await expect(page.locator(".loading-skeleton")).toHaveCount(3);
    release();
    await expect(page.getByRole("heading",{name:"Start your first job",exact:true})).toBeVisible();
    await expect(page.getByLabel("Project",{exact:true})).toHaveCount(0);
    await expect(page.getByRole("link",{name:"Read the getting started guide"})).toHaveAttribute("rel","noopener noreferrer");
    await page.unrouteAll({behavior:"wait"});
    let jobsRequests = 0;
    await page.route("**/api/**", route => {
      if (route.request().url().includes("/jobs?all")) jobsRequests++;
      return route.fulfill({status:503,body:"Temporarily unavailable"});
    });
    await page.goto("/#/dashboard");
    await expect(page.getByRole("alert")).toHaveCount(1);
    await expect(page.locator(".kpi strong")).toHaveText(["—","—","—","—","—"]);
    await expect(page.getByText("Unavailable",{exact:true})).toHaveCount(0);
    await expect.poll(() => jobsRequests).toBeGreaterThan(1);
  });
}
