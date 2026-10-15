import { describe, it, expect } from "vitest";
import { localSource } from "./source";
import { LocalServeSource } from "./datasource";

describe("source resolver", () => {
  it("provides the local serve source", () => {
    expect(localSource()).toBeInstanceOf(LocalServeSource);
  });
});
