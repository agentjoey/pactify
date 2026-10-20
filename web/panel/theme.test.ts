import { afterEach, expect, it, vi } from "vitest";
import { readFileSync } from "node:fs";
import { initializeTheme } from "./theme";

afterEach(() => { vi.unstubAllGlobals(); delete document.documentElement.dataset.theme; });
for (const dark of [false, true]) it(`follows the OS at startup (${dark}) and on change`, () => {
  const media = new EventTarget();
  Object.assign(media, { matches: dark, addEventListener: media.addEventListener, removeEventListener: media.removeEventListener });
  const matchMedia = vi.fn(() => media);
  vi.stubGlobal("matchMedia", matchMedia);
  const stop = initializeTheme();
  expect(matchMedia).toHaveBeenCalledWith("(prefers-color-scheme: dark)");
  expect(document.documentElement.dataset.theme).toBe(dark ? "dark" : "light");
  Object.assign(media, { matches: !dark });
  media.dispatchEvent(new Event("change"));
  expect(document.documentElement.dataset.theme).toBe(dark ? "light" : "dark");
  stop();
  Object.assign(media, { matches: dark });
  media.dispatchEvent(new Event("change"));
  expect(document.documentElement.dataset.theme).toBe(dark ? "light" : "dark");
  expect(readFileSync("panel/main.tsx", "utf8")).toContain("initializeTheme();");
});
