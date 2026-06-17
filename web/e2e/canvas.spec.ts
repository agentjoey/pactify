import { test, expect } from "@playwright/test";
import {
  resetServer,
  gotoApp,
  switchToCanvas,
  switchToPlan,
  rfNode,
  awaitMeasured,
  transformOf,
  allNodeTransforms,
  centerOf,
  dragNodeBy,
  connectDrag,
  getPuts,
  pushSnapshot,
  waitForPutCountAbove,
  settleBoundingBox,
} from "./helpers";
import { snapshotT2InProgress } from "./fixtures.mjs";

// Plan-mode canvas regressions — three of the four user-reported bugs plus the
// SSE-merge guarantee. Each maps to a root cause in spec §0.

test.beforeEach(async ({ page }) => {
  await resetServer(page);
  await gotoApp(page);
  await switchToCanvas(page);
  await switchToPlan(page);
});

// Bug #1: moving one box must not move the others (position materialization).
test("drag-isolation: dragging one node leaves the others put", async ({ page }) => {
  const t1 = rfNode(page, "task:t1");
  await awaitMeasured(t1);
  // Wait until all four nodes (f1 feature + t1 + t2 + two seats) are measured —
  // the materialization PUT only lands once positions exist.
  await awaitMeasured(rfNode(page, "task:t2"));

  // Wait for the first materialization PUT to land (debounce ~0.8s) so the
  // baseline "everyone has a saved position" PUT is on record — poll the PUT log
  // instead of a fixed sleep.
  await waitForPutCountAbove(page, 0);
  const putsBefore = await getPuts(page);
  expect(putsBefore.length).toBeGreaterThan(0);
  const firstPositions = putsBefore[putsBefore.length - 1].positions ?? {};
  const putCountBeforeDrag = putsBefore.length;

  const before = await allNodeTransforms(page);

  // Drag t1 by a clear delta.
  await dragNodeBy(page, t1, 150, 100);

  // Every OTHER node's transform is byte-for-byte unchanged.
  const after = await allNodeTransforms(page);
  for (const id of Object.keys(before)) {
    if (id === "task:t1") continue;
    expect(after[id], `node ${id} must not move when t1 is dragged`).toBe(before[id]);
  }
  // t1 itself moved.
  expect(after["task:t1"]).not.toBe(before["task:t1"]);

  // After the drag debounce, the last PUT changes ONLY t1's entry; every other
  // saved position equals the pre-drag materialization PUT. Poll for the new PUT
  // produced by the drag rather than sleeping a fixed window.
  await waitForPutCountAbove(page, putCountBeforeDrag);
  const putsAfter = await getPuts(page);
  const last = putsAfter[putsAfter.length - 1].positions ?? {};
  for (const id of Object.keys(firstPositions)) {
    if (id === "task:t1") continue;
    expect(last[id], `saved position of ${id} must be unchanged`).toEqual(firstPositions[id]);
  }
  expect(last["task:t1"]).not.toEqual(firstPositions["task:t1"]);
});

// Bug #2: box-to-box wiring must work. Create two drafts in the same feature, then
// wire draft A's bottom port → draft B's top port and assert an edge appears.
test("connect: wiring two drafts adds an edge", async ({ page }) => {
  // Create two drafts via the New Task editor (feature defaults to f1).
  for (const id of ["d1", "d2"]) {
    await page.locator('[data-testid="canvas-toolbar"] button', { hasText: "Task" }).click();
    const editor = page.locator('[data-testid="task-editor"]');
    await editor.waitFor();
    await editor.getByLabel("task id").fill(id);
    // Feature select already defaults to f1 (only committed feature).
    await editor.getByRole("button", { name: "Add draft" }).click();
    await editor.waitFor({ state: "hidden" });
  }

  const a = rfNode(page, "draft:d1");
  const b = rfNode(page, "draft:d2");
  await awaitMeasured(a);
  await awaitMeasured(b);

  // The two new drafts are placed below the entry fitView; bring their ports into
  // the viewport so the connection drag can hit them (RF hit-tests live DOM —
  // off-screen handles can't be grabbed). A real author pans/fits the same way.
  await page.getByRole("button", { name: "fit view" }).click();
  // Wait for the fitView animation to rest (boundingBox stable two reads running)
  // rather than sleeping a fixed window.
  await settleBoundingBox(a);

  const edgesBefore = await page.locator(".react-flow__edge").count();

  // Source = draft A's bottom port (.task-port-out); target = draft B's top port
  // (.task-port-in). connectDrag drives the drag via CDP pointer events; it returns
  // whether the in-flight preview line was observed mid-drag (best-effort — the
  // .react-flow__connectionline element renders for a timing-sensitive transient
  // window in headless Chromium). The LOAD-BEARING assertion is the edge appearing.
  const outPort = a.locator(".task-port-out");
  const inPort = b.locator(".task-port-in");

  await connectDrag(page, outPort, inPort);

  // Edge count increased by one — the wiring actually landed (bug #2 fixed). This
  // is the load-bearing assertion; the transient in-flight preview line is a
  // best-effort signal inside connectDrag, not a gate (its render window is
  // timing-sensitive in headless Chromium).
  await expect(page.locator(".react-flow__edge")).toHaveCount(edgesBefore + 1);
});

// connect (negative, spec §4 case② second half): wiring to a COMMITTED task is
// rejected. A committed task's deps were frozen at assign time, so dropping a
// draft's out-port onto a committed task's in-port must NOT create an edge — and
// must surface the "已固定" canvas notice instead.
test("connect: wiring to a committed task is rejected (edge count held + notice)", async ({ page }) => {
  // One draft in f1 (same feature as committed t1/t2 — the committed-target rule
  // fires before the same-feature check, so f1 is the right place to prove it).
  await page.locator('[data-testid="canvas-toolbar"] button', { hasText: "Task" }).click();
  const editor = page.locator('[data-testid="task-editor"]');
  await editor.waitFor();
  await editor.getByLabel("task id").fill("d1");
  await editor.getByRole("button", { name: "Add draft" }).click();
  await editor.waitFor({ state: "hidden" });

  const draft = rfNode(page, "draft:d1");
  const committed = rfNode(page, "task:t1");
  await awaitMeasured(draft);
  await awaitMeasured(committed);

  // Bring both ports into the viewport (RF hit-tests live DOM — see connectDrag).
  await page.getByRole("button", { name: "fit view" }).click();
  // Wait for the fitView animation to rest (boundingBox stable two reads running).
  await settleBoundingBox(draft);

  const edgesBefore = await page.locator(".react-flow__edge").count();

  // Source = draft's bottom port (.task-port-out); target = the COMMITTED task's
  // top port (.task-port-in). The drop is invalid: t1's deps are fixed.
  const outPort = draft.locator(".task-port-out");
  const inPort = committed.locator(".task-port-in");
  await connectDrag(page, outPort, inPort);

  // No edge appears — the invalid wiring was rejected (load-bearing assertion).
  await expect(page.locator(".react-flow__edge")).toHaveCount(edgesBefore);
  // And the author got the "locked" notice explaining why.
  const notice = page.locator('[data-testid="canvas-notice"]');
  await expect(notice).toBeVisible();
  await expect(notice).toContainText("locked");
});

// connect (negative, spec §4 case② first half): a cross-feature dep is rejected.
// Deps are same-feature by protocol, so dropping a draft in feature f1 onto a
// draft in a SECOND feature must NOT create an edge — and must surface the
// "同一个 feature" canvas notice instead. Because both endpoints are DRAFTS (not
// committed), the same-feature rule is the one that fires (committed-target check
// passes), proving that branch is reachable via onConnectEnd.
test("connect: cross-feature wiring is rejected (edge count held + notice)", async ({ page }) => {
  // Author a second feature (f2) via the toolbar's New Feature form.
  await page.locator('[data-testid="canvas-toolbar"] button', { hasText: "Feature" }).click();
  const tbar = page.locator('[data-testid="canvas-toolbar"]');
  await tbar.getByLabel("feature id").fill("f2");
  await tbar.getByLabel("feature branch").fill("feat-f2");
  await tbar.getByRole("button", { name: "Add" }).click();

  // Draft d1 in f1 (the default feature) and draft d2 in f2.
  const addDraft = async (taskId: string, featureLabel: string) => {
    await page.locator('[data-testid="canvas-toolbar"] button', { hasText: "Task" }).click();
    const editor = page.locator('[data-testid="task-editor"]');
    await editor.waitFor();
    await editor.getByLabel("task id").fill(taskId);
    await editor.getByLabel("feature").selectOption({ label: featureLabel });
    await editor.getByRole("button", { name: "Add draft" }).click();
    await editor.waitFor({ state: "hidden" });
  };
  await addDraft("d1", "f1");
  await addDraft("d2", "f2 (draft)");

  const from = rfNode(page, "draft:d1");
  const to = rfNode(page, "draft:d2");
  await awaitMeasured(from);
  await awaitMeasured(to);

  // Bring both ports into the viewport (RF hit-tests live DOM — see connectDrag).
  await page.getByRole("button", { name: "fit view" }).click();
  await settleBoundingBox(from);

  const edgesBefore = await page.locator(".react-flow__edge").count();

  // Source = f1 draft's out-port; target = f2 draft's in-port. The drop is invalid:
  // a dep must stay within one feature.
  await connectDrag(page, from.locator(".task-port-out"), to.locator(".task-port-in"));

  // No edge appears — the cross-feature wiring was rejected (load-bearing).
  await expect(page.locator(".react-flow__edge")).toHaveCount(edgesBefore);
  // And the author got the "same feature" notice explaining why.
  const notice = page.locator('[data-testid="canvas-notice"]');
  await expect(notice).toBeVisible();
  await expect(notice).toContainText("same feature");
});

// Bug #1 (SSE variant): a snapshot arriving mid-drag must not teleport the node
// being dragged (mergeNodes preserves the in-flight position).
test("drag-during-sse: an SSE snapshot mid-drag does not teleport the dragged node", async ({ page }) => {
  const t1 = rfNode(page, "task:t1");
  await awaitMeasured(t1);
  await awaitMeasured(rfNode(page, "task:t2"));
  await waitForPutCountAbove(page, 0); // let materialization settle (poll PUT log)

  // Baseline: t2 starts `assigned` → diamond medallion. After the SSE snapshot
  // below it flips to `in_progress` → ⚡; we wait on that glyph change to confirm
  // the snapshot actually merged (instead of a blind sleep).
  const t2Med = rfNode(page, "task:t2").locator('[data-testid="task-card-med"]');
  await expect(t2Med).toHaveText("◇");

  const start = await centerOf(t1);

  // Begin dragging t1 and move a few steps WITHOUT releasing.
  await t1.hover();
  await page.mouse.down();
  await page.mouse.move(start.x + 4, start.y + 4, { steps: 2 });
  await page.mouse.move(start.x + 80, start.y + 60, { steps: 6 });
  const midTransform = await transformOf(t1);

  // Push a new snapshot (t2 → in_progress) over SSE while still holding t1.
  await pushSnapshot(page, snapshotT2InProgress());
  // Wait for the refetch + merge to actually land: t2's medallion flips ◇ → ⚡.
  // This proves the snapshot was applied before we assert t1 wasn't teleported.
  await expect(t2Med).toHaveText("⚡");

  // Continue dragging — t1 keeps following the pointer (transform advances),
  // it was NOT snapped back by the snapshot-driven merge.
  await page.mouse.move(start.x + 150, start.y + 110, { steps: 6 });
  const afterTransform = await transformOf(t1);
  expect(afterTransform).not.toBe(midTransform); // kept following the pointer

  await page.mouse.up();

  // Final rest: the node landed where the pointer left it, NOT snapped back to its
  // original slot. Its on-screen center is clearly displaced from the start. Wait
  // for drag-stop + any post-drop merge to settle by polling the boundingBox until
  // it stops moving (two equal reads) rather than sleeping a fixed window.
  const t1Box = await settleBoundingBox(t1);
  expect(t1Box).not.toBeNull();
  const finalCenterX = t1Box!.x + t1Box!.width / 2;
  expect(Math.abs(finalCenterX - start.x)).toBeGreaterThan(40);
});
