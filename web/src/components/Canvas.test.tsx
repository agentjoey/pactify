import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import type { State } from "../lib/types";

// Mock the api module: getLayout resolves empty, putLayout is a spy, and
// getActingSeat is unused by Canvas but stubbed for completeness.
const putLayout = vi.fn().mockResolvedValue(undefined);
vi.mock("../lib/api", () => ({
  getLayout: vi.fn().mockResolvedValue({}),
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

describe("Canvas", () => {
  beforeEach(() => {
    putLayout.mockClear();
  });

  it("renders task nodes, seat rail, and the dep edge", async () => {
    const { container } = render(
      <Canvas project="demo" state={fixture} author={false} />,
    );

    // Task node ids appear.
    await waitFor(() => {
      expect(screen.getByText("T1")).toBeInTheDocument();
      expect(screen.getByText("T2")).toBeInTheDocument();
    });

    // Seat rail cards.
    expect(screen.getByText("alice")).toBeInTheDocument();
    expect(screen.getByText("bob")).toBeInTheDocument();

    // Feature group header.
    expect(screen.getByText("F1")).toBeInTheDocument();

    // Dep edge: React Flow renders edges as DOM elements with a data-id.
    await waitFor(() => {
      expect(container.querySelector('[data-id="dep:T1→T2"]')).not.toBeNull();
    });
  });

  it("replay mode is read-only: no author affordances rendered", async () => {
    // App passes author={author && !replaying}, so a replaying canvas receives
    // author=false → the build-mode toolbar (New feature / New task) is absent.
    render(<Canvas project="demo" state={fixture} author={false} replaying />);
    await waitFor(() => expect(screen.getByText("T1")).toBeInTheDocument());
    expect(screen.queryByText("+ New feature")).toBeNull();
    expect(screen.queryByText("+ New task")).toBeNull();
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
    const { container } = render(<Canvas project="demo" state={blocked} author={false} />);
    await waitFor(() => expect(screen.getByText("T2")).toBeInTheDocument());

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
      <Canvas project="demo" state={fixture} author={false} pulses={new Set(["T2"])} />,
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
      <Canvas project="demo" state={fixture} author={false} onSelectTask={onSelectTask} />,
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
