// Screenshot matrix for both embedded builds, using hermetic local APIs only.
import { chromium, expect } from "@playwright/test";
import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import { startMock } from "../e2e/mock-server.mjs";
import { startPanelMock } from "../e2e/panel-mock.mjs";

export async function capturePages(panel = false) {
  const output = join(process.env.SHOT_OUT || "/tmp/pactify-shots", panel ? "panel" : "dashboard");
  const { server, baseURL } = panel ? await startPanelMock() : await startMock();
  const views = ["dashboard", "jobs-pipeline", "jobs-list", "shortcuts", "summary", "live", "logs", "diff", "brief", "commands", "settings-machine", "settings-storage", "settings-doctor", "settings-appearance", "settings-shortcuts", "empty", "loading", "error", "401", ...(panel ? ["second-tab"] : ["storage-confirmation", "resume", "discard-confirm"])];
  let browser;
  try {
    await mkdir(output, { recursive: true });
    browser = await chromium.launch();
    for (const theme of ["dark", "light"]) for (const width of [1440, 390]) for (const view of views) {
      const context = await browser.newContext({ viewport: { width, height: 900 }, deviceScaleFactor: 1, reducedMotion: "reduce" });
      await context.addInitScript(theme => { try { localStorage.setItem("pactify-theme", theme); } catch { /* Optional browser preference. */ } }, theme);
      const page = await context.newPage();
      const errors = [];
      page.on("pageerror", error => errors.push(error.message));
      let release = () => {};
      try {
        const jobsPath = panel ? "**/api/jobs?all=1" : "**/api/sub/jobs?all=1";
        if (["empty", "error", "401", "loading"].includes(view)) {
          const pending = new Promise(resolve => { release = resolve; });
          await page.route(jobsPath, async route => {
            if (view === "loading") await pending;
            await route.fulfill(view === "error" || view === "401" ? { status: view === "401" ? 401 : 503, body: "Unavailable" } : { json: [] });
          });
        }
        const detail = ["summary", "live", "logs", "diff", "brief", "commands"].includes(view);
        const settings = view.startsWith("settings-") || view === "storage-confirmation";
        const tab = view === "commands" ? "summary" : view;
        const route = detail ? `jobs/${view === "live" ? "job-review" : "job-summary"}?tab=${tab}` : settings ? "settings" : view === "dashboard" ? "dashboard" : "jobs";
        if (view === "second-tab") {
          const owner = await context.newPage();
          await owner.goto(`${baseURL}/#/jobs`);
          await expect(owner.getByRole("searchbox")).toBeVisible();
          await page.goto(`${baseURL}/?peer=1#/jobs`);
        } else await page.goto(`${baseURL}/#/${route}`);
        if (view === "loading") await expect(page.getByRole("status", { name: "Loading jobs", exact: true })).toBeVisible();
        else if (view === "401" && panel) await expect(page.getByRole("heading", { name: "Session ended", exact: true })).toBeVisible();
        else if (view === "401" || view === "error") await expect(page.getByRole("alert")).toContainText(view === "401" ? "401" : "503");
        else if (view === "empty") await expect(page.getByRole("heading", { name: "Start your first job", exact: true })).toBeVisible();
        else if (detail) {
          const modal = page.getByRole("dialog", { name: "Job details" });
          await expect(modal.getByRole("tab", { name: tab[0].toUpperCase() + tab.slice(1), exact: true })).toHaveAttribute("aria-selected", "true");
          if (view === "live") await expect(modal.getByText("3 observed calls", { exact: true })).toBeVisible();
          if (view === "logs") await expect(modal.getByRole("log")).toContainText("message Tests passed.");
          if (view === "diff") await expect(modal.locator(".diff-added")).toBeVisible();
          if (view === "commands" && panel && width < 700) await modal.getByRole("button", { name: "Show commands", exact: true }).click();
          await modal.locator(".detail-scroll").evaluate(el => { el.scrollTop = 0; });
        } else if (settings) {
          const section = view === "storage-confirmation" ? "storage" : view.slice("settings-".length);
          const labels = { machine: "Harnesses / Models & routing", storage: "Storage", doctor: "Doctor", appearance: "Appearance", shortcuts: "Shortcuts" };
          if (width < 640) await page.getByLabel("Settings section", { exact: true }).selectOption(section);
          else await page.getByRole("navigation", { name: "Settings sections" }).getByRole("button", { name: labels[section], exact: true }).click();
          if (section === "storage") {
            await expect(page.getByRole("region", { name: "Storage", exact: true })).toContainText("4.8 GB");
            if (panel) await expect(page.getByRole("button", { name: "Clean up now", exact: true })).toHaveCount(0);
          }
          if (section === "machine") await expect(page.getByRole("article", { name: "codex", exact: true })).toBeVisible();
          if (view === "storage-confirmation") {
            await page.getByRole("button", { name: "Clean up now", exact: true }).click();
            await expect(page.getByRole("dialog", { name: "Confirm storage cleanup" })).toBeVisible();
          }
        } else if (view === "dashboard") {
          await expect(page.getByRole("img", { name: "Token usage · last 14 days" })).toBeVisible();
          await expect(page.getByRole("progressbar", { name: "codex 7d remaining" })).toBeVisible();
        } else {
          await expect(page.getByRole("searchbox")).toBeVisible();
          if (view === "jobs-list") await page.getByRole("button", { name: "List", exact: true }).click();
          else if (view === "shortcuts") {
            await expect(page.locator(".pipeline-scroll")).toBeVisible();
            await page.getByRole("heading", {name: "Jobs", exact: true}).click();
            await page.keyboard.press("/");
            await expect(page.getByRole("searchbox")).toBeFocused();
            await page.getByRole("searchbox").blur();
            await page.keyboard.press("?");
            await expect(page.getByRole("region", { name: "Keyboard shortcuts", exact: true })).toBeVisible();
          } else if (["resume", "discard-confirm"].includes(view)) {
            if (width < 640) await page.getByRole("button", { name: "Go to Needs you", exact: true }).click();
            else await page.locator('[data-station="attention"] .station-heading').scrollIntoViewIfNeeded();
            const card = page.locator("article").filter({ hasText: view === "resume" ? "Fix CLI exit code edge cases" : "Share job summaries" });
            await card.getByRole("button", { name: view === "resume" ? "Resume" : "Discard", exact: true }).click();
            const dialog = page.getByRole("dialog");
            await expect(dialog).toBeVisible();
            if (view === "resume") await dialog.getByLabel("Instructions for the next run").fill("Fix the remaining checks.\nRun all tests.");
            else await expect(dialog).toContainText("permanently delete the worktree");
          } else await expect(page.locator(".pipeline-scroll")).toBeVisible();
        }
        if (panel) await expect(page.getByRole("button", { name: /^(Cancel|Resume|Apply|Discard|Save defaults|Clean up now)$/ })).toHaveCount(0);
        await page.evaluate(() => document.fonts.ready);
        await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
        expect(await page.evaluate(() => document.documentElement.scrollWidth - innerWidth), `${view} overflow`).toBeLessThanOrEqual(1);
        expect(errors).toEqual([]);
        const path = join(output, `${view}-${theme}-${width}.png`);
        await page.screenshot({ path, fullPage: true, animations: "disabled" });
        console.log(path);
      } finally {
        release();
        await page.unrouteAll({ behavior: "wait" });
        await context.close();
      }
    }
  } finally {
    await browser?.close();
    server.closeAllConnections();
    await new Promise(resolve => server.close(resolve));
  }
}
