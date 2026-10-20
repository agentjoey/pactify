// cd web && npm run build:panel && node scripts/shots-panel.mjs
// Exercises the final embedded build on an isolated local mock server.
import { mkdtemp } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { chromium, expect } from "@playwright/test";
import { startPanelMock } from "../e2e/panel-mock.mjs";

const output = await mkdtemp(join(tmpdir(), "pactify-panel-shots-"));
const { server, baseURL } = await startPanelMock();
const views = ["jobs", "compact", "second-tab", "summary", "commands", "live", "logs", "diff", "brief", "dashboard", "settings", "empty", "401", "5xx", "loading"];
let browser;
try {
  browser = await chromium.launch();
  for (const theme of ["light", "dark"]) {
    for (const [device, width] of [["desktop", 1440], ["mobile", 390]]) {
      for (const view of views) {
        const context = await browser.newContext({viewport:{width,height:width === 390 ? 844 : 900},colorScheme:theme,deviceScaleFactor:1});
        let release = () => {};
        const page = await context.newPage();
        try {
          const errors = [];
          page.on("pageerror", error => errors.push(error.message));
          if (view === "401" || view === "5xx") {
            await page.route("**/api/**", route => route.fulfill({status:view === "401" ? 401 : 503,body:"Unavailable"}));
          } else if (view === "empty") {
            await page.route("**/api/jobs?all=1", route => route.fulfill({json:[]}));
          } else if (view === "loading") {
            const pending = new Promise(resolve => {release=resolve;});
            await page.route("**/api/jobs?all=1", async route => {await pending; await route.fulfill({json:[]});});
          }
          const tab = ["summary","live","logs","diff","brief"].includes(view);
          const route = view === "commands" ? "jobs/job-summary?tab=summary" : tab ? `jobs/job-review?tab=${view}` : ["dashboard","settings"].includes(view) ? view : view === "5xx" ? "dashboard" : "jobs";
          if (view === "second-tab") {
            const owner = await context.newPage();
            await owner.goto(`${baseURL}/#/jobs`);
            await owner.getByRole("heading", {name:"Jobs",exact:true}).waitFor();
            await page.goto(`${baseURL}/?peer=1#/jobs`);
          } else await page.goto(`${baseURL}/#/${route}`);
          if (view === "commands") {
            const modal=page.getByRole("dialog", {name:"Job details"});
            if (width <= 700) await modal.getByRole("button", {name:"Show commands",exact:true}).click();
            await expect(modal.locator(".next-steps")).toHaveCount(1);
            const list=modal.getByRole("region", {name:width <= 700 ? "Terminal commands" : "Next steps",exact:true});
            await list.scrollIntoViewIfNeeded();
            await expect(list.getByRole("button", {name:/^Copy pactify sub /})).toHaveCount(5);
          }
          if (view === "compact") {
            await page.getByRole("button", {name:"Compact list",exact:true}).click();
            await page.getByRole("list", {name:"Completed jobs"}).first().waitFor();
            if (width > 1100) {
              const positions=await page.locator('.compact-history [data-column="harness"]').evaluateAll(cells => cells.map(cell => cell.getBoundingClientRect().x));
              for (const x of positions) expect(Math.abs(x-positions[0])).toBeLessThanOrEqual(1);
            }
          }
          if (view === "loading") await page.getByRole("status",{name:"Loading jobs",exact:true}).waitFor();
          else if (view === "401") await page.getByRole("heading",{name:"Session ended",exact:true}).waitFor();
          else if (view === "5xx") {
            await page.getByRole("alert").waitFor();
            await expect(page.locator(".dashboard-content > .panel-section").last()).toContainText("—");
          }
          else if (view === "empty") await page.getByRole("heading",{name:"Start your first job",exact:true}).waitFor();
          else if (tab || view === "commands") {
            const modal=page.getByRole("dialog",{name:"Job details"});
            await expect(modal.getByRole("tab",{name:view === "commands" ? "Summary" : view[0].toUpperCase()+view.slice(1),exact:true})).toHaveAttribute("aria-selected","true");
            if(view === "logs") await modal.getByText("Finished",{exact:true}).waitFor();
            if(view === "live") await modal.getByText("3 observed calls",{exact:true}).waitFor();
            if(view === "diff") {
              await modal.locator(".diff-added").waitFor();
              await expect(modal.locator(".diff-file summary")).toHaveText("internal/subpanel/server.go +1 / −0");
            }
          } else if (view === "dashboard") await page.getByRole("img",{name:"Token usage · last 14 days"}).waitFor();
          else if (view === "settings") await page.getByRole("article",{name:"codex",exact:true}).waitFor();
          else await page.getByRole("link",{name:"Build a local job panel",exact:true}).waitFor();
          await page.evaluate(() => document.fonts.ready);
          await expect(page.locator("html")).toHaveAttribute("data-theme",theme);
          if(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw new Error(`horizontal overflow: ${theme} ${device} ${view}`);
          if(errors.length) throw new Error(errors.join("\n"));
          const path=join(output,`${view}-${theme}-${device}.png`);
          await page.screenshot({path,fullPage:true});
          console.log(path);
        } finally {
          release();
          await page.unrouteAll({behavior:"wait"});
          await context.close();
        }
      }
    }
  }
} finally {
  await browser?.close();
  server.closeAllConnections();
  await new Promise(resolve => server.close(resolve));
}
