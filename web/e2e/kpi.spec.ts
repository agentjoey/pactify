import { expect, test } from "@playwright/test";
import { seedJobs } from "./jobs-fixtures.mjs";

test.beforeEach(async ({ request }) => {
  await request.post("/__test/reset");
});

const flow = (page: import("@playwright/test").Page) =>
  page.locator(".dot-matrix circle").first().evaluate((dot) => getComputedStyle(dot).animationName);

test("KPI labels stay readable next to the numbers and in progress is blue in light mode", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/#/dashboard");
  await page.evaluate(() => document.documentElement.setAttribute("data-theme", "light"));
  const kpi = page.locator(".kpi").first();
  await expect(kpi.locator("strong")).toHaveText("1");
  const sizes = await kpi.evaluate((node) => ({
    number: parseFloat(getComputedStyle(node.querySelector("strong")!).fontSize),
    label: parseFloat(getComputedStyle(node.querySelector("h2")!).fontSize),
    color: getComputedStyle(node.querySelector("strong")!).color,
  }));
  expect(sizes.number).toBeLessThanOrEqual(56);
  expect(sizes.label).toBeGreaterThanOrEqual(12);
  expect(sizes.color).toBe("rgb(36, 87, 214)");
});

test("dot matrix flows while a job runs and rests when idle or motion is reduced", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/#/dashboard");
  await expect(page.locator(".dot-matrix")).toHaveAttribute("data-running", "true");
  await expect.poll(() => flow(page)).toBe("dot-flow");
  const delays = await page.locator(".dot-matrix circle").evaluateAll((dots) =>
    [0, 1, 23].map((i) => parseFloat(getComputedStyle(dots[i]).animationDelay)),
  );
  // The wave travels left to right: later columns start later.
  expect(delays[0]).toBeLessThan(delays[1]);
  expect(delays[1]).toBeLessThan(delays[2]);

  await page.emulateMedia({ reducedMotion: "reduce" });
  await expect.poll(() => flow(page)).toBe("none");
  await page.emulateMedia({ reducedMotion: "no-preference" });

  const idle = seedJobs().filter((j) => !["running", "retrying"].includes(j.status));
  await page.route("**/api/sub/jobs?all=1", (route) => route.fulfill({ json: idle }));
  await page.reload();
  await expect(page.locator(".kpi").first().locator("strong")).toHaveText("0");
  await expect(page.locator(".dot-matrix")).toHaveAttribute("data-running", "false");
  expect(await flow(page)).toBe("none");
});
