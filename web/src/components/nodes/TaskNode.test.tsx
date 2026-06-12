import { render } from "@testing-library/react";
import { ReactFlow, ReactFlowProvider, type Node } from "@xyflow/react";
import { describe, it, expect } from "vitest";
import { TaskNode } from "./TaskNode";

const nodeTypes = { task: TaskNode };

function renderNode(data: Record<string, unknown>) {
  const nodes: Node[] = [
    { id: "task:T1", type: "task", position: { x: 0, y: 0 }, data, measured: { width: 200, height: 80 } },
  ];
  return render(
    <ReactFlowProvider>
      <div style={{ width: 400, height: 400 }}>
        <ReactFlow nodes={nodes} edges={[]} nodeTypes={nodeTypes} nodesConnectable />
      </div>
    </ReactFlowProvider>,
  );
}

describe("TaskNode bar connect handles", () => {
  it("renders a top (target) and bottom (source) bar handle, each with a .port-mark", () => {
    const { container } = renderNode({
      task: { id: "T1", owner: "bob", reviewer: "alice", status: "assigned", spec: "", evidence: "" },
    });
    const inPort = container.querySelector(".react-flow__handle.task-port.task-port-in");
    const outPort = container.querySelector(".react-flow__handle.task-port.task-port-out");
    expect(inPort).not.toBeNull();
    expect(outPort).not.toBeNull();
    // Each bar carries a central mark element.
    expect(inPort!.querySelector(".port-mark")).not.toBeNull();
    expect(outPort!.querySelector(".port-mark")).not.toBeNull();
    // Source/target wiring preserved.
    expect(inPort!.classList.contains("target")).toBe(true);
    expect(outPort!.classList.contains("source")).toBe(true);
  });

  it("the target (in) bar is not a connection START handle (isConnectableStart=false)", () => {
    const { container } = renderNode({
      task: { id: "T1", owner: "bob", reviewer: "alice", status: "assigned", spec: "", evidence: "" },
    });
    // React Flow v12 renders connectablestart on handles that can START a
    // connection. isConnectableStart={false} on the target means it can only be a
    // drop end — so it carries connectableend but NOT connectablestart.
    const inPort = container.querySelector(".react-flow__handle.task-port.task-port-in")!;
    const outPort = container.querySelector(".react-flow__handle.task-port.task-port-out")!;
    expect(inPort.classList.contains("connectablestart")).toBe(false);
    expect(outPort.classList.contains("connectablestart")).toBe(true);
  });

  it("keeps the dispatch affordance + not-joined badge for drafts", () => {
    const { getByTestId } = renderNode({
      draft: true,
      onDispatch: () => {},
      commsNotJoined: true,
    });
    expect(getByTestId("dispatch-btn")).not.toBeNull();
    expect(getByTestId("notjoined-badge")).not.toBeNull();
  });
});
