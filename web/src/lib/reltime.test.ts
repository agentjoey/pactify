import { describe, it, expect } from "vitest";
import { relTime } from "./reltime";

// relTime renders a compact age WITHOUT the trailing " ago" (card chips are tight):
// <60s "now", <60m "Nm", <24h "Nh", else "Nd". now is injectable for determinism.
describe("relTime", () => {
  const now = Date.parse("2026-06-11T12:00:00Z");
  const ago = (ms: number) => new Date(now - ms).toISOString();

  it("<60s → now", () => {
    expect(relTime(ago(0), now)).toBe("now");
    expect(relTime(ago(59_000), now)).toBe("now");
  });
  it("minutes → Nm", () => {
    expect(relTime(ago(60_000), now)).toBe("1m");
    expect(relTime(ago(32 * 60_000), now)).toBe("32m");
  });
  it("hours → Nh", () => {
    expect(relTime(ago(60 * 60_000), now)).toBe("1h");
    expect(relTime(ago(5 * 60 * 60_000), now)).toBe("5h");
  });
  it("days → Nd", () => {
    expect(relTime(ago(24 * 60 * 60_000), now)).toBe("1d");
    expect(relTime(ago(3 * 24 * 60 * 60_000), now)).toBe("3d");
  });
  it("empty/unparseable → empty string", () => {
    expect(relTime("", now)).toBe("");
    expect(relTime("not-a-date", now)).toBe("");
  });
});
