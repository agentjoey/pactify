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
  it("declares the neutral dark palette", () => {
    // Neutral surfaces retain the semantic dark role colors.
    const palette = ["#0b0b0c", "#18181a", "#1f1f22", "#ffd479", "#8ab4ff", "#6ee7a0"];
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
    expect(tokensCss).toContain("/fonts/SpaceGrotesk-Latin.woff2");
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

const luminance = (hex: string) => hex.slice(1).match(/../g)!.map(v=>parseInt(v,16)/255).map(v=>v<=.04045?v/12.92:((v+.055)/1.055)**2.4).reduce((n,v,i)=>n+v*[.2126,.7152,.0722][i],0);
const ratio=(a:string,b:string)=>{const [hi,lo]=[luminance(a),luminance(b)].sort((x,y)=>y-x);return (hi+.05)/(lo+.05);};
it("keeps content text tokens AA on all surfaces in both themes", () => {
  const blocks=[tokensCss.match(/:root\s*\{([\s\S]*?)\}/)![1],tokensCss.match(/:root\[data-theme="light"\]\s*\{([\s\S]*?)\}/)![1]];
  for(const block of blocks){
    const values=Object.fromEntries([...block.matchAll(/(--[\w-]+):\s*(#[\da-f]{6})/g)].map(m=>[m[1],m[2]]));
    // text-4 is a decorative divider/disabled token, never used for content.
    for(const text of ["--text","--text-2","--text-3"])
      for(const bg of ["--bg","--bg-panel","--bg-elev","--bg-elev2","--bg-input"])
        expect(ratio(values[text],values[bg]),`${text} on ${bg}`).toBeGreaterThanOrEqual(4.5);
    expect(ratio(values['--accent-ink'],values['--accent'])).toBeGreaterThanOrEqual(4.5);
  }
});
it("contains no gradients in any shared CSS or TSX", async () => {
  const {readdirSync}=await import("node:fs");
  function check(dir:string){for(const file of readdirSync(dir,{withFileTypes:true})){const path=resolve(dir,file.name);if(file.isDirectory())check(path);else if(/\.(css|tsx)$/.test(file.name))expect(readFileSync(path,"utf8"),path).not.toContain("gradient(");}}
  check(resolve(process.cwd(),"src"));
  check(resolve(process.cwd(),"panel"));
});

it("defines the terminal palette, statuses and stable chart series in both themes", () => {
  const blocks = [tokensCss.match(/:root\s*\{([\s\S]*?)\}/)![1], tokensCss.match(/:root\[data-theme="light"\]\s*\{([\s\S]*?)\}/)![1]];
  const keys = ["--bg", "--bg-panel", "--bg-elev", "--bg-elev2", "--bg-input", "--bg-code", "--border", "--border-2", "--text", "--text-2", "--text-3", "--text-4", "--accent", "--accent-ink", "--accent-2", "--accent-line", "--status-running", "--status-attention", "--status-attention-ink", "--status-success", "--status-success-ink", "--status-failure-line", "--chart-codex", "--chart-kimi", "--chart-opencode"];
  const palettes = [
    ["#0b0b0c", "#111112", "#18181a", "#1f1f22", "#141416", "#050505", "#2a2a2e", "#1f1f22", "#ededee", "#c9c9cd", "#9a9aa1", "#5e5e65", "#54d398", "#04140b", "#0f261b", "#236b49", "#8ab4ff", "#f5c451", "#1a1302", "#6ee7a0", "#04140b", "#f97066", "#8ab4ff", "#6ee7a0", "#f5c451"],
    ["#f6f5f1", "#fafaf7", "#ffffff", "#eceae4", "#fafaf7", "#fafaf7", "#dedcd5", "#e2e0d9", "#141413", "#5c5b56", "#6b6963", "#a8a69f", "#141413", "#f6f5f1", "#eceae4", "#dedcd5", "#141413", "#c2410c", "#ffffff", "#141413", "#f6f5f1", "#c2410c", "#141413", "#8a8880", "#c9c7bf"],
  ];
  blocks.forEach((block, theme) => {
    const values = Object.fromEntries([...block.matchAll(/(--[\w-]+):\s*(#[\da-f]{6})/g)].map(m => [m[1], m[2]]));
    keys.forEach((key, i) => expect(values[key], `theme ${theme}: ${key}`).toBe(palettes[theme][i]));
    for (const bg of ["--bg", "--bg-elev"]) expect(ratio(values["--text-3"], values[bg]), `text-3 on ${bg}`).toBeGreaterThanOrEqual(4.5);
    for (const [ink, fill] of [["--status-attention-ink", "--status-attention"], ["--status-success-ink", "--status-success"]]) expect(ratio(values[ink], values[fill])).toBeGreaterThanOrEqual(4.5);
  });
});
