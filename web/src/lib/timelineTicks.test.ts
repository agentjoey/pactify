import { describe, it, expect } from "vitest";
import { tickColorVar } from "./timelineTicks";

describe("tickColorVar", () => {
  it("maps init and assign to the warn (amber) token", () => {
    expect(tickColorVar("init")).toBe("var(--color-warn)");
    expect(tickColorVar("assign")).toBe("var(--color-warn)");
  });

  it("maps join, checkpoint, accept to the success (green) token", () => {
    expect(tickColorVar("join")).toBe("var(--color-success)");
    expect(tickColorVar("checkpoint")).toBe("var(--color-success)");
    expect(tickColorVar("accept")).toBe("var(--color-success)");
  });

  it("maps changes (and the engine's changes_requested / awaiting_review) to danger/role-design per spec", () => {
    expect(tickColorVar("changes")).toBe("var(--color-danger)");
    expect(tickColorVar("changes_requested")).toBe("var(--color-danger)");
  });

  it("maps merge to the role-design (blue) token", () => {
    expect(tickColorVar("merge")).toBe("var(--color-role-design)");
  });

  it("maps awaiting_review (review-flow) to the role-design blue token", () => {
    expect(tickColorVar("awaiting_review")).toBe("var(--color-role-design)");
  });

  it("falls back to the gray text-3 token for unknown types", () => {
    expect(tickColorVar("nudge")).toBe("var(--color-text-3)");
    expect(tickColorVar("")).toBe("var(--color-text-3)");
    expect(tickColorVar("whatever")).toBe("var(--color-text-3)");
  });

  it("is case-insensitive", () => {
    expect(tickColorVar("INIT")).toBe("var(--color-warn)");
    expect(tickColorVar("Accept")).toBe("var(--color-success)");
  });
});
