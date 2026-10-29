import { expect, test } from "@playwright/test";
import type { Job } from "../src/components/jobs/types";

for (const panel of [false, true]) test.describe(panel ? "panel line activity" : "dashboard line activity", () => {
  test.use({ baseURL: panel ? `http://127.0.0.1:${process.env.PANEL_E2E_PORT || 4176}` : `http://localhost:${process.env.E2E_PORT || 4173}` });
  for (const width of [1440, 390]) test(`quiet lines, search and saved visibility at ${width}px`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 844 });
    const now = Date.now();
    const ago = (days: number) => new Date(now - days * 86400000).toISOString();
    const job = (name: string, extra: Partial<Job> = {}): Job => ({
      id: name, title: name, repo: `/workspace/${name}`, harness: "codex", status: "succeeded", modelRequested: "model",
      cwd: "/workspace", access: "write", configMode: "minimal", duration: "1m", createdAt: ago(10),
      inputTokens: 0, outputTokens: 0, attention: [], resumeOf: "", resumedBy: [], ...extra,
    });
    let dismissed = false;
    await page.route(/\/api\/(?:sub\/)?jobs(?:\?.*)?$/, route => route.fulfill({ json: [
      job("running-line", { status: "running", startedAt: ago(2) }),
      job("old-attention", { status: "failed", actions: dismissed ? [] : ["dismiss"], attention: dismissed ? [] : [{ kind: "failed", message: "Job failed" }] }),
      job("recent-line", { finishedAt: ago(0.1) }),
      job("quiet-line", { finishedAt: ago(4) }),
      job("quiet-older", { finishedAt: ago(7) }),
    ] }));
    await page.route("**/api/sub/jobs/old-attention/dismiss", route => {
      dismissed = true;
      return route.fulfill({ json: { ok: true } });
    });
    await page.goto("/#/jobs");
    await expect(page.getByText("Quiet lines · 2")).toBeVisible();
    await expect(page.locator(".line-rail h3")).toHaveText(["running-line", "old-attention", "recent-line"]);
    await expect(page.getByRole("link", { name: "quiet-line", exact: true })).toHaveCount(0);
    const chip = page.getByRole("button", { name: "quiet-line · 4d ago" });
    await chip.click();
    await expect(chip).toHaveAttribute("aria-expanded", "true");
    await expect(page.getByRole("link", { name: "quiet-line", exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: "quiet-older", exact: true })).toHaveCount(0);
    await page.getByRole("button", { name: "Show quiet lines", exact: true }).click();
    await expect(page.getByRole("link", { name: "quiet-older", exact: true })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth - innerWidth)).toBeLessThanOrEqual(1);
    await page.screenshot({ path: testInfo.outputPath("quiet-lines.png"), fullPage: true });
    await page.getByRole("button", { name: "Hide quiet lines", exact: true }).click();
    await page.getByRole("searchbox").fill("quiet-line");
    await expect(page.getByRole("link", { name: "quiet-line", exact: true })).toBeAttached();
    await expect(page.getByText(/Quiet lines ·/)).toHaveCount(0);
    await page.getByRole("searchbox").fill("");
    await page.getByRole("button", { name: "All lines", exact: true }).click();
    await page.reload();
    await expect(page.getByRole("button", { name: "All lines", exact: true })).toHaveAttribute("aria-pressed", "true");
    await expect(page.getByText(/Quiet lines ·/)).toHaveCount(0);
    if (panel) {
      await expect(page.getByRole("button", { name: "Dismiss", exact: true })).toHaveCount(0);
    } else {
      if (width === 390) await page.getByRole("button", { name: "Go to Needs you", exact: true }).click();
      await page.getByRole("button", { name: "Dismiss", exact: true }).click();
      await expect(page.getByRole("dialog")).toContainText("Dismiss this job? It stays in history but leaves Needs you.");
      await page.getByRole("button", { name: "Confirm Dismiss", exact: true }).click();
      await expect(page.getByRole("button", { name: "Dismiss", exact: true })).toHaveCount(0);
      await expect(page.locator('[data-station="completed"] a').filter({ hasText: "old-attention" })).toHaveCount(1);
    }
  });
});
