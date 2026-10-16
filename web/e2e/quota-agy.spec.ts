import { expect, test } from "@playwright/test";

for (const width of [1440, 390]) {
  test(`agy quota snapshot from final build at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.route("**/api/sub/cost?**", async (route) => {
      const response = await route.fetch();
      const report = await response.json();
      report.quotaSnapshots.push({
        harness: "agy", jobId: "agy-job", observedAt: new Date().toISOString(), rateLimit: "",
        buckets: [{ id: "gemini-weekly", window: "weekly", remainingFraction: 0.75, resetAt: "2099-10-23T00:40:01Z" }],
      });
      await route.fulfill({ response, json: report });
    });
    await page.goto("/#/dashboard");
    const bar = page.getByRole("progressbar", { name: "agy gemini-weekly remaining" });
    await expect(bar).toBeVisible();
    await expect(bar).toHaveAttribute("value", "75");
    await expect(page.getByRole("progressbar", { name: "codex 7d remaining" })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth - innerWidth)).toBeLessThanOrEqual(1);
    await page.screenshot({ path: test.info().outputPath(`agy-quota-${width}-final-build.png`), fullPage: true });
  });
}
