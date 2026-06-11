import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

// Read the raw CSS sources off disk (the ?raw loader is intercepted by the
// Tailwind CSS plugin and yields empty content) so the assertions track the
// actual files the build ships, not a transformed/bundled copy. vitest runs
// with cwd = web/, so resolve from there.
const tokensCss = readFileSync(resolve(process.cwd(), "src/tokens.css"), "utf8");
const indexCss = readFileSync(resolve(process.cwd(), "src/index.css"), "utf8");

describe("dashboard v2 tokens.css", () => {
  it("declares the six locked palette hexes (spec §1)", () => {
    const locked = ["#191B26", "#20222F", "#272A3A", "#2F3246", "#ECC678", "#93B4F2"];
    const lower = tokensCss.toLowerCase();
    for (const hex of locked) {
      expect(lower, `missing locked token ${hex}`).toContain(hex.toLowerCase());
    }
  });

  it("re-points the legacy role vars to the new desaturated palette", () => {
    // --role-product/-design/-dev are read via var() by canvas components; they
    // must resolve to the new role colors (directly or via indirection).
    expect(tokensCss).toMatch(/--role-product:\s*var\(--color-role-product\)/);
    expect(tokensCss).toMatch(/--role-design:\s*var\(--color-role-design\)/);
    expect(tokensCss).toMatch(/--role-dev:\s*var\(--color-role-dev\)/);
    expect(tokensCss.toLowerCase()).toContain("--color-role-product: #ecc678");
    expect(tokensCss.toLowerCase()).toContain("--color-role-design: #93b4f2");
    expect(tokensCss.toLowerCase()).toContain("--color-role-dev: #7bd8a0");
  });

  it("self-hosts the fonts with font-display: swap and no external URLs", () => {
    expect(tokensCss).toContain("@font-face");
    expect(tokensCss).toContain("font-display: swap");
    expect(tokensCss).toContain("/fonts/InterVariable.woff2");
    expect(tokensCss).toContain("/fonts/JetBrainsMono-Regular.woff2");
    expect(tokensCss).toContain("/fonts/JetBrainsMono-Medium.woff2");
    expect(tokensCss).not.toMatch(/src:\s*url\(["']?https?:/i);
  });
});

describe("legacy palette fully superseded in dashboard CSS", () => {
  it("contains no occurrence of the old role hexes in tokens.css or index.css", () => {
    for (const old of ["ffd479", "8ab4ff", "6ee7a0"]) {
      expect(tokensCss.toLowerCase(), `old hex ${old} lingers in tokens.css`).not.toContain(old);
      expect(indexCss.toLowerCase(), `old hex ${old} lingers in index.css`).not.toContain(old);
    }
  });

  it("retires the old #0d1117 dashboard background in favor of the page token", () => {
    expect(indexCss.toLowerCase()).not.toContain("0d1117");
    expect(indexCss).toContain("var(--color-bg-page)");
    expect(indexCss).toContain('@import "./tokens.css"');
  });
});
