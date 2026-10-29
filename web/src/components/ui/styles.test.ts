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

it("keeps outlined, primary, danger, focus and disabled control styles", () => {
  const css = readFileSync("src/index.css", "utf8");
  expect(css).toMatch(/\.ui-button\s*\{[^}]*border: 1px solid var\(--button-border\)/);
  expect(css).toMatch(/\.ui-button-primary\s*\{[^}]*background: var\(--accent\);[^}]*color: var\(--accent-ink\)/);
  expect(css).toMatch(/\.ui-button-danger\s*\{[^}]*border-color: var\(--status-failure-line\);[^}]*color: var\(--err\)/);
  expect(css).toMatch(/\.ui-button:focus-visible\s*\{[^}]*outline: 2px solid var\(--accent\)/);
  expect(css).toMatch(/@media \(prefers-reduced-motion: no-preference\)\s*\{\s*\.ui-button:active:not\(:disabled\)\s*\{[^}]*transform: scale\(.97\)/);
  expect(css).toMatch(/\.ui-button:disabled\s*\{[^}]*opacity: .4;[^}]*pointer-events: none/);
});
