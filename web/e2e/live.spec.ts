import { expect, test } from "@playwright/test";
import { seedJobs } from "./jobs-fixtures.mjs";

test.beforeEach(async ({ request }) => {
  await request.post("/__test/reset");
});
test("Dashboard Live uses real event fields, routes, rework links and closes streams", async ({
  page,
}) => {
  let opened = 0,
    closed = 0;
  page.on("request", (r) => {
    if (r.url().endsWith("/events")) opened++;
  });
  page.on("requestfailed", (r) => {
    if (r.url().endsWith("/events")) closed++;
  });
  await page.goto("/#/dashboard");
  const live = page.getByRole("region", { name: "Live agent tree" });
  await expect(live.getByText("Claude Code (host)")).toBeVisible();
  await expect(live.getByText("@fast → codex · gpt-test · low")).toBeVisible();
  await expect(live.getByText("codex/gpt-test · exhausted")).toBeVisible();
  await expect(live.getByText("3 observed calls")).toBeVisible();
  await expect(live.getByText("3 files · +42 / −8")).toBeVisible();
  await expect(live.getByText("7d · 65% remaining")).toBeVisible();
  await expect(live.getByRole("log")).toContainText(
    "Running the panel acceptance checks.",
  );
  await expect(live.getByText("rework → job-review")).toBeVisible();
  await expect(live.getByText(/explorer|researcher|p=0/)).toHaveCount(0);
  expect(opened).toBe(1);
  await page.getByRole("link", { name: "Settings", exact: true }).click();
  await expect.poll(() => closed).toBe(1);
});
test("Dashboard has recorded replay with no running jobs, and an empty state", async ({
  page,
}) => {
  const jobs = seedJobs().filter((j) => j.status === "succeeded");
  await page.route("**/api/sub/jobs?all=1", (route) =>
    route.fulfill({ json: jobs }),
  );
  await page.goto("/#/dashboard");
  await expect(
    page.getByText("Recent run replay · recorded tail"),
  ).toBeVisible();
  await expect(page.locator(".live-pulse")).toHaveCount(0);
  await expect(page.locator(".live-harness")).toHaveAttribute(
    "data-phase",
    "done",
  );
  await page.route("**/api/sub/jobs?all=1", (route) =>
    route.fulfill({ json: [] }),
  );
  await page.reload();
  await expect(page.getByText("No runs to display.")).toBeVisible();
});
test("detail Live tab isolates the job and aborts its stream when switching tabs", async ({
  page,
}) => {
  let closed = 0;
  page.on("requestfailed", (r) => {
    if (r.url().endsWith("/events")) closed++;
  });
  await page.goto("/#/jobs/job-review?tab=live");
  const dialog = page.getByRole("dialog", { name: "Job details" });
  await expect(
    dialog.getByRole("tab", { name: "Live", exact: true }),
  ).toHaveAttribute("aria-selected", "true");
  await expect(dialog.locator(".live-harness")).toHaveCount(1);
  await expect(dialog.getByText("3 observed calls")).toBeVisible();
  await dialog.getByRole("tab", { name: "Summary", exact: true }).click();
  await expect.poll(() => closed).toBe(1);
  await dialog.getByRole("tab", { name: "Live", exact: true }).click();
  await expect(dialog.getByText("3 observed calls")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect.poll(() => closed).toBe(2);
});
test("reduced motion disables pulses and mobile Live never widens the page", async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto("/#/dashboard");
  await expect(page.getByText("3 observed calls")).toBeVisible();
  expect(
    await page
      .locator(".live-pulse")
      .first()
      .evaluate((el) => ({
        animation: getComputedStyle(el).animationName,
        display: getComputedStyle(el).display,
      })),
  ).toEqual({ animation: "none", display: "none" });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth - innerWidth,
    ),
  ).toBeLessThanOrEqual(1);
  await page.goto("/#/jobs/job-review?tab=live");
  await expect(page.getByText("3 observed calls")).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth - innerWidth,
    ),
  ).toBeLessThanOrEqual(1);
});
test("read-only panel shares Live on Dashboard and detail without write actions", async ({
  page,
}) => {
  await page.goto(
    `http://127.0.0.1:${process.env.PANEL_E2E_PORT || 4176}/#/dashboard`,
  );
  await expect(page.getByText("3 observed calls")).toBeVisible();
  await page.goto(
    `http://127.0.0.1:${process.env.PANEL_E2E_PORT || 4176}/#/jobs/job-review?tab=live`,
  );
  await expect(page.getByText("3 observed calls")).toBeVisible();
  await expect(
    page.getByRole("button", { name: /^(Cancel|Resume|Apply|Discard)$/ }),
  ).toHaveCount(0);
});
test("multiple jobs keep four streams and update overflow jobs from summaries", async ({
  page,
}) => {
  const jobs = Array.from({ length: 6 }, (_, i) => ({
    ...seedJobs()[1],
    id: `live-${i}`,
    resumeOf: "",
    resumedBy: [],
  }));
  let opened = 0;
  await page.route("**/api/sub/jobs?all=1", (route) =>
    route.fulfill({ json: jobs }),
  );
  await page.route("**/api/sub/jobs/*/events", async (route) => {
    opened++;
    // An unfinished response body remains open until the page aborts it.
    await route.continue({
      url: route
        .request()
        .url()
        .replace(/live-\d+/, "job-review"),
    });
  });
  await page.goto("/#/dashboard");
  await expect(page.locator(".live-harness")).toHaveCount(6);
  await expect.poll(() => opened).toBe(4);
  await expect(
    page.getByText("Summary only · waiting for stream slot", { exact: true }),
  ).toHaveCount(2);
  await expect(page.locator(".live-metrics").last()).toContainText(
    "10,290 tokens",
  );
  await page.getByRole("link", { name: "Jobs", exact: true }).click();
  await expect(page.locator(".live-harness")).toHaveCount(0);
  expect(opened).toBe(4);
});

for (const panel of [false, true]) {
  for (const [width, height] of [
    [1440, 1100],
    [375, 812],
  ]) {
    test(`${panel ? "panel" : "web"} Live detail contains all content before the footer at ${width}x${height}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height });
      await page.emulateMedia({ reducedMotion: "reduce" });
      const base = panel
        ? `http://127.0.0.1:${process.env.PANEL_E2E_PORT || 4176}`
        : "";
      await page.goto(`${base}/#/jobs/job-review`);
      const drawer = page.getByRole("dialog", { name: "Job details" });
      await drawer.getByRole("tab", { name: "Live", exact: true }).click();
      await expect(
        drawer.getByRole("tab", { name: "Live", exact: true }),
      ).toHaveAttribute("aria-selected", "true");
      await expect(drawer.getByText("3 observed calls")).toBeVisible();
      await expect(drawer.getByRole("log")).toContainText(
        "Running the panel acceptance checks.",
      );
      if (panel)
        await expect(
          drawer.getByRole("button", { name: "Cancel", exact: true }),
        ).toHaveCount(0);
      else
        await expect(
          drawer.getByRole("button", { name: "Cancel", exact: true }),
        ).toHaveCount(1);

      const layout = async () =>
        drawer.evaluate(async (element) => {
          element.scrollTop = element.scrollHeight;
          await new Promise(requestAnimationFrame);
          const live = element.querySelector<HTMLElement>(".live-panel")!;
          const body = element.querySelector<HTMLElement>(".detail-body")!;
          const footer = element.querySelector<HTMLElement>(".detail-footer")!;
          const panelBox = live.getBoundingClientRect();
          const footerBox = footer.getBoundingClientRect();
          const bodyBox = body.getBoundingClientRect();
          const drawerBox = element.getBoundingClientRect();
          const tolerance = 1;
          const violations: string[] = [];
          // Include clipped/offscreen descendants: a last log row painted below
          // the action bar must fail even when fullPage screenshots look plausible.
          for (const node of [live, ...live.querySelectorAll("*")]) {
            const box = node.getBoundingClientRect();
            if (box.bottom > footerBox.top + tolerance)
              violations.push(
                `${node.tagName}.${node.getAttribute("class") || ""} bottom ${box.bottom} > footer ${footerBox.top}`,
              );
          }
          if (panelBox.bottom > bodyBox.bottom + tolerance)
            violations.push("Live panel exceeds tab panel");
          for (const selector of [".live-session", ".panel-caption"]) {
            for (const node of live.querySelectorAll(selector)) {
              const box = node.getBoundingClientRect();
              if (
                box.top < panelBox.top - tolerance ||
                box.bottom > panelBox.bottom + tolerance ||
                box.left < panelBox.left - tolerance ||
                box.right > panelBox.right + tolerance
              )
                violations.push(`${selector} exceeds Live panel border`);
              if (box.bottom > drawerBox.bottom + tolerance)
                violations.push(
                  `${selector} remains below the drawer after scrolling to the bottom`,
                );
            }
          }
          return {
            violations,
            scrollTop: element.scrollTop,
            remaining:
              element.scrollHeight - element.clientHeight - element.scrollTop,
          };
        });
      await expect.poll(async () => (await layout()).violations).toEqual([]);
      const finalLayout = await layout();
      expect(finalLayout.scrollTop).toBeGreaterThan(0);
      expect(finalLayout.remaining).toBeLessThanOrEqual(1);

      // Mutation check: restoring the original flex defaults must reproduce the
      // geometric failure. This guards against a regression test that passes
      // merely because the wrong tab was open or too little content was loaded.
      await page.addStyleTag({
        content: ".jobs-drawer > * { flex-shrink: 1 !important; }",
      });
      await expect
        .poll(async () => (await layout()).violations)
        .not.toEqual([]);
    });
  }
}
