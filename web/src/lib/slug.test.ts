import { describe, it, expect } from "vitest";
import { slugify } from "./slug";

describe("slugify", () => {
  it("lowercases and dashes a goal into a valid feature slug", () => {
    expect(slugify("Add 2FA Login")).toBe("add-2fa-login");
  });
  it("collapses runs and trims leading/trailing dashes", () => {
    expect(slugify("  --Hello,  World!!  ")).toBe("hello-world");
  });
  it("caps length at 40 chars", () => {
    expect(slugify("a".repeat(60)).length).toBe(40);
  });
  it("returns a slug matching ^[a-z0-9][a-z0-9-]*$ or empty", () => {
    const s = slugify("***");
    expect(s === "" || /^[a-z0-9][a-z0-9-]*$/.test(s)).toBe(true);
  });
});
