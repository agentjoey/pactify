import { test, expect } from "@playwright/test";
import { resetServer, gotoApp } from "./helpers";

// The fallback cards are the dashboard's face for a run that is PAUSED waiting on
// a person. These exercise the wiring the unit tests mock away: the real
// DataSource, the real endpoints, and the polling that has to notice a proposal
// raised while the operator is already looking at the page.
//
// The assertions deliberately end on SERVER state, not on the card vanishing: a
// client that approves without naming a task gets a 404, and the card renders a
// 404 as "already handled elsewhere" by retiring itself. On screen the two are
// indistinguishable; only /__test/fallback/approvals tells them apart.

const proposal = {
  scope: "p3",
  task: "p3-process-b",
  seat: "build2",
  fromRole: "frontend",
  toRole: "frontend-cheap",
  reason: "worker run: run timeout (--run-timeout) exceeded",
};

// A concurrent run's second pause — a different feature, a different seat, a
// separate human decision.
const second = {
  scope: "p4",
  task: "p4-migrate",
  seat: "build3",
  fromRole: "backend",
  toRole: "backend-cheap",
  reason: "worker run: provider quota exhausted",
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
  await page.request.post("/__test/fallback", { data: [proposal] });

  const card = page.getByTestId("fallback-card");
  await card.waitFor({ timeout: 20_000 });
  await expect(card).toContainText("build2 could not run");
  await expect(card).toContainText("frontend-cheap");
  await expect(card).toContainText("run timeout");

  await page.getByTestId("fallback-approve").click();
  await expect(card).toHaveCount(0);

  const r = await page.request.get("/__test/fallback/approvals");
  // Named the task, so the server actually adopted THIS proposal.
  expect((await r.json()).approvals).toEqual([proposal.task]);
});

test("fallback: several pending proposals render as several cards", async ({ page }) => {
  await page.request.post("/__test/fallback", { data: [second, proposal] });

  const cards = page.getByTestId("fallback-card");
  await expect(cards).toHaveCount(2, { timeout: 20_000 });

  // Server order is by scope, and the stack must follow it: p3 before p4.
  await expect(cards.nth(0)).toHaveAttribute("data-scope", "p3");
  await expect(cards.nth(1)).toHaveAttribute("data-scope", "p4");
  // Each card is one decision, and says which one.
  await expect(cards.nth(0)).toContainText("build2 could not run");
  await expect(cards.nth(1)).toContainText("build3 could not run");
  await expect(cards.nth(1)).toContainText("quota exhausted");
});

test("fallback: approving one proposal leaves the other pending", async ({ page }) => {
  await page.request.post("/__test/fallback", { data: [proposal, second] });
  const cards = page.getByTestId("fallback-card");
  await expect(cards).toHaveCount(2, { timeout: 20_000 });

  await cards.nth(1).getByTestId("fallback-approve").click();

  // The approved card goes; the other stays exactly where it was.
  await expect(cards).toHaveCount(1);
  await expect(cards.nth(0)).toHaveAttribute("data-scope", "p3");

  const a = await page.request.get("/__test/fallback/approvals");
  expect((await a.json()).approvals).toEqual([second.task]);

  // …and the server still holds the untouched one.
  const r = await page.request.get(`/api/projects/p1/fallback-proposal`);
  const body = await r.json();
  expect(body.proposals.map((p: { task: string }) => p.task)).toEqual([proposal.task]);
});

test("fallback: dismiss leaves the proposal pending on the server", async ({ page }) => {
  await page.request.post("/__test/fallback", { data: [proposal] });
  await page.getByTestId("fallback-card").waitFor({ timeout: 20_000 });

  await page.getByTestId("fallback-dismiss").click();
  await expect(page.getByTestId("fallback-card")).toHaveCount(0);

  // Dismiss is a view decision, not an approval: the run stays paused and the
  // proposal is still there for the CLI or a reload.
  const r = await page.request.get(`/api/projects/p1/fallback-proposal`);
  expect((await r.json()).proposals).toHaveLength(1);
  const a = await page.request.get("/__test/fallback/approvals");
  expect((await a.json()).approvals).toEqual([]);
});
