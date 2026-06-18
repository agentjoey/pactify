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

// switchToOffice presses "2" to reach the Canvas view (which defaults to
// office mode), then waits for the office surface to mount.
export async function switchToOffice(page: Page) {
  await page.keyboard.press("2");
  await page.locator('[data-testid="office-view"]').waitFor();
}

// centerOf returns the viewport-space center of a locator's bounding box.
export async function centerOf(loc: Locator): Promise<{ x: number; y: number }> {
  const b = await loc.boundingBox();
  if (!b) throw new Error("no bounding box for locator");
  return { x: b.x + b.width / 2, y: b.y + b.height / 2 };
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
