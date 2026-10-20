// Capture the final embedded builds with isolated fixtures. SHOT_BASE optionally
// points to a dev proxy; the default never reads a real job store.
import { chromium, expect } from "@playwright/test";
import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import { startMock } from "../e2e/mock-server.mjs";
const output = process.env.SHOT_OUT || "/tmp/pactify-shots";
const only = process.argv[2];
const mocks = [];
let browser;
try {
  const dashboard = process.env.SHOT_BASE ? null : await startMock();
  if (dashboard) mocks.push(dashboard);
  const panel = await startMock(0, true);
  mocks.push(panel);
  const base = process.env.SHOT_BASE || dashboard.baseURL;
  await mkdir(output, { recursive: true });
  browser = await chromium.launch();
  for (const width of [1440, 390]) {
    const context = await browser.newContext({
      viewport: { width, height: 900 },
      deviceScaleFactor: 1,
      reducedMotion: "reduce",
    });
    const page = await context.newPage();
    const errors = [];
    page.on("pageerror", (error) => errors.push(error.message));
    async function capture(name) {
      await page.evaluate(() => document.fonts.ready);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth - innerWidth,
        ),
        `${name} overflows at ${width}px`,
      ).toBeLessThanOrEqual(1);
      expect(errors, `${name} browser errors`).toEqual([]);
      const path = join(output, `${name}-${width}-final-build.png`);
      await page.screenshot({ path, fullPage: true, animations: "disabled" });
      console.log(path);
    }
    for (const view of ["dashboard", "jobs", "settings"]) {
      if (only && only !== view) continue;
      await page.goto(`${base}/#/${view}`);
      await expect(
        page.getByRole("heading", {
          name: view[0].toUpperCase() + view.slice(1),
          exact: true,
        }),
      ).toBeVisible();
      if (view === "jobs")
        await expect(
          page.getByRole("region", { name: "In progress", exact: true }),
        ).toBeVisible();
      if (view === "settings") {
        await expect(page.getByLabel("codex Default model")).toBeVisible();
        await expect(
          page
            .locator(".routing-heading")
            .getByRole("button", { name: "Edit", exact: true }),
        ).toBeVisible();
      }
      if (view === "dashboard") {
        await expect(
          page.getByRole("img", { name: "Token usage · last 14 days" }),
        ).toBeVisible();
        await expect(
          page.getByRole("progressbar", { name: "codex 7d remaining" }),
        ).toBeVisible();
        await expect(page.getByRole("row", { name: /opencode/ })).toContainText(
          "$0.13",
        );
      }
      await capture(view);
    }
    if (!only || only === "jobs") {
      await page.goto(`${base}/#/jobs/job-summary`);
      const detail = page.getByRole("dialog", {
        name: "Job details",
        exact: true,
      });
      await expect(
        detail.getByRole("heading", { name: "Result summary" }),
      ).toBeVisible();
      for (const tab of ["Summary", "Logs", "Diff", "Brief"]) {
        await detail.getByRole("tab", { name: tab, exact: true }).click();
        if (tab === "Logs") {
          await expect(detail.getByRole("log")).toContainText(
            "message Tests passed.",
          );
          await expect(
            detail.getByText("Finished", { exact: true }),
          ).toBeVisible();
        }
        if (tab === "Diff")
          await expect(detail.locator("pre")).toContainText(
            "+func Listen(port int)",
          );
        if (tab === "Brief")
          await expect(
            detail.getByRole("heading", { name: "Task brief" }),
          ).toBeVisible();
        // A full-page screenshot cannot reveal an internally scrolled modal's top.
        await detail.locator(".detail-scroll").evaluate((node) => {
          node.scrollTop = 0;
        });
        await capture(`job-${tab.toLowerCase()}`);
      }
      await page.keyboard.press("Escape");
      await expect(detail).toHaveCount(0);
      const failed = page
        .locator("article")
        .filter({ hasText: "Fix CLI exit code edge cases" });
      await failed.getByRole("button", { name: "Resume", exact: true }).click();
      const resume = page.getByRole("dialog", {
        name: "Resume Fix CLI exit code edge cases",
        exact: true,
      });
      await resume
        .getByLabel("Instructions for the next run")
        .fill(
          "Fix the remaining CLI exit code checks.\nRun all tests before finishing.",
        );
      await expect(
        resume.getByRole("button", { name: "Start", exact: true }),
      ).toBeEnabled();
      await capture("resume");
      await resume.getByRole("button", { name: "Back", exact: true }).click();
      const worktree = page.locator("article").filter({ hasText: "Share job summaries" });
      await worktree
        .getByRole("button", { name: "Discard", exact: true })
        .click();
      await expect(
        page.getByRole("dialog", { name: /^Discard Share job summaries/ }),
      ).toContainText("permanently delete the worktree");
      await capture("discard-confirm");
    }
    if (!only || only === "panel") {
      await page.goto(`${panel.baseURL}/#/jobs`);
      await expect(
        page.getByRole("region", { name: "In progress", exact: true }),
      ).toBeVisible();
      const actions = page.getByRole("button", {
        name: /^(Cancel|Resume|Apply|Discard)$/,
      });
      await expect(actions).toHaveCount(0);
      await capture("panel-jobs");
      await page
        .getByRole("link", {
          name: "Share job summaries",
          exact: true,
        })
        .click();
      const detail = page.getByRole("dialog", {
        name: "Job details",
        exact: true,
      });
      await expect(
        detail.getByRole("heading", { name: "Result summary" }),
      ).toBeVisible();
      await expect(detail.getByText(/Worktree:.*pending/)).toBeVisible();
      await expect(actions).toHaveCount(0);
      await capture("panel-job-summary");
    }
    await context.close();
  }
} finally {
  await browser?.close();
  for (const mock of mocks) {
    mock.server.closeAllConnections();
    await new Promise((resolve) => mock.server.close(resolve));
  }
}
