import "@testing-library/jest-dom";

// --- jsdom stubs ----------------------------------------------------------
// jsdom lacks several layout/measurement APIs that components (and libs like
// cmdk) touch at render time. Minimal stubs keep mounts geometry-free.

// ResizeObserver: layout-aware components subscribe to their pane.
if (typeof globalThis.ResizeObserver === "undefined") {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
}

// scrollIntoView: jsdom doesn't implement it. cmdk (the ⌘K palette) calls it to
// keep the active item in view on selection-change; without the stub mounting the
// palette throws.
if (typeof Element.prototype.scrollIntoView === "undefined") {
  Element.prototype.scrollIntoView = function () {};
}

// getBoundingClientRect: jsdom returns all-zeros; size-aware components bail
// on a zero-size pane, so report a fixed non-zero box.
if (!(globalThis as { __rfRectPatched?: boolean }).__rfRectPatched) {
  (globalThis as { __rfRectPatched?: boolean }).__rfRectPatched = true;
  Element.prototype.getBoundingClientRect = function () {
    return {
      x: 0, y: 0, top: 0, left: 0, right: 800, bottom: 600,
      width: 800, height: 600, toJSON: () => {},
    } as DOMRect;
  };
}
