import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { useState } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import type { State } from "../lib/types";
import type { Draft, DraftFeature } from "../lib/canvas";

// Mock the api module: getLayout resolves empty, putLayout is a spy, and
// getActingSeat is unused by Canvas but stubbed for completeness.
const putLayout = vi.fn().mockResolvedValue(undefined);
const getLayout = vi.fn().mockResolvedValue({});
vi.mock("../lib/api", () => ({
  getLayout: (...args: unknown[]) => getLayout(...args),
  putLayout: (...args: unknown[]) => putLayout(...args),
  getActingSeat: vi.fn().mockResolvedValue({ seat: "" }),
}));

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

// noopDraftProps supplies the lifted draft state Canvas now requires as props.
// Tests that don't exercise drafts pass empty lists + no-op setters.
//
// initialMode="plan": Office is the DEFAULT landing mode (T10), but every test
// in this file exercises PLAN-mode behavior (task/seat/feature nodes, dep edges,
// context menus, drafts). Forcing Plan via the prop keeps their semantics
// unchanged without re-clicking the mode segment in each test. Office-mode
// behavior is covered by OfficeView.test.tsx + the default-mode test below.
const noopDraftProps = {
  drafts: [] as Draft[],
  setDrafts: () => {},
  draftFeatures: [] as DraftFeature[],
  setDraftFeatures: () => {},
  initialMode: "plan" as const,
};

describe("Canvas", () => {
  beforeEach(() => {
    putLayout.mockClear();
    getLayout.mockReset();
    getLayout.mockResolvedValue({});
  });

  it("renders task nodes, seat rail, and the dep edge", async () => {
    const { container } = render(
      <Canvas project="demo" state={fixture} author={false} {...noopDraftProps} />,
    );

    // Task node ids appear. TaskCard renders the id in two tiers (mono id +
    // title, which falls back to the id absent a title field), so the id text
    // occurs more than once per card — assert presence via getAllByText.
    await waitFor(() => {
      expect(screen.getAllByText("T1").length).toBeGreaterThan(0);
      expect(screen.getAllByText("T2").length).toBeGreaterThan(0);
    });

    // Seat rail cards. (Scope to the seat nodes: the task cards' owner/reviewer
    // ant chips now carry the seat name as an SVG <title> too, so a bare
    // getByText collides — query within each seat node instead.)
    expect(
      container.querySelector('.react-flow__node[data-id="seat:alice"]')!.textContent,
    ).toContain("alice");
    expect(
      container.querySelector('.react-flow__node[data-id="seat:bob"]')!.textContent,
    ).toContain("bob");

    // Feature group header.
    expect(screen.getByText("F1")).toBeInTheDocument();

    // Dep edge: React Flow renders edges as DOM elements with a data-id.
    await waitFor(() => {
      expect(container.querySelector('[data-id="dep:T1→T2"]')).not.toBeNull();
    });

    // T7 re-skin chrome: the frosted toolbar, the bottom-right zoom HUD and the
    // bottom-left MiniMap all mount inside the stage.
    expect(screen.getByTestId("canvas-toolbar")).toBeInTheDocument();
    expect(screen.getByTestId("canvas-hud")).toBeInTheDocument();
    expect(container.querySelector(".react-flow__minimap")).not.toBeNull();
  });

  it("feature frame v2 shows the accepted/total progress readout", async () => {
    // F1 = T1 accepted + T2 assigned → "1/2 accepted".
    render(<Canvas project="demo" state={fixture} author={false} {...noopDraftProps} />);
    const prog = await screen.findByTestId("feature-progress");
    expect(prog.textContent).toContain("1/2 accepted");
  });

  it("replay mode is read-only: no author affordances rendered", async () => {
    // App passes author={author && !replaying}, so a replaying canvas receives
    // author=false → the build-mode toolbar (New feature / New task) is absent.
    render(<Canvas project="demo" state={fixture} author={false} replaying {...noopDraftProps} />);
    await waitFor(() => expect(screen.getAllByText("T1").length).toBeGreaterThan(0));
    // The frosted toolbar renders (it always carries the comms pill), but the
    // author build-mode affordances (Feature / Task) are absent.
    const toolbar = screen.getByTestId("canvas-toolbar");
    expect(toolbar.textContent).not.toContain("Feature");
    expect(toolbar.textContent).not.toContain("Task");
  });

  it("comms toggle (default off) merges the overlay lens + legend when turned on", async () => {
    // T2 has an unmet dep (T1 in_progress) → it is a blocked task; reviewer alice
    // reviews nothing pending and owns no in-flight work → idle seat. The overlay
    // is OFF by default and merges these markers + the legend when toggled on.
    // (Wait EDGE id/label/dashed style is asserted exhaustively in comms.test.ts;
    // here we assert the toggle wires deriveComms→mergeComms into the live graph
    // via the node-class markers, which render deterministically under jsdom.)
    const blocked: State = {
      ...fixture,
      awaiting_count: 0,
      features: [{
        ...fixture.features[0],
        tasks: [
          { id: "T1", owner: "bob", status: "in_progress", reviewer: "alice", spec: "", evidence: "" },
          { id: "T2", owner: "bob", status: "in_progress", reviewer: "alice", spec: "", evidence: "", deps: ["T1"] },
        ],
      }],
    };
    const { container } = render(<Canvas project="demo" state={blocked} author={false} {...noopDraftProps} />);
    await waitFor(() => expect(screen.getAllByText("T2").length).toBeGreaterThan(0));

    // Default OFF: no overlay markers, no legend.
    expect(container.querySelector('.react-flow__node[data-id="task:T2"]')!.className).not.toContain("comms-blocked");
    expect(screen.queryByTestId("comms-legend")).toBeNull();

    fireEvent.click(screen.getByTestId("comms-toggle"));

    await waitFor(() => {
      const t2 = container.querySelector('.react-flow__node[data-id="task:T2"]')!;
      expect(t2.className).toContain("comms-blocked");
    });
    expect(container.querySelector('.react-flow__node[data-id="seat:alice"]')!.className).toContain("comms-idle");
    expect(screen.getByTestId("comms-legend")).toBeInTheDocument();

    // Toggling back OFF removes the lens (it's a display-only overlay).
    fireEvent.click(screen.getByTestId("comms-toggle"));
    await waitFor(() => {
      expect(container.querySelector('.react-flow__node[data-id="task:T2"]')!.className).not.toContain("comms-blocked");
    });
    expect(screen.queryByTestId("comms-legend")).toBeNull();
  });

  it("pulses prop applies the pulse class to the changed task node", async () => {
    const { container } = render(
      <Canvas project="demo" state={fixture} author={false} pulses={new Set(["T2"])} {...noopDraftProps} />,
    );
    await waitFor(() => {
      const node = container.querySelector('.react-flow__node[data-id="task:T2"]');
      expect(node).not.toBeNull();
      expect(node!.className).toContain("pulse");
    });
    // T1 (not in pulses) does not pulse.
    const t1 = container.querySelector('.react-flow__node[data-id="task:T1"]')!;
    expect(t1.className).not.toContain("pulse");
  });

  it("clicking a task node reaches onSelectTask with the raw id", async () => {
    const onSelectTask = vi.fn();
    const { container } = render(
      <Canvas project="demo" state={fixture} author={false} onSelectTask={onSelectTask} {...noopDraftProps} />,
    );
    // React Flow wraps each node in a [data-id] element; T1 is a task node.
    const node = await waitFor(() => {
      const el = container.querySelector('.react-flow__node[data-id="task:T1"]');
      expect(el).not.toBeNull();
      return el as Element;
    });
    fireEvent.click(node);
    expect(onSelectTask).toHaveBeenCalledWith("T1");
  });

  // ① Acceptance-blocker regression: drafts must survive Canvas unmount/remount.
  // App lifts `drafts` above Canvas and unmounts Canvas when the view leaves
  // "canvas" (canvas→ops→canvas). The harness reproduces that lifecycle: state
  // is held ABOVE Canvas and the Canvas mount is toggled. Before the fix, drafts
  // lived in Canvas-local useState and vanished on unmount ("画布为空").
  it("drafts survive a Canvas unmount/remount cycle (lifted state)", async () => {
    function Harness() {
      const [drafts, setDrafts] = useState<Draft[]>([]);
      const [draftFeatures, setDraftFeatures] = useState<DraftFeature[]>([]);
      const [mounted, setMounted] = useState(true);
      return (
        <div>
          <button onClick={() => setMounted((m) => !m)}>toggle</button>
          {mounted && (
            <Canvas
              project="demo"
              state={fixture}
              author
              initialMode="plan"
              drafts={drafts}
              setDrafts={setDrafts}
              draftFeatures={draftFeatures}
              setDraftFeatures={setDraftFeatures}
            />
          )}
        </div>
      );
    }

    const { container } = render(<Harness />);

    // Author a draft via the real New-task editor flow.
    fireEvent.click(await screen.findByRole("button", { name: /Task/ }));
    const editor = await screen.findByTestId("task-editor");
    expect(editor).toBeInTheDocument();
    // The id field is pre-seeded (auto-id) but we set it explicitly for clarity.
    fireEvent.change(screen.getByLabelText("task id"), { target: { value: "d1" } });
    fireEvent.click(screen.getByText("Add draft"));

    // Draft node renders on the canvas.
    await waitFor(() => {
      expect(container.querySelector('.react-flow__node[data-id="draft:d1"]')).not.toBeNull();
    });

    // Switch away (unmount Canvas) then back (remount) — the App view-switch path.
    fireEvent.click(screen.getByText("toggle"));
    await waitFor(() => {
      expect(container.querySelector('[data-testid="canvas-root"]')).toBeNull();
    });
    fireEvent.click(screen.getByText("toggle"));

    // The draft is STILL there — lifted state outlived the unmount.
    await waitFor(() => {
      expect(container.querySelector('.react-flow__node[data-id="draft:d1"]')).not.toBeNull();
    });
  });

  // ③ Auto-id: the New-task editor opens with a generated default id. The
  // fixture's committed tasks are uppercase (T1/T2), which the lowercase 't'
  // series ignores, so the first suggestion is t1. Editable + slug validation
  // unchanged is covered by TaskEditor's own contract; here we only assert the
  // field is pre-filled rather than blank.
  // T8: right-click a committed task node → context menu with "View spec";
  // gated to author && !replaying.
  it("context menu opens on node right-click (author) and is hidden in replay", async () => {
    const { container, rerender } = render(
      <Canvas project="demo" state={fixture} author onSelectTask={() => {}} {...noopDraftProps} />,
    );
    const node = await waitFor(() => {
      const el = container.querySelector('.react-flow__node[data-id="task:T1"]');
      expect(el).not.toBeNull();
      return el as Element;
    });
    fireEvent.contextMenu(node);
    expect(await screen.findByTestId("canvas-ctx")).toBeInTheDocument();
    expect(screen.getByText("View spec")).toBeInTheDocument();

    // Esc closes it.
    fireEvent.keyDown(window, { key: "Escape" });
    await waitFor(() => expect(screen.queryByTestId("canvas-ctx")).toBeNull());

    // Replay (author=false) → right-click yields no menu.
    rerender(<Canvas project="demo" state={fixture} author={false} replaying {...noopDraftProps} />);
    const node2 = container.querySelector('.react-flow__node[data-id="task:T1"]')!;
    fireEvent.contextMenu(node2);
    expect(screen.queryByTestId("canvas-ctx")).toBeNull();
  });


  // T8 quality fix: React Flow's built-in Backspace delete is disabled
  // (deleteKeyCode={null}) — selecting a COMMITTED node and pressing Backspace
  // must not remove it (deletion is exclusively the draft-only path).
  it("Backspace on a selected committed node does not remove it", async () => {
    const { container } = render(
      <Canvas project="demo" state={fixture} author onSelectTask={() => {}} {...noopDraftProps} />,
    );
    const node = await waitFor(() => {
      const el = container.querySelector('.react-flow__node[data-id="task:T1"]');
      expect(el).not.toBeNull();
      return el as Element;
    });
    fireEvent.click(node); // select it
    fireEvent.keyDown(window, { key: "Backspace" });
    await waitFor(() => {
      expect(container.querySelector('.react-flow__node[data-id="task:T1"]')).not.toBeNull();
    });
  });

  // T8: pane right-click → New feature / New task entries.
  it("pane right-click opens the New feature / New task menu (author)", async () => {
    const { container } = render(
      <Canvas project="demo" state={fixture} author {...noopDraftProps} />,
    );
    await waitFor(() => expect(screen.getAllByText("T1").length).toBeGreaterThan(0));
    const pane = container.querySelector(".react-flow__pane")!;
    fireEvent.contextMenu(pane);
    expect(await screen.findByTestId("canvas-ctx")).toBeInTheDocument();
    expect(screen.getByText("New feature")).toBeInTheDocument();
    expect(screen.getByText("New task")).toBeInTheDocument();
  });

  // T8: dep edges are ant-crawl edges (type:"ant") so the AntEdge renderer takes
  // over. The first eligible dep edge wins an ant slot (cap not exceeded here).
  it("dep edges render as ant edges with a crawling ant within the cap", async () => {
    const { container } = render(
      <Canvas project="demo" state={fixture} author={false} {...noopDraftProps} />,
    );
    await waitFor(() => {
      expect(container.querySelector('[data-id="dep:T1→T2"]')).not.toBeNull();
    });
    // The single dep edge is under the cap → it animates (animateMotion present).
    await waitFor(() => {
      expect(container.querySelector("animateMotion")).not.toBeNull();
    });
  });

  it("New-task editor pre-fills an auto-generated id (not blank)", async () => {
    render(
      <Canvas
        project="demo"
        state={fixture}
        author
        initialMode="plan"
        drafts={[]}
        setDrafts={() => {}}
        draftFeatures={[]}
        setDraftFeatures={() => {}}
      />,
    );
    fireEvent.click(await screen.findByRole("button", { name: /Task/ }));
    const idInput = (await screen.findByLabelText("task id")) as HTMLInputElement;
    // committed tasks are T1/T2 (uppercase) → 't' series is free from t1.
    expect(idInput.value).toBe("t1");
  });

  // T10: Office is the DEFAULT landing mode; the segment switches to Plan.
  it("lands in Office mode by default; the mode segment switches to Plan", async () => {
    const { container } = render(
      // No initialMode → defaults to "office".
      <Canvas project="demo" state={fixture} author drafts={[]} setDrafts={() => {}} draftFeatures={[]} setDraftFeatures={() => {}} />,
    );
    // Office surface present, Plan toolbar absent.
    await waitFor(() => expect(screen.getByTestId("office-view")).toBeInTheDocument());
    expect(screen.queryByTestId("canvas-toolbar")).toBeNull();
    expect(container.querySelector('[data-testid="desk-bob"]')).not.toBeNull();

    // Click "Plan" → Plan surface mounts, Office unmounts.
    fireEvent.click(screen.getByTestId("mode-plan"));
    await waitFor(() => expect(screen.getByTestId("canvas-toolbar")).toBeInTheDocument());
    expect(screen.queryByTestId("office-view")).toBeNull();

    // Back to Office.
    fireEvent.click(screen.getByTestId("mode-office"));
    await waitFor(() => expect(screen.getByTestId("office-view")).toBeInTheDocument());
  });

  // T10: dropping a draft onto a desk opens DispatchModal with owner pre-filled.
  it("office click-dispatch opens DispatchModal with the seat as owner", async () => {
    render(
      <Canvas
        project="demo"
        state={fixture}
        author
        drafts={[{ id: "d1", specMd: "# d", feature: "F1", deps: [] }]}
        setDrafts={() => {}}
        draftFeatures={[]}
        setDraftFeatures={() => {}}
      />,
    );
    // bob's desk is BUSY but with a single draft, a desk click dispatches.
    await waitFor(() => expect(screen.getByTestId("desk-bob")).toBeInTheDocument());
    fireEvent.click(screen.getByTestId("desk-bob"));
    const modal = await screen.findByTestId("dispatch-modal");
    expect(modal).toBeInTheDocument();
    // Owner is pre-filled read-only with the clicked seat.
    expect(modal.textContent).toContain("bob");
    expect(modal.textContent).toContain("d1");
  });

});


import { toRFNodes } from "./Canvas";
import { deriveFlow } from "../lib/canvas";

it("drafts are NOT clamped to their feature (extent), committed tasks are", () => {
  const flow = deriveFlow(fixture, {}, [
    { id: "D1", specMd: "# d", feature: "F1", deps: [] },
  ]);
  const nodes = toRFNodes(flow.nodes);
  const task = nodes.find((n) => n.id === "task:T1")!;
  const draft = nodes.find((n) => n.id === "draft:D1")!;
  expect(task.extent).toBe("parent");
  expect(draft.extent).toBeUndefined(); // must be able to reach the seat rail
  expect(draft.parentId).toBe("feature:F1"); // still grouped visually
});
