import { test, expect } from "@playwright/test";
import { resetServer, gotoApp } from "./helpers";

// The fallback card is the dashboard's face for a run that is PAUSED waiting on
// a person. These exercise the wiring the unit tests mock away: the real
// DataSource, the real endpoints, and the polling that has to notice a proposal
// raised while the operator is already looking at the page.

const proposal = {
  task: "p3-process-b",
  seat: "build2",
  fromRole: "frontend",
  toRole: "frontend-cheap",
  reason: "worker run: run timeout (--run-timeout) exceeded",
};

test.beforeEach(async ({ page }) => {
  await resetServer(page);
  await gotoApp(page);
  await page.getByTestId("lens-dashboard").click();
});

test("fallback: proposal raised mid-run surfaces, and approving retires the card", async ({
  page,
}) => {
  // Nothing pending → no card, no noise.
  await expect(page.getByTestId("fallback-card")).toHaveCount(0);

  // The escalation happens while the page is open — no reload.
  await page.request.post("/__test/fallback", { data: proposal });

  const card = page.getByTestId("fallback-card");
  await card.waitFor({ timeout: 20_000 });
  await expect(card).toContainText("build2 could not run");
  await expect(card).toContainText("frontend-cheap");
  await expect(card).toContainText("run timeout");

  await page.getByTestId("fallback-approve").click();
  await expect(card).toHaveCount(0);

  const r = await page.request.get("/__test/fallback/approvals");
  expect((await r.json()).approvals).toBe(1);
});

test("fallback: dismiss leaves the proposal pending on the server", async ({ page }) => {
  await page.request.post("/__test/fallback", { data: proposal });
  await page.getByTestId("fallback-card").waitFor({ timeout: 20_000 });

  await page.getByTestId("fallback-dismiss").click();
  await expect(page.getByTestId("fallback-card")).toHaveCount(0);

  // Dismiss is a view decision, not an approval: the run stays paused and the
  // proposal is still there for the CLI or a reload.
  const r = await page.request.get(`/api/projects/p1/fallback-proposal`);
  expect((await r.json()).pending).toBe(true);
  const a = await page.request.get("/__test/fallback/approvals");
  expect((await a.json()).approvals).toBe(0);
});
