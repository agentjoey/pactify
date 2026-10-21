import { afterEach, expect, it, vi } from "vitest";
import { readFileSync } from "node:fs";
import { initializeTheme, setTheme } from "./theme";
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); localStorage.clear(); delete document.documentElement.dataset.theme; });
it("defaults to dark regardless of the OS", () => {
  const media=vi.fn(); vi.stubGlobal("matchMedia",media);
  initializeTheme();
  expect(document.documentElement.dataset.theme).toBe("dark");
  expect(media).not.toHaveBeenCalled();
  for (const entry of ["panel/main.tsx", "src/main.tsx"])
    expect(readFileSync(entry,"utf8")).toContain("initializeTheme();");
});
it("persists a manual light or dark choice", () => {
  initializeTheme();setTheme("light");initializeTheme();
  expect(document.documentElement.dataset.theme).toBe("light");
  setTheme("dark");initializeTheme();
  expect(document.documentElement.dataset.theme).toBe("dark");
});
it("works when localStorage is unavailable", () => {
  vi.spyOn(Storage.prototype,"getItem").mockImplementation(()=>{throw Error("blocked");});
  vi.spyOn(Storage.prototype,"setItem").mockImplementation(()=>{throw Error("blocked");});
  expect(()=>initializeTheme()).not.toThrow();expect(()=>setTheme("light")).not.toThrow();
  expect(document.documentElement.dataset.theme).toBe("light");
});
