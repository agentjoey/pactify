import { test, expect } from "@playwright/test";
import {
  resetServer,
  gotoApp,
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
} from "./helpers";
import { snapshotT2InProgress } from "./fixtures.mjs";

// Plan-mode canvas regressions — three of the four user-reported bugs plus the
// SSE-merge guarantee. Each maps to a root cause in spec §0.

test.beforeEach(async ({ page }) => {
  await resetServer(page);
  await gotoApp(page);
  await switchToPlan(page);
});

// Bug #1: moving one box must not move the others (position materialization).
test("drag-isolation: dragging one node leaves the others put", async ({ page }) => {
  const t1 = rfNode(page, "task:t1");
  await awaitMeasured(t1);
  // Wait until all four nodes (f1 feature + t1 + t2 + two seats) are measured —
  // the materialization PUT only lands once positions exist.
  await awaitMeasured(rfNode(page, "task:t2"));

  // Let the first materialization PUT settle (debounce ~0.8s) so the baseline
  // "everyone has a saved position" PUT is on record.
  await page.waitForTimeout(1300);
  const putsBefore = await getPuts(page);
  expect(putsBefore.length).toBeGreaterThan(0);
  const firstPositions = putsBefore[putsBefore.length - 1].positions ?? {};

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
  // saved position equals the pre-drag materialization PUT.
  await page.waitForTimeout(1300);
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
  await page.waitForTimeout(400);

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
  await page.waitForTimeout(400);

  const edgesBefore = await page.locator(".react-flow__edge").count();

  // Source = draft's bottom port (.task-port-out); target = the COMMITTED task's
  // top port (.task-port-in). The drop is invalid: t1's deps are fixed.
  const outPort = draft.locator(".task-port-out");
  const inPort = committed.locator(".task-port-in");
  await connectDrag(page, outPort, inPort);

  // No edge appears — the invalid wiring was rejected (load-bearing assertion).
  await expect(page.locator(".react-flow__edge")).toHaveCount(edgesBefore);
  // And the author got the "已固定" notice explaining why.
  const notice = page.locator('[data-testid="canvas-notice"]');
  await expect(notice).toBeVisible();
  await expect(notice).toContainText("已固定");
});

// Bug #1 (SSE variant): a snapshot arriving mid-drag must not teleport the node
// being dragged (mergeNodes preserves the in-flight position).
test("drag-during-sse: an SSE snapshot mid-drag does not teleport the dragged node", async ({ page }) => {
  const t1 = rfNode(page, "task:t1");
  await awaitMeasured(t1);
  await awaitMeasured(rfNode(page, "task:t2"));
  await page.waitForTimeout(1300); // let materialization settle

  const start = await centerOf(t1);

  // Begin dragging t1 and move a few steps WITHOUT releasing.
  await t1.hover();
  await page.mouse.down();
  await page.mouse.move(start.x + 4, start.y + 4, { steps: 2 });
  await page.mouse.move(start.x + 80, start.y + 60, { steps: 6 });
  const midTransform = await transformOf(t1);

  // Push a new snapshot (t2 → in_progress) over SSE while still holding t1.
  await pushSnapshot(page, snapshotT2InProgress());
  await page.waitForTimeout(150); // let the refetch + merge run

  // Continue dragging — t1 keeps following the pointer (transform advances),
  // it was NOT snapped back by the snapshot-driven merge.
  await page.mouse.move(start.x + 150, start.y + 110, { steps: 6 });
  const afterTransform = await transformOf(t1);
  expect(afterTransform).not.toBe(midTransform); // kept following the pointer

  await page.mouse.up();
  await page.waitForTimeout(300); // let drag-stop + any post-drop merge settle

  // Final rest: the node landed where the pointer left it, NOT snapped back to its
  // original slot. Its on-screen center is clearly displaced from the start.
  const t1Box = await t1.boundingBox();
  expect(t1Box).not.toBeNull();
  const finalCenterX = t1Box!.x + t1Box!.width / 2;
  expect(Math.abs(finalCenterX - start.x)).toBeGreaterThan(40);
});
