import { expect, type Page, type Locator } from "@playwright/test";

// Shared e2e helpers. The drag/connect patterns follow the xyflow official e2e
// playbook: wait for React Flow to MEASURE a node (visibility:visible) before
// touching it, drive pointer drags via mouse.down/move/up, and read positions
// off the node wrapper's CSS transform.

export const PROJECT_ID = "p1";

// resetServer restores the mock server's seed state + clears layout/puts. Call in
// beforeEach so each test starts from the same fixture (the PUT log especially).
export async function resetServer(page: Page) {
  const r = await page.request.post("/__test/reset");
  expect(r.ok()).toBeTruthy();
}

// gotoApp loads the SPA and waits for the app shell to mount. View-agnostic:
// it does NOT navigate to any specific lens — callers use switchToCanvas /
// switchToOffice / etc. to reach the lens they need.
export async function gotoApp(page: Page) {
  await page.goto("/");
  await page.locator('[data-testid="app-root"]').waitFor();
}

// switchToCanvas presses "2" (IA v2 keymap) and waits for the canvas stage.
export async function switchToCanvas(page: Page) {
  await page.keyboard.press("2");
  await page.locator('[data-testid="canvas-root"]').waitFor();
}

// switchToOffice presses "2" to reach the Canvas view (which defaults to
// office mode), then waits for the office surface to mount.
export async function switchToOffice(page: Page) {
  await page.keyboard.press("2");
  await page.locator('[data-testid="office-view"]').waitFor();
}

// switchToPlan clicks the Plan mode segment inside the Canvas view. Callers
// must already be on the canvas view (switchToCanvas) before calling this.
export async function switchToPlan(page: Page) {
  await page.locator('[data-testid="mode-plan"]').click();
  await page.locator('[data-testid="canvas-root"]').waitFor();
}

// rfNode returns the React Flow node wrapper for a given flow id (e.g. "task:t1").
export function rfNode(page: Page, flowId: string): Locator {
  return page.locator(`.react-flow__node[data-id="${flowId}"]`);
}

// awaitMeasured asserts React Flow has measured a node (visibility flips to
// visible only after measurement) — the xyflow-recommended settle gate before
// any geometry read or drag.
export async function awaitMeasured(node: Locator) {
  await expect(node).toHaveCSS("visibility", "visible");
}

// transformOf reads the inline transform RF writes on a node wrapper (the
// translate(x,y) that positions it). Used to assert a node did / didn't move.
export async function transformOf(node: Locator): Promise<string> {
  return (await node.evaluate((el) => (el as HTMLElement).style.transform)) || "";
}

// allNodeTransforms returns a map flowId → transform for every rendered RF node.
export async function allNodeTransforms(page: Page): Promise<Record<string, string>> {
  return page.evaluate(() => {
    const out: Record<string, string> = {};
    for (const el of document.querySelectorAll<HTMLElement>(".react-flow__node")) {
      const id = el.getAttribute("data-id");
      if (id) out[id] = el.style.transform || "";
    }
    return out;
  });
}

// centerOf returns the viewport-space center of a locator's bounding box.
export async function centerOf(loc: Locator): Promise<{ x: number; y: number }> {
  const b = await loc.boundingBox();
  if (!b) throw new Error("no bounding box for locator");
  return { x: b.x + b.width / 2, y: b.y + b.height / 2 };
}

// dragNodeBy performs an xyflow-style node drag by a screen-space delta:
// hover → mouse.down on the node center → stepped mouse.move → mouse.up.
export async function dragNodeBy(
  page: Page,
  node: Locator,
  dx: number,
  dy: number,
) {
  await awaitMeasured(node);
  const c = await centerOf(node);
  await node.hover();
  await page.mouse.down();
  // A first small move arms RF's drag (it has a small start threshold), then the
  // real delta in steps so intermediate dragging frames fire.
  await page.mouse.move(c.x + 4, c.y + 4, { steps: 2 });
  await page.mouse.move(c.x + dx, c.y + dy, { steps: 8 });
  await page.mouse.up();
}

// getPuts returns the ordered list of PUT layout bodies the mock server recorded.
export async function getPuts(page: Page): Promise<Array<{ v?: number; positions?: Record<string, { x: number; y: number }>; office?: Record<string, { x: number; y: number }> }>> {
  const r = await page.request.get("/__test/puts");
  return r.json();
}

// waitForPutCountAbove polls the recorded PUT log until at least `min`+1 PUTs have
// landed (i.e. count > min). Replaces a fixed waitForTimeout that blindly hoped a
// debounced PUT (≈0.8s) had fired — this waits on the observable instead, so it's
// both faster on the happy path and robust under load. Returns once satisfied.
export async function waitForPutCountAbove(page: Page, min: number, timeout = 5000) {
  await expect
    .poll(() => getPuts(page).then((p) => p.length), { timeout })
    .toBeGreaterThan(min);
}

// settleBoundingBox waits for a node's on-screen boundingBox to stop moving:
// it reads the box twice with a short gap and only returns once two consecutive
// reads are identical (the fitView / drag-settle animation has finished). Replaces
// a fixed post-fitView sleep — it ends the instant the animation rests rather than
// over-waiting a worst-case constant. Returns the settled box.
export async function settleBoundingBox(node: Locator, timeout = 3000) {
  await awaitMeasured(node);
  const deadline = Date.now() + timeout;
  let prev = await node.boundingBox();
  while (Date.now() < deadline) {
    await node.page().waitForTimeout(50);
    const cur = await node.boundingBox();
    if (
      prev &&
      cur &&
      prev.x === cur.x &&
      prev.y === cur.y &&
      prev.width === cur.width &&
      prev.height === cur.height
    ) {
      return cur;
    }
    prev = cur;
  }
  return prev;
}

// pushSnapshot injects a new protocol state and pushes it over SSE (the test hook
// the mock server exposes — same event/refetch path as real serve).
export async function pushSnapshot(page: Page, snapshot: unknown) {
  const r = await page.request.post("/__test/snapshot", { data: snapshot });
  expect(r.ok()).toBeTruthy();
}

// connectDrag drives an xyflow connection drag from one handle to another using
// the xyflow-official Playwright pattern: native page.mouse hover → down → small
// arming move → stepped move to the target → up. React Flow v12 arms the
// connection on the handle's pointerdown and renders the in-flight
// .react-flow__connectionline once the pointer crosses RF's drag threshold; the
// edge lands on release over a valid target handle.
//
// PRECONDITION: both handles must be ON-SCREEN (inside the viewport). RF's
// connection drag is hit-tested against the live DOM, so an off-screen port can't
// be grabbed — callers fitView() after authoring new nodes so the ports are
// visible (see canvas.spec connect test).
//
// Returns whether the in-flight connection line (.react-flow__connectionline) was
// observed mid-drag. With the ports on-screen this is reliable, but it stays a
// best-effort signal; the load-bearing assertion is the resulting edge count.
export async function connectDrag(
  page: Page,
  sourceHandle: Locator,
  targetHandle: Locator,
): Promise<boolean> {
  await awaitMeasured(sourceHandle.locator("xpath=ancestor::*[contains(@class,'react-flow__node')][1]"));
  const from = await centerOf(sourceHandle);
  const to = await centerOf(targetHandle);
  const lineCount = () => page.locator(".react-flow__connectionline").count();

  await page.mouse.move(from.x, from.y);
  await page.mouse.down();
  // Small move to arm RF's connection drag (it has a 1px threshold), then step to
  // the target so intermediate connection-in-progress frames render.
  await page.mouse.move(from.x + 2, from.y + 4, { steps: 2 });
  await page.mouse.move((from.x + to.x) / 2, (from.y + to.y) / 2, { steps: 8 });
  let sawLine = (await lineCount()) > 0;
  await page.mouse.move(to.x, to.y, { steps: 8 });
  if (!sawLine) sawLine = (await lineCount()) > 0;
  await page.mouse.up();
  return sawLine;
}

// html5DragTo simulates an HTML5 drag-and-drop (the Office draft dock → desk
// gesture). Playwright's mouse drag does NOT fire the dragstart/dragover/drop
// events React's onDragStart/onDrop rely on, so we dispatch them ourselves with a
// shared DataTransfer carried through the sequence. Coordinates are taken from the
// live bounding boxes so the office's elementFromPoint drop resolution works.
export async function html5DragTo(page: Page, source: Locator, target: Locator) {
  const sb = await source.boundingBox();
  const tb = await target.boundingBox();
  if (!sb || !tb) throw new Error("missing bounding box for html5 drag");
  const sx = sb.x + sb.width / 2;
  const sy = sb.y + sb.height / 2;
  const tx = tb.x + tb.width / 2;
  const ty = tb.y + tb.height / 2;

  const sourceHandle = await source.elementHandle();
  const targetHandle = await target.elementHandle();
  if (!sourceHandle || !targetHandle) throw new Error("missing element handle");

  await page.evaluate(
    ({ srcEl, tgtEl, sx, sy, tx, ty }) => {
      const dt = new DataTransfer();
      const fire = (el: Element, type: string, cx: number, cy: number) => {
        const ev = new DragEvent(type, {
          bubbles: true,
          cancelable: true,
          composed: true,
          clientX: cx,
          clientY: cy,
          dataTransfer: dt,
        });
        el.dispatchEvent(ev);
      };
      fire(srcEl as Element, "dragstart", sx, sy);
      // The office listens for dragover/drop on the PANE and resolves the desk
      // under the pointer via document.elementFromPoint(clientX,clientY); firing
      // on the target element with the target's coords satisfies both.
      fire(tgtEl as Element, "dragover", tx, ty);
      fire(tgtEl as Element, "drop", tx, ty);
      fire(srcEl as Element, "dragend", tx, ty);
    },
    { srcEl: sourceHandle, tgtEl: targetHandle, sx, sy, tx, ty },
  );
}
