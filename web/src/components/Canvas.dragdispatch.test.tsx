import { render, screen, act } from "@testing-library/react";
import { useState } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import type { State } from "../lib/types";
import type { Draft, DraftFeature } from "../lib/canvas";
import type { Node } from "@xyflow/react";

// Integration seam for the multi-select drag-dispatch guard (review fix #2).
//
// A real React Flow pointer drag can't run under jsdom (d3-drag dereferences a
// null document on a synthetic mousedown — spec §5). To prove the PERSISTENCE
// half of the guard — a multi-select group drag (draft + task) over a seat must
// NOT open the DispatchModal and MUST write BOTH nodes' layout entries — we mock
// @xyflow/react with a thin stub that captures the `onNodeDragStop` prop Canvas
// passes, then invoke it with React Flow's documented 3-arg shape
// (event, primaryNode, draggedSet).

const putLayout = vi.fn().mockResolvedValue(undefined);
const getLayout = vi.fn().mockResolvedValue({});
vi.mock("../lib/api", () => ({
  getLayout: (...args: unknown[]) => getLayout(...args),
  putLayout: (...args: unknown[]) => putLayout(...args),
  getActingSeat: vi.fn().mockResolvedValue({ seat: "" }),
}));

// Capture the handlers Canvas hands to ReactFlow.
const captured: {
  onNodeDragStop?: (e: unknown, n: Node, dragged: Node[]) => void;
  nodes?: Node[];
} = {};

vi.mock("@xyflow/react", () => {
  return {
    // ReactFlow stub: records the props we need and renders nothing heavy.
    ReactFlow: (props: {
      nodes: Node[];
      onNodeDragStop?: (e: unknown, n: Node, d: Node[]) => void;
      children?: React.ReactNode;
    }) => {
      captured.onNodeDragStop = props.onNodeDragStop;
      captured.nodes = props.nodes;
      return null;
    },
    // Hooks used by FitOnEntry / SnapGuides — children are not rendered by the
    // stub above, so these are never actually called, but they must exist for
    // the module's import graph.
    useReactFlow: () => ({ fitView: () => {} }),
    useViewport: () => ({ x: 0, y: 0, zoom: 1 }),
    applyNodeChanges: (_c: unknown, n: Node[]) => n,
  };
});

import { Canvas } from "./Canvas";

const fixture: State = {
  project: "demo",
  awaiting_count: 0,
  agents: [
    { id: "alice", roles: ["orchestrator"] },
    { id: "bob", roles: ["worker"] },
  ],
  features: [
    {
      id: "F1",
      branch: "feat/f1",
      status: "active",
      tasks: [
        { id: "T1", owner: "bob", status: "accepted", reviewer: "alice", spec: "", evidence: "" },
        { id: "T2", owner: "bob", status: "assigned", reviewer: "alice", spec: "", evidence: "", deps: ["T1"] },
      ],
    },
  ],
};

describe("Canvas multi-select drag dispatch guard (integration)", () => {
  beforeEach(() => {
    putLayout.mockClear();
    getLayout.mockReset();
    getLayout.mockResolvedValue({});
    captured.onNodeDragStop = undefined;
    captured.nodes = undefined;
  });

  it("group drag (draft over seat + task): no DispatchModal, both nodes' positions PUT", async () => {
    vi.useFakeTimers();
    try {
      render(
        <Canvas
          project="demo"
          state={fixture}
          author
          initialMode="plan"
          drafts={[{ id: "d1", specMd: "# d", feature: "F1", deps: [] }]}
          setDrafts={() => {}}
          draftFeatures={[] as DraftFeature[]}
          setDraftFeatures={() => {}}
        />,
      );

      // Wait for materialization to populate the node array Canvas hands the stub.
      await vi.waitFor(() => {
        expect(captured.onNodeDragStop).toBeDefined();
        expect(captured.nodes?.some((n) => n.id === "draft:d1")).toBe(true);
        expect(captured.nodes?.some((n) => n.id === "seat:bob")).toBe(true);
      });
      await vi.advanceTimersByTimeAsync(900); // flush materialize PUT
      putLayout.mockClear();

      // Place the dragged draft so its ABSOLUTE box lands on bob's seat (the
      // draft is a child of feature:F1, so nodeBounds adds the parent's absolute
      // position — set the parent-relative position to (seat - parent) so the
      // two boxes overlap). A single-draft drag here WOULD dispatch, proving the
      // guard is what suppresses the multi-node case.
      const seat = captured.nodes!.find((n) => n.id === "seat:bob")!;
      const parent = captured.nodes!.find((n) => n.id === "feature:F1")!;
      const onSeatRel = {
        x: seat.position.x - parent.position.x,
        y: seat.position.y - parent.position.y,
      };
      const draggedDraft: Node = {
        ...captured.nodes!.find((n) => n.id === "draft:d1")!,
        position: { ...onSeatRel },
      };
      // A second dragged node (the committed task), moved elsewhere.
      const draggedTask: Node = {
        ...captured.nodes!.find((n) => n.id === "task:T2")!,
        position: { x: 16, y: 999 },
      };

      // React Flow v12: (event, primaryNode, draggedSet). Primary = the draft.
      captured.onNodeDragStop!(null, draggedDraft, [draggedDraft, draggedTask]);

      // Guard held: NO DispatchModal opened for a multi-node group drag.
      expect(screen.queryByTestId("dispatch-modal")).toBeNull();

      // Both dragged nodes' positions were persisted in the PUT body.
      await vi.advanceTimersByTimeAsync(900); // flush the drag-save debounce
      expect(putLayout).toHaveBeenCalled();
      const body = putLayout.mock.calls.at(-1)![1] as {
        positions: Record<string, { x: number; y: number }>;
      };
      // onNodeDragStop writes each node's RF-native position verbatim — the
      // draft's parent-relative onSeatRel and the task's moved coords.
      expect(body.positions["draft:d1"]).toEqual(onSeatRel);
      expect(body.positions["task:T2"]).toEqual({ x: 16, y: 999 });
    } finally {
      vi.useRealTimers();
    }
  });

  it("single draft over a seat: DispatchModal opens (no position PUT for the gesture)", async () => {
    vi.useFakeTimers();
    try {
      render(
        <Canvas
          project="demo"
          state={fixture}
          author
          initialMode="plan"
          drafts={[{ id: "d1", specMd: "# d", feature: "F1", deps: [] }]}
          setDrafts={() => {}}
          draftFeatures={[] as DraftFeature[]}
          setDraftFeatures={() => {}}
        />,
      );
      await vi.waitFor(() => {
        expect(captured.onNodeDragStop).toBeDefined();
        expect(captured.nodes?.some((n) => n.id === "seat:bob")).toBe(true);
      });
      await vi.advanceTimersByTimeAsync(900);

      const seat = captured.nodes!.find((n) => n.id === "seat:bob")!;
      const parent = captured.nodes!.find((n) => n.id === "feature:F1")!;
      const draggedDraft: Node = {
        ...captured.nodes!.find((n) => n.id === "draft:d1")!,
        // Parent-relative position whose absolute box lands on bob's seat.
        position: {
          x: seat.position.x - parent.position.x,
          y: seat.position.y - parent.position.y,
        },
      };
      // Single-node drag (length 1) → dispatch gesture fires.
      act(() => {
        captured.onNodeDragStop!(null, draggedDraft, [draggedDraft]);
      });

      // vi.waitFor advances fake timers (RTL's findBy polls on real timers and
      // would deadlock here). setDispatch is synchronous so the modal appears on
      // the next render.
      const modal = await vi.waitFor(() => screen.getByTestId("dispatch-modal"));
      expect(modal).toBeInTheDocument();
      expect(modal.textContent).toContain("bob");
    } finally {
      vi.useRealTimers();
    }
  });

  // Review fix #3: openDispatchFor must close over the CURRENT drafts list. The
  // draft node's injected onDispatch is captured here; after a DIFFERENT draft is
  // deleted, invoking the surviving draft's onDispatch must still resolve it from
  // the fresh drafts list (a stale closure would search the pre-delete list).
  it("draft onDispatch stays fresh after another draft is deleted", async () => {
    function Harness() {
      const [drafts, setDrafts] = useState<Draft[]>([
        { id: "d1", specMd: "# d1", feature: "F1", deps: [] },
        { id: "d2", specMd: "# d2", feature: "F1", deps: [] },
      ]);
      return (
        <div>
          <button onClick={() => setDrafts((ds) => ds.filter((d) => d.id !== "d1"))}>
            del-d1
          </button>
          <Canvas
            project="demo"
            state={fixture}
            author
            initialMode="plan"
            drafts={drafts}
            setDrafts={setDrafts}
            draftFeatures={[] as DraftFeature[]}
            setDraftFeatures={() => {}}
          />
        </div>
      );
    }

    const findOnDispatch = (id: string) =>
      (captured.nodes!.find((n) => n.id === id)?.data as { onDispatch?: () => void } | undefined)
        ?.onDispatch;

    render(<Harness />);
    await waitForTruthy(() => !!findOnDispatch("draft:d2"));
    const before = findOnDispatch("draft:d2")!;

    // Delete d1. openDispatchFor (useCallback over [replaying, drafts]) gets a new
    // identity when drafts changes, and injectCallbacks (which lists it as a dep)
    // re-mints draft:d2's data.onDispatch — so the captured callback is FRESH,
    // closing over the post-delete drafts list rather than the stale one.
    act(() => {
      screen.getByText("del-d1").click();
    });
    await waitForTruthy(() => captured.nodes!.every((n) => n.id !== "draft:d1"));
    const after = findOnDispatch("draft:d2")!;

    // Freshness mechanism: the surviving draft's onDispatch was re-minted (new
    // reference) after the drafts list changed — this is what stops it pointing
    // at a stale closure.
    expect(after).not.toBe(before);

    // And it still resolves d2 from the current list → modal opens with d2.
    act(() => after());
    const modal = await screen.findByTestId("dispatch-modal");
    expect(modal.textContent).toContain("d2");
  });
});

// Tiny polling helper (no fake timers in this test): waits for a predicate.
async function waitForTruthy(pred: () => boolean, tries = 50): Promise<void> {
  for (let i = 0; i < tries; i++) {
    if (pred()) return;
    await new Promise((r) => setTimeout(r, 5));
  }
  throw new Error("waitForTruthy timed out");
}
