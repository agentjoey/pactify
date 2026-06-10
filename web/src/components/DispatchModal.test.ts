import { describe, it, expect } from "vitest";
import { dispatchPayload } from "./DispatchModal";
import type { Draft } from "../lib/canvas";

const draft: Draft = { id: "T7", specMd: "# do it", feature: "F1", deps: ["T1", "T2"] };

describe("dispatchPayload", () => {
  it("assembles the assign body with deps passed through verbatim", () => {
    const body = dispatchPayload(draft, "opencode", "claude-opus", "feat/x");
    expect(body).toEqual({
      task: "T7",
      feature: "F1",
      branch: "feat/x",
      owner: "opencode",
      reviewer: "claude-opus",
      spec: ".pact/tasks/T7.md",
      deps: ["T1", "T2"],
    });
  });

  it("throws on the owner==reviewer invariant", () => {
    expect(() => dispatchPayload(draft, "opencode", "opencode", "feat/x")).toThrow(
      /owner and reviewer must differ/,
    );
  });

  it("carries an empty deps array through unchanged", () => {
    const d: Draft = { ...draft, deps: [] };
    expect(dispatchPayload(d, "a", "b", "br").deps).toEqual([]);
  });
});
