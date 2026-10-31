import { test, expect, type Page } from "@playwright/test";
const noOverflow = async (page: Page) =>
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth - innerWidth,
    ),
  ).toBeLessThanOrEqual(1);
async function navigate(page: Page, label: string, _width: number) {
  await page.getByRole("navigation", { name: "Main navigation" })
    .getByRole("link", { name: label, exact: true }).click();
  await expect(
    page.getByRole("heading", { name: label, exact: true }),
  ).toBeVisible();
}
test.beforeEach(async ({ request }) => {
  await request.post("/__test/reset");
});
for (const width of [1440, 390]) {
  test(`three pages, filters, stacks, detail tabs and final-build screenshots at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/");
    await expect(
      page.getByRole("heading", { name: "Jobs", exact: true }),
    ).toBeVisible();
    const running = page.getByRole("region", {
      name: "In progress",
      exact: true,
    });
    if (width < 640) await page.getByRole("button", {name:"Filters", exact:true}).click();
    await expect(running.locator("article")).toHaveCount(1);
    await expect(running).toContainText("1 rework");
    const queuedCard = running.locator("article").first();
    const titleBox = await queuedCard.locator("h3").boundingBox();
    const metadataBox = await queuedCard
      .locator(".panel-metadata")
      .boundingBox();
    expect(titleBox!.height).toBeLessThan(30);
    expect(metadataBox!.y - titleBox!.y - titleBox!.height).toBeLessThanOrEqual(
      14,
    );
    const completed = page.getByRole("region", {
      name: "Completed",
      exact: true,
    });
    const today = completed.locator(".completed-day").first();
    await expect(today.getByRole("button", { name: "Show all" })).toBeVisible();
    await today.getByRole("button", { name: "Show all" }).click();
    await expect(today.locator("article")).toHaveCount(6);
    await today.getByRole("button", { name: "Collapse" }).click();
    await expect(today.locator(".job-stack")).toBeVisible();
    await page
      .getByLabel("Project", { exact: true })
      .selectOption("/workspace/harness-eval");
    await expect(page).toHaveURL(/repo=/);
    await page.reload();
    await expect(page.getByLabel("Project", { exact: true })).toHaveValue(
      "/workspace/harness-eval",
    );
    await expect(
      page.getByRole("region", { name: "In progress", exact: true }),
    ).toHaveCount(0);
    await page.getByLabel("Project", { exact: true }).selectOption("");
    await page.getByLabel("Harness", { exact: true }).selectOption("codex");
    await page.getByLabel("Status", { exact: true }).selectOption("progress");
    await page.getByPlaceholder("Search jobs").fill("local job panel");
    await expect(page.locator("article")).toHaveCount(1);
    await running
      .getByRole("link", { name: "Build a local job panel" })
      .click();
    const modal = page.getByRole("dialog", { name: "Job details" });
    await expect(modal.getByRole("tab", { name: "Live", exact: true })).toHaveAttribute("aria-selected", "true");
    await modal.getByRole("tab", { name: "Summary", exact: true }).click();
    await expect(modal.getByRole("heading", { name: "Result summary" })).toBeVisible();
    await modal
      .getByRole("navigation", { name: "Run timeline" })
      .getByRole("link", { name: /^job-panel ·/ })
      .click();
    await expect(
      modal.getByRole("heading", {
        name: "Build a local job panel",
        exact: true,
      }),
    ).toBeVisible();
    await modal.getByRole("tab", { name: "Logs", exact: true }).click();
    await expect(modal.getByRole("log")).toContainText("message Tests passed.");
    await modal.getByRole("tab", { name: "Diff", exact: true }).click();
    await expect(modal.locator("pre")).toContainText("+func Listen(port int)");
    await modal.getByRole("tab", { name: "Brief", exact: true }).click();
    await expect(
      modal.getByRole("heading", { name: "Task brief" }),
    ).toBeVisible();
    const box = await modal.boundingBox();
    if (width === 390) {
      expect(Math.abs(box!.width - width)).toBeLessThanOrEqual(1);
      expect(Math.abs(box!.height - 900)).toBeLessThanOrEqual(1);
    } else expect(Math.abs(box!.width - 960)).toBeLessThanOrEqual(1);
    await noOverflow(page);
    await test.info().attach(`jobs-detail-${width}-final-build`, {
      body: await page.screenshot({ fullPage: true }),
      contentType: "image/png",
    });
    await page.keyboard.press("Escape");
    await expect(modal).toHaveCount(0);
    await expect(page.getByPlaceholder("Search jobs")).toHaveValue("local job panel");
    await navigate(page, "Dashboard", width);
    await expect(
      page.getByRole("img", { name: "Token usage · last 14 days" }),
    ).toBeVisible();
    await expect(page.getByRole("row", { name: /opencode/ })).toContainText(
      "$0.13",
    );
    await expect(
      page.getByRole("progressbar", { name: "codex 5h remaining" }),
    ).toHaveAttribute("value", "80");
    await expect(
      page.getByRole("progressbar", { name: "codex 7d remaining" }),
    ).toHaveAttribute("value", "65");
    await expect(page.locator(".chart-gridline")).toHaveCount(4);
    await expect(page.locator(".token-plot svg text")).toHaveCount(0);
    const labels = page.locator(".chart-label:visible");
    for (const label of await labels.all())
      expect(
        await label.evaluate((node) =>
          parseFloat(getComputedStyle(node).fontSize),
        ),
      ).toBeGreaterThanOrEqual(11);
    await expect(
      page.locator(".chart-x-axis .chart-label:visible"),
    ).toHaveCount(width < 700 ? 5 : 14);
    if (width < 700) {
      await expect(
        page.getByText("Scroll horizontally to see tokens and cost →"),
      ).toBeVisible();
      const scroll = page.getByRole("region", { name: "By harness usage" });
      expect(
        await scroll.evaluate((node) => node.scrollWidth - node.clientWidth),
      ).toBeGreaterThan(0);
      await scroll.evaluate((node) => {
        node.scrollLeft = node.scrollWidth;
      });
      const tableBox = await scroll.boundingBox();
      const costBox = await scroll
        .getByRole("cell", { name: "$0.13", exact: true })
        .boundingBox();
      expect(costBox!.x).toBeGreaterThanOrEqual(tableBox!.x - 1);
      expect(costBox!.x + costBox!.width).toBeLessThanOrEqual(
        tableBox!.x + tableBox!.width + 1,
      );
    }
    await noOverflow(page);
    await test.info().attach(`dashboard-${width}-final-build`, {
      body: await page.screenshot({ fullPage: true }),
      contentType: "image/png",
    });
    await navigate(page, "Settings", width);
    for (const name of ["codex", "kimi", "agy", "opencode"]) {
      const card = page.getByRole("article", { name, exact: true });
      await expect(card.getByText("experimental", { exact: true })).toHaveCount(
        name === "agy" ? 1 : 0,
      );
      await expect(card).toContainText(
        `Read-only: ${name === "kimi" ? "Not supported" : "Supported"}`,
      );
    }
    await expect(page.getByLabel("codex Default model")).toHaveValue(
      "gpt-6.1-sol",
    );
    await page.getByLabel("codex Default model").selectOption("gpt-6-sol");
    const saved = page.waitForResponse(
      (r) =>
        r.url().endsWith("/api/sub/router") && r.request().method() === "PUT",
    );
    await page
      .getByRole("button", { name: "Save defaults", exact: true })
      .click();
    expect((await saved).status()).toBe(200);
    await expect(
      page.getByText("Defaults saved", { exact: true }),
    ).toBeVisible();
    await page.reload();
    await expect(page.getByLabel("codex Default model")).toHaveValue(
      "gpt-6-sol",
    );
    await expect(page.getByText("@fast", { exact: true })).toBeVisible();
    const edit = page
      .locator(".routing-heading")
      .getByRole("button", { name: "Edit", exact: true });
    await expect(edit).toBeVisible();
    await expect(
      page
        .locator(".routing-heading")
        .getByRole("heading", { name: "Routing", exact: true }),
    ).toBeVisible();
    await expect(page.locator(".routing-heading .routing-path")).toBeVisible();
    const editBox = await edit.boundingBox();
    const saveBox = await page
      .getByRole("button", { name: "Save defaults", exact: true })
      .boundingBox();
    expect(
      Math.abs(editBox!.x + editBox!.width - saveBox!.x - saveBox!.width),
    ).toBeLessThanOrEqual(1);
    await noOverflow(page);
    await test.info().attach(`settings-${width}-final-build`, {
      body: await page.screenshot({ fullPage: true }),
      contentType: "image/png",
    });
  });
}
test("Resume creates the next run and refreshes the merged chain", async ({
  page,
}) => {
  await page.goto("/#/jobs");
  const card = page
    .getByRole("region", { name: "Needs attention", exact: true })
    .locator("article")
    .filter({ hasText: "Fix CLI exit code edge cases" });
  await card.getByRole("button", { name: "Resume", exact: true }).click();
  const dialog = page.getByRole("dialog", {
    name: "Resume Fix CLI exit code edge cases",
  });
  await dialog
    .getByLabel("Instructions for the next run")
    .fill("Fix the remaining checks\nRun all tests");
  await dialog.getByRole("button", { name: "Start", exact: true }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "Started job-resumed-1" }),
  ).toBeVisible();
  const running = page.getByRole("region", { name: "In progress", exact: true });
  await expect(
    running
      .locator("article")
      .filter({ hasText: "Fix CLI exit code edge cases" }),
  ).toContainText("1 rework");
});
test("Cancel needs an inline confirmation and can be backed out", async ({
  page,
  request,
}) => {
  await page.goto("/#/jobs");
  const running = page.getByRole("region", {
    name: "In progress",
    exact: true,
  });
  await running.getByRole("button", { name: "Cancel", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toContainText("stop the running job");
  await dialog.getByRole("button", { name: "Back", exact: true }).click();
  expect(
    (await (await request.get("/api/sub/jobs?all=1")).json()).find(
      (j: { id: string }) => j.id === "job-review",
    ).status,
  ).toBe("running");
  await running.getByRole("button", { name: "Cancel", exact: true }).click();
  await dialog
    .getByRole("button", { name: "Confirm Cancel", exact: true })
    .click();
  await expect(dialog).toContainText(
    "Stop this job now? The harness stops immediately and the run cannot be continued as it is.",
  );
  expect(
    (await (await request.get("/api/sub/jobs?all=1")).json()).find(
      (j: { id: string }) => j.id === "job-review",
    ).status,
  ).toBe("running");
  await dialog.getByRole("button", { name: "Stop job", exact: true }).click();
  await expect(running).toHaveCount(0);
  expect(
    (await (await request.get("/api/sub/jobs?all=1")).json()).find(
      (j: { id: string }) => j.id === "job-review",
    ).status,
  ).toBe("canceled");
});
test("a finished job with a pending worktree waits In review and leaves apply and discard to the delegating session", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/#/jobs");
  await page.getByRole("button", { name: "List", exact: true }).click();
  await expect(
    page.getByRole("region", { name: "Needs attention", exact: true })
      .locator("article").filter({ hasText: "Share job summaries" }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("region", { name: "In review", exact: true })
      .locator("article").filter({ hasText: "Share job summaries" }),
  ).toHaveCount(1);
  const sizes = await page.locator("article.task-card").evaluateAll((cards) =>
    cards.flatMap((card) => [card, ...card.querySelectorAll("*")])
      .filter((node) => [...node.childNodes].some((child) => child.nodeType === 3 && child.textContent!.trim()))
      .map((node) => parseFloat(getComputedStyle(node).fontSize)),
  );
  expect(sizes.length).toBeGreaterThan(0);
  expect(Math.min(...sizes)).toBeGreaterThanOrEqual(11);
  await page.getByRole("button", { name: "Lines", exact: true }).click();
  const lineSizes = await page.locator("article.task-card").evaluateAll((cards) =>
    cards.flatMap((card) => [card, ...card.querySelectorAll("*")])
      .filter((node) => [...node.childNodes].some((child) => child.nodeType === 3 && child.textContent!.trim()))
      .map((node) => parseFloat(getComputedStyle(node).fontSize)),
  );
  expect(Math.min(...lineSizes)).toBeGreaterThanOrEqual(11);
  await page.goto("/#/jobs/job-summary?tab=diff");
  const detail = page.getByRole("dialog", { name: "Job details" });
  await expect(detail.locator("pre")).toContainText("+func Listen(port int)");
  await expect(detail.getByRole("button", { name: /^(Apply|Discard)$/ })).toHaveCount(0);
  await expect(detail.getByRole("button", { name: "Resume", exact: true })).toBeVisible();
});
test("Dashboard cost failure does not hide KPIs or chart", async ({ page }) => {
  await page.route("**/api/sub/cost?*", (route) =>
    route.fulfill({ status: 503, body: "offline" }),
  );
  await page.goto("/#/dashboard");
  await expect(page.getByRole("alert")).toContainText("503");
  await expect(page.getByText("Unavailable", { exact: true })).toHaveCount(0);
  await expect(page.getByLabel("In progress", { exact: true })).toContainText(
    "1",
  );
  await expect(
    page.getByRole("img", { name: "Token usage · last 14 days" }),
  ).toBeVisible();
});

// Keep the established list interactions under test alongside pipeline.spec.ts.
test.beforeEach(async ({ context }) => {
  await context.addInitScript(() => localStorage.setItem("pactify-jobs-view", "list"));
});
