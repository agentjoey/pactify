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
        <ReactFlow nodes={nodes} edges={[]} nodeTypes={nodeTypes} />
      </div>
    </ReactFlowProvider>,
  );
}

describe("TaskNode connect handles (T8)", () => {
  it("renders enlarged source + target handles with the .task-handle class", () => {
    const { container } = renderNode({
      task: { id: "T1", owner: "bob", reviewer: "alice", status: "assigned", spec: "", evidence: "" },
    });
    const handles = container.querySelectorAll(".react-flow__handle.task-handle");
    expect(handles.length).toBe(2);
    // Hit area enlarged to ≥12px (set via inline style).
    handles.forEach((h) => {
      expect((h as HTMLElement).style.width).toBe("12px");
      expect((h as HTMLElement).style.height).toBe("12px");
    });
    // A source and a target handle are present.
    expect(container.querySelector(".react-flow__handle.source")).not.toBeNull();
    expect(container.querySelector(".react-flow__handle.target")).not.toBeNull();
  });
});
