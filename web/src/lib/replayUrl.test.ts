import { describe, it, expect } from "vitest";
import { readAt, writeAt } from "./replayUrl";

describe("readAt", () => {
  it("returns null when ?at is absent", () => {
    expect(readAt("")).toBeNull();
    expect(readAt("?foo=1")).toBeNull();
    expect(readAt("?view=canvas&p=demo")).toBeNull();
  });

  it("reads a non-negative integer at value", () => {
    expect(readAt("?at=0")).toBe(0);
    expect(readAt("?at=5")).toBe(5);
    expect(readAt("?at=12")).toBe(12);
  });

  it("accepts a leading-? or bare search string", () => {
    expect(readAt("at=7")).toBe(7);
    expect(readAt("?at=7")).toBe(7);
  });

  it("reads at amid other params", () => {
    expect(readAt("?view=canvas&at=3&p=demo")).toBe(3);
  });

  it("returns null for malformed/negative/non-integer values", () => {
    expect(readAt("?at=")).toBeNull();
    expect(readAt("?at=abc")).toBeNull();
    expect(readAt("?at=-1")).toBeNull();
    expect(readAt("?at=2.5")).toBeNull();
    expect(readAt("?at=3x")).toBeNull();
  });
});

describe("writeAt", () => {
  it("adds at to an empty search", () => {
    expect(writeAt("", 3)).toBe("?at=3");
  });

  it("adds at while preserving other params", () => {
    expect(writeAt("?view=canvas&p=demo", 4)).toBe("?view=canvas&p=demo&at=4");
  });

  it("replaces an existing at, preserving order of other params", () => {
    expect(writeAt("?at=1&view=canvas", 9)).toBe("?at=9&view=canvas");
    expect(writeAt("?view=canvas&at=1", 9)).toBe("?view=canvas&at=9");
  });

  it("writes at=0 explicitly (0 is a valid replay position)", () => {
    expect(writeAt("?view=canvas", 0)).toBe("?view=canvas&at=0");
  });

  it("removes at when passed null, preserving other params", () => {
    expect(writeAt("?view=canvas&at=4&p=demo", null)).toBe("?view=canvas&p=demo");
    expect(writeAt("?at=4", null)).toBe("");
  });

  it("returns empty string (not '?') when clearing the only/last param", () => {
    expect(writeAt("?at=4", null)).toBe("");
    expect(writeAt("", null)).toBe("");
  });

  it("round-trips: writeAt then readAt yields the same value", () => {
    for (const n of [0, 1, 7, 42]) {
      expect(readAt(writeAt("?view=canvas&p=demo", n))).toBe(n);
    }
    expect(readAt(writeAt("?view=canvas", null))).toBeNull();
  });
});
