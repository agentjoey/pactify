import { render } from "@testing-library/react";
import { ReactFlow, ReactFlowProvider, Position, type Edge, type Node } from "@xyflow/react";
import { describe, it, expect, vi, afterEach } from "vitest";
import { AntEdge } from "./AntEdge";
import { assignAntFlags, ANT_CAP } from "../../../lib/canvas";

const edgeTypes = { ant: AntEdge };

// Seed measured dims + handle bounds so React Flow can resolve edge endpoints
// without a real layout pass (jsdom never measures) — same trick Canvas uses.
const nodes: Node[] = [
  {
    id: "a",
    position: { x: 0, y: 0 },
    data: {},
    measured: { width: 100, height: 40 },
    handles: [{ type: "source", position: Position.Bottom, x: 50, y: 40, width: 1, height: 1 }],
  },
  {
    id: "b",
    position: { x: 0, y: 200 },
    data: {},
    measured: { width: 100, height: 40 },
    handles: [{ type: "target", position: Position.Top, x: 50, y: 0, width: 1, height: 1 }],
  },
];

function renderEdge(edge: Edge) {
  return render(
    <ReactFlowProvider>
      <div style={{ width: 400, height: 400 }}>
        <ReactFlow nodes={nodes} edges={[edge]} edgeTypes={edgeTypes} fitView />
      </div>
    </ReactFlowProvider>,
  );
}

// stubMatchMedia forces a reduced-motion answer for the duration of a test.
function stubMatchMedia(reduce: boolean) {
  vi.stubGlobal(
    "matchMedia",
    (q: string) => ({
      matches: reduce && q.includes("reduce"),
      media: q,
      addEventListener: () => {},
      removeEventListener: () => {},
    }),
  );
}

describe("AntEdge", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("renders an animateMotion ant when data.ant && motion allowed", () => {
    stubMatchMedia(false);
    const { container } = renderEdge({
      id: "e1",
      source: "a",
      target: "b",
      type: "ant",
      data: { kind: "wait", ant: true, color: "#93B4F2" },
    });
    expect(container.querySelector("animateMotion")).not.toBeNull();
    // mpath must reference the base path's unique id so the ant crawls the edge.
    const mpath = container.querySelector("mpath");
    expect(mpath?.getAttribute("href")).toBe("#e1-path");
    // BaseEdge renders the referenced path id.
    expect(container.querySelector("#e1-path")).not.toBeNull();
  });

  it("renders NO ant when data.ant is false", () => {
    stubMatchMedia(false);
    const { container } = renderEdge({
      id: "e2",
      source: "a",
      target: "b",
      type: "ant",
      data: { kind: "dep", ant: false },
    });
    expect(container.querySelector("animateMotion")).toBeNull();
    // base path still drawn.
    expect(container.querySelector("#e2-path")).not.toBeNull();
  });

  it("renders NO ant under prefers-reduced-motion even when data.ant is true", () => {
    stubMatchMedia(true);
    const { container } = renderEdge({
      id: "e3",
      source: "a",
      target: "b",
      type: "ant",
      data: { kind: "dep", ant: true },
    });
    expect(container.querySelector("animateMotion")).toBeNull();
  });

  it("carrier ant (dep) carries a cargo cube; messenger (wait) does not", () => {
    stubMatchMedia(false);
    const dep = renderEdge({
      id: "ed",
      source: "a",
      target: "b",
      type: "ant",
      data: { kind: "dep", ant: true, color: "#D9A23D" },
    });
    expect(dep.container.querySelector('[data-testid="ant-ed"] rect')).not.toBeNull();
    dep.unmount();

    const wait = renderEdge({
      id: "ew",
      source: "a",
      target: "b",
      type: "ant",
      data: { kind: "wait", ant: true, color: "#93B4F2" },
    });
    expect(wait.container.querySelector('[data-testid="ant-ew"] rect')).toBeNull();
  });
});

describe("assignAntFlags (cap + priority)", () => {
  it("gives ants to the first ANT_CAP eligible edges", () => {
    const edges = Array.from({ length: ANT_CAP + 3 }, (_, i) => ({
      id: `e${i}`,
      kind: "dep" as const,
    }));
    const flags = assignAntFlags(edges);
    expect(flags.size).toBe(ANT_CAP);
    expect(flags.has("e0")).toBe(true);
    expect(flags.has(`e${ANT_CAP}`)).toBe(false);
  });

  it("prioritizes wait edges over dep edges within the cap", () => {
    // 6 dep edges first, then 1 wait edge → the wait edge must still win a slot
    // (it jumps ahead), and the last dep edge loses its slot.
    const deps = Array.from({ length: ANT_CAP }, (_, i) => ({
      id: `d${i}`,
      kind: "dep" as const,
    }));
    const edges = [...deps, { id: "w0", kind: "wait" as const }];
    const flags = assignAntFlags(edges);
    expect(flags.has("w0")).toBe(true);
    expect(flags.size).toBe(ANT_CAP);
    expect(flags.has(`d${ANT_CAP - 1}`)).toBe(false); // bumped out by the wait edge
  });

  it("returns an empty set under reduced motion (no ant anywhere)", () => {
    const edges = [{ id: "e0", kind: "wait" as const }];
    expect(assignAntFlags(edges, true).size).toBe(0);
  });
});
