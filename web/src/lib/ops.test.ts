import { describe, it, expect } from "vitest";
import { isSlug, relativeTime } from "./ops";

describe("isSlug", () => {
  it("accepts lowercase alnum slugs with hyphens", () => {
    expect(isSlug("claude-opus")).toBe(true);
    expect(isSlug("a")).toBe(true);
    expect(isSlug("seat1")).toBe(true);
    expect(isSlug("a1-b2-c3")).toBe(true);
  });
  it("rejects leading hyphen, uppercase, spaces, empties, symbols", () => {
    expect(isSlug("-lead")).toBe(false);
    expect(isSlug("Up")).toBe(false);
    expect(isSlug("has space")).toBe(false);
    expect(isSlug("")).toBe(false);
    expect(isSlug("under_score")).toBe(false);
    expect(isSlug("dot.dot")).toBe(false);
  });
});

describe("relativeTime", () => {
  const now = Date.parse("2026-06-11T12:00:00Z");
  it("returns 'never' for empty/absent", () => {
    expect(relativeTime(undefined, now)).toBe("never");
    expect(relativeTime("", now)).toBe("never");
  });
  it("buckets seconds, minutes, hours, days", () => {
    expect(relativeTime("2026-06-11T11:59:30Z", now)).toBe("30s ago");
    expect(relativeTime("2026-06-11T11:55:00Z", now)).toBe("5m ago");
    expect(relativeTime("2026-06-11T09:00:00Z", now)).toBe("3h ago");
    expect(relativeTime("2026-06-09T12:00:00Z", now)).toBe("2d ago");
  });
  it("returns the raw string when unparseable", () => {
    expect(relativeTime("not-a-date", now)).toBe("not-a-date");
  });
});
