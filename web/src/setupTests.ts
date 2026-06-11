import "@testing-library/jest-dom";

// --- jsdom stubs for @xyflow/react ---------------------------------------
// React Flow measures the DOM at render time; jsdom lacks the layout APIs it
// relies on. These minimal stubs let it mount in tests without real geometry.

// ResizeObserver: React Flow's resize handling subscribes to the pane.
if (typeof globalThis.ResizeObserver === "undefined") {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
}

// DOMMatrixReadOnly: used by React Flow's transform math.
if (typeof globalThis.DOMMatrixReadOnly === "undefined") {
  globalThis.DOMMatrixReadOnly = class {
    m22 = 1;
  } as unknown as typeof DOMMatrixReadOnly;
}

// getBBox: jsdom doesn't implement the SVG text metrics API. React Flow's edge
// LABEL renderer (the wait-edge reason chips) calls getBBox on the <text>; without
// this stub the edge render throws and the labeled edge never mounts.
if (typeof (globalThis.SVGElement?.prototype as unknown as { getBBox?: unknown })?.getBBox === "undefined") {
  (globalThis.SVGElement.prototype as unknown as { getBBox: () => DOMRect }).getBBox = function () {
    return { x: 0, y: 0, width: 40, height: 12, top: 0, left: 0, right: 40, bottom: 12, toJSON: () => {} } as DOMRect;
  };
}

// getBoundingClientRect: jsdom returns all-zeros; React Flow bails when the
// pane has zero size, so report a fixed non-zero box.
if (!(globalThis as { __rfRectPatched?: boolean }).__rfRectPatched) {
  (globalThis as { __rfRectPatched?: boolean }).__rfRectPatched = true;
  Element.prototype.getBoundingClientRect = function () {
    return {
      x: 0, y: 0, top: 0, left: 0, right: 800, bottom: 600,
      width: 800, height: 600, toJSON: () => {},
    } as DOMRect;
  };
}
