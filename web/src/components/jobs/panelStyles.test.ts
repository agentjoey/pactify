import { execFile } from "node:child_process";
import { mkdtemp, readFile, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { promisify } from "node:util";
import { expect, it } from "vitest";

it("includes the shared Jobs layout utilities in the standalone panel build", async () => {
  // A separate process exercises panel's own source scan, without inheriting
  // the dashboard/Vitest Tailwind scanner or changing the tracked dist.
  const output = await mkdtemp(join(tmpdir(), "pactify-panel-css-"));
  try {
    await promisify(execFile)(process.execPath, [
      resolve(process.cwd(), "node_modules/vite/bin/vite.js"), "build",
      "--config", resolve(process.cwd(), "vite.panel.config.ts"), "--outDir", output,
    ], {env: {...process.env, NODE_ENV: "production"}});
    const assets = join(output, "assets");
    const names = (await readdir(assets)).filter(name => name.endsWith(".css"));
    const css = (await Promise.all(names.map(name => readFile(join(assets, name), "utf8")))).join("\n");
    for (const [className, property] of [
      ["absolute", "position:absolute"],
      ["flex", "display:flex"],
      ["overflow-hidden", "overflow:hidden"],
    ]) {
      expect(css).toMatch(new RegExp(`\\.${className}\\s*\\{[^}]*${property}`));
    }
    expect(css).toMatch(/\.detail-scroll\s*\{[^}]*overflow-y:auto/);
    expect(css).toMatch(/\.detail-scroll\s*\{[^}]*min-height:0/);
    expect(css).toMatch(/\.jobs-drawer\s*\{[^}]*height:100vh/);
    expect(css).toMatch(/\.jobs-drawer\s*\{[^}]*height:100dvh/);
    expect(css).toMatch(/\.jobs-drawer\s*\{[^}]*max-height:100vh/);
    expect(css).toMatch(/\.jobs-drawer\s*\{[^}]*max-height:100dvh/);
    const scripts = (await readdir(assets)).filter(name => name.endsWith(".js"));
    const js = (await Promise.all(scripts.map(name => readFile(join(assets, name), "utf8")))).join("\n");
    for (const text of ["Instructions for the next run", "Confirm Discard", "Add alias", "Add quota group", "Save defaults", "Duplicate alias name"]) {
      expect(js.includes(text), `write-only code in panel: ${text}`).toBe(false);
    }
    expect(/method:\s*["'`](?:POST|PUT|DELETE)["'`]/.test(js)).toBe(false);
  } finally {
    await rm(output, {recursive: true, force: true});
  }
}, 30000);
