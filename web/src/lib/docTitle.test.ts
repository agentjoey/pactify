import { describe, it, expect } from "vitest";
import { docTitle } from "./docTitle";

// docTitle drives document.title (spec §6.5): «project» guillemets, an
// "N awaiting ●" suffix when any task awaits review, and the bare product name
// when no project is selected.
describe("docTitle", () => {
  it("project + awaiting → «p» · N awaiting ●", () => {
    expect(docTitle("greet", 2)).toBe("«greet» · 2 awaiting ●");
    expect(docTitle("greet", 1)).toBe("«greet» · 1 awaiting ●");
  });
  it("project, nothing awaiting → bare «p»", () => {
    expect(docTitle("greet", 0)).toBe("«greet»");
  });
  it("no project → product name", () => {
    expect(docTitle("", 0)).toBe("pactify");
    expect(docTitle("", 5)).toBe("pactify");
  });
});
