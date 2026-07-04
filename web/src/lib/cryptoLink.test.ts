import { describe, it, expect } from "vitest";
import { deriveProjectKey } from "@pactify-apps/crypto";

// Proves the crypto package (and its @noble/@scure transitive deps) bundles and
// runs in the dashboard's browser-target build through the workspace alias — the
// genuine technical risk for P3 RelaySource, which must derive the project key
// to decrypt relay event bodies client-side. Asserting the cross-language golden
// vector (spec §4.2b/§8) also confirms Go↔TS parity survives the bundling.
describe("cloud package link: @pactify-apps/crypto", () => {
  it("deriveProjectKey matches the golden vector through the alias", () => {
    const master = Uint8Array.from({ length: 32 }, (_, i) => i); // 00..1f
    const key = deriveProjectKey(master, "acct1:pactify");
    const hex = [...key].map((b) => b.toString(16).padStart(2, "0")).join("");
    expect(hex).toBe("cb1824a13ab023fe2af7238df9dd1e2a5d53a9abf01d1bf446b4725840ddfdd7");
  });
});
