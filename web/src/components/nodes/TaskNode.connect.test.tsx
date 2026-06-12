import { render } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";

// Mock the @xyflow/react surface TaskNode touches so we can drive the
// useConnection() state directly and assert the connect-target highlight ring —
// the affordance that says "you can aim here" while a connection is dragged.
// Handle is stubbed to a plain div (no flow store needed) preserving its class
// passthrough so the bar-handle classes remain assertable here too.
const connectionState: { inProgress: boolean; fromId?: string } = {
  inProgress: false,
  fromId: undefined,
};

vi.mock("@xyflow/react", () => ({
  Handle: ({ className, children }: { className?: string; children?: unknown }) => (
    <div className={`react-flow__handle ${className ?? ""}`}>{children as never}</div>
  ),
  Position: { Top: "top", Bottom: "bottom" },
  useConnection: () => ({
    inProgress: connectionState.inProgress,
    fromNode: connectionState.fromId ? { id: connectionState.fromId } : undefined,
  }),
}));

import { TaskNode } from "./TaskNode";
import type { NodeProps } from "@xyflow/react";

const baseData = {
  task: { id: "T1", owner: "bob", reviewer: "alice", status: "assigned", spec: "", evidence: "" },
};

function renderTaskNode() {
  // NodeProps is loose; TaskNode only consumes id + data, so cast a partial.
  const props = { id: "task:T2", data: baseData } as unknown as NodeProps;
  return render(<TaskNode {...props} />);
}

describe("TaskNode connect-target highlight", () => {
  it("no highlight ring when no connection is in progress", () => {
    connectionState.inProgress = false;
    connectionState.fromId = undefined;
    const { container } = renderTaskNode();
    expect(container.querySelector(".task-node.connect-target")).toBeNull();
  });

  it("highlights as a valid drop target when a connection drags from another node", () => {
    connectionState.inProgress = true;
    connectionState.fromId = "task:T1"; // different from this node (task:T2)
    const { container } = renderTaskNode();
    expect(container.querySelector(".task-node.connect-target")).not.toBeNull();
  });

  it("does NOT highlight the connection's own source node (self-loop)", () => {
    connectionState.inProgress = true;
    connectionState.fromId = "task:T2"; // same as this node
    const { container } = renderTaskNode();
    expect(container.querySelector(".task-node.connect-target")).toBeNull();
  });
});
