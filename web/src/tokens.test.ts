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
  it("declares the dark-theme palette hexes (2026-06-19 handoff)", () => {
    // Dark theme: page near-black void, cards deep slate, role hues are the
    // original brand hexes tuned for dark surfaces.
    const palette = ["#0a0e14", "#10151e", "#171e2a", "#ffd479", "#8ab4ff", "#6ee7a0"];
    const lower = tokensCss.toLowerCase();
    for (const hex of palette) {
      expect(lower, `missing dark token ${hex}`).toContain(hex.toLowerCase());
    }
  });

  it("re-points the legacy role vars to the dark role palette", () => {
    // --role-product/-design/-dev are read via var() by canvas components; they
    // must resolve to the role colors (directly or via indirection).
    expect(tokensCss).toMatch(/--role-product:\s*var\(--color-role-product\)/);
    expect(tokensCss).toMatch(/--role-design:\s*var\(--color-role-design\)/);
    expect(tokensCss).toMatch(/--role-dev:\s*var\(--color-role-dev\)/);
    expect(tokensCss.toLowerCase()).toMatch(/--color-role-product:\s*#ffd479/);
    expect(tokensCss.toLowerCase()).toMatch(/--color-role-design:\s*#8ab4ff/);
    expect(tokensCss.toLowerCase()).toMatch(/--color-role-dev:\s*#6ee7a0/);
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
  it("contains no occurrence of the old light role hexes in tokens.css or index.css", () => {
    // The previous light-theme role colors (tuned for white) should be gone.
    for (const old of ["f2b50a", "2563eb", "16a85a"]) {
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
