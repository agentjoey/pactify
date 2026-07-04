import { describe, it, expect } from "vitest";
import { hexToBytes, isHostedMode, relayUrl, localSource, connectRelaySource } from "./source";
import { LocalServeSource } from "./datasource";

describe("source resolver", () => {
  it("defaults to LOCAL mode when no relay URL is configured (test env)", () => {
    // No VITE_PACTIFY_RELAY_URL in the test env → local build.
    expect(isHostedMode()).toBe(false);
    expect(relayUrl()).toBe("");
    expect(localSource()).toBeInstanceOf(LocalServeSource);
  });

  it("connectRelaySource refuses in local mode", async () => {
    await expect(connectRelaySource("00")).rejects.toThrow(/no relay configured/);
  });

  describe("hexToBytes", () => {
    it("decodes a valid hex string", () => {
      expect(Array.from(hexToBytes("00ff10"))).toEqual([0, 255, 16]);
    });
    it("trims and lowercases", () => {
      expect(Array.from(hexToBytes("  AABB \n"))).toEqual([170, 187]);
    });
    it("rejects odd length", () => {
      expect(() => hexToBytes("abc")).toThrow(/even-length hex/);
    });
    it("rejects non-hex chars", () => {
      expect(() => hexToBytes("zz")).toThrow(/hex/);
    });
    it("rejects empty", () => {
      expect(() => hexToBytes("")).toThrow(/hex/);
    });
  });
});
