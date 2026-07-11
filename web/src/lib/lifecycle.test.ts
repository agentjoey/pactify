import { describe, it, expect } from "vitest";
import { lifecycleStage, statusColorVar } from "./lifecycle";

describe("lifecycleStage", () => {
  it("assigned → 0", () => expect(lifecycleStage("assigned")).toBe(0));
  it("in_progress → 1", () => expect(lifecycleStage("in_progress")).toBe(1));
  it("awaiting_review → 2", () => expect(lifecycleStage("awaiting_review")).toBe(2));
  it("changes_requested → 2", () => expect(lifecycleStage("changes_requested")).toBe(2));
  it("accepted → 3", () => expect(lifecycleStage("accepted")).toBe(3));
  it("unknown → 0", () => {
    expect(lifecycleStage("")).toBe(0);
    expect(lifecycleStage("draft")).toBe(0);
    expect(lifecycleStage("nonsense")).toBe(0);
  });
});

describe("statusColorVar", () => {
  // The medallion / wash / lifecycle bars are all driven by the --st color this
  // helper resolves. Returns a CSS color (var() reference or hex) per spec §3.
  it("assigned → neutral text-2 token", () => {
    expect(statusColorVar("assigned")).toBe("var(--color-text-2)");
  });
  it("in_progress → role-design (working blue)", () => {
    expect(statusColorVar("in_progress")).toBe("var(--color-role-design)");
  });
  it("accepted → role-dev (worker color)", () => {
    expect(statusColorVar("accepted")).toBe("var(--color-role-dev)");
  });
  it("awaiting_review → role-product (review gold)", () => {
    expect(statusColorVar("awaiting_review")).toBe("var(--color-role-product)");
  });
  it("changes_requested → danger", () => {
    expect(statusColorVar("changes_requested")).toBe("var(--color-danger)");
  });
  it("unknown → neutral text-2 token", () => {
    expect(statusColorVar("draft")).toBe("var(--color-text-2)");
    expect(statusColorVar("")).toBe("var(--color-text-2)");
  });
});
