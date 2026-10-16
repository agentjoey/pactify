import { readFileSync } from "node:fs";
import { expect, it } from "vitest";
it("preserves shared modal, spinner, status and skeleton motion after removing governance styles", () => {
  const css = readFileSync("src/index.css", "utf8");
  for (const selector of [
    ".ui-modal-overlay",
    ".ui-modal-panel",
    ".spinner-spin",
    ".status-pill-dot-live",
    ".skeleton::after",
    ".hover-lift",
    ".press",
  ])
    expect(css).toContain(selector);
  expect(css).toContain("prefers-reduced-motion");
});
