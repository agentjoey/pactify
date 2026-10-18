import { readFileSync } from "node:fs";
import { expect, it } from "vitest";
it("preserves shared modal and spinner motion after removing unused styles", () => {
  const css = readFileSync("src/index.css", "utf8");
  for (const selector of [
    ".ui-modal-overlay",
    ".ui-modal-panel",
    ".spinner-spin",
    ".hover-lift",
    ".press",
  ])
    expect(css).toContain(selector);
  expect(css).toContain("prefers-reduced-motion");
});
