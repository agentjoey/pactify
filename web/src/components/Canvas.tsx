import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ReactFlow,
  Background,
  Controls,
  Position,
  type Node,
  type Edge,
  type NodeTypes,
  type NodeChange,
  applyNodeChanges,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import type { State } from "../lib/types";
import { deriveFlow, type Draft, type FlowNode, type LayoutJSON } from "../lib/canvas";
import { getLayout, putLayout } from "../lib/api";
import { TaskNode } from "./nodes/TaskNode";
import { SeatNode } from "./nodes/SeatNode";
import { FeatureGroup } from "./nodes/FeatureGroup";

const nodeTypes: NodeTypes = {
  task: TaskNode,
  seat: SeatNode,
  feature: FeatureGroup,
  draft: TaskNode, // drafts reuse TaskNode; data.draft drives the dashed style
};

// Feature-group sizing — a basic bound computed from its child rows so the
// translucent container wraps its tasks/drafts. Children position relative to
// the parent in React Flow, so a child at the group's own x/y maps to (0,0).
const CHILD_W = 200;
const ROW_H = 120;
const PAD = 16;
const HEADER = 28;

// featureStyle returns {width,height} sized to hold `rows` stacked children.
function featureStyle(rows: number): { width: number; height: number } {
  const r = Math.max(rows, 1);
  return { width: CHILD_W + PAD * 2, height: HEADER + r * ROW_H + PAD };
}

// handlesFor mirrors the <Handle> elements each node type renders, so edge
// routing can resolve handle bounds without a real DOM measurement pass.
type SeedHandle = NonNullable<Node["handles"]>[number];
function handlesFor(type: string): SeedHandle[] {
  if (type === "seat") {
    return [{ type: "source", position: Position.Right, x: CHILD_W, y: 40, width: 1, height: 1 }];
  }
  // task + draft
  return [
    { type: "target", position: Position.Top, x: CHILD_W / 2, y: 0, width: 1, height: 1 },
    { type: "source", position: Position.Bottom, x: CHILD_W / 2, y: 80, width: 1, height: 1 },
  ];
}

// toRFNodes maps derived FlowNodes → React Flow nodes. Feature nodes get a sized
// style + dropped into the front; children get parentId + extent:'parent' and
// their positions are rebased to be relative to the parent origin.
function toRFNodes(flow: FlowNode[]): Node[] {
  const byId = new Map(flow.map((n) => [n.id, n]));
  // Count child rows per feature for the container bound.
  const rows = new Map<string, number>();
  for (const n of flow) {
    if (n.parentId) rows.set(n.parentId, (rows.get(n.parentId) ?? 0) + 1);
  }

  const out: Node[] = [];
  for (const n of flow) {
    if (n.type === "feature") {
      const sz = featureStyle(rows.get(n.id) ?? 0);
      out.push({
        id: n.id,
        type: "feature",
        position: n.position,
        data: n.data,
        style: sz,
        // Seed measured dims so React Flow can route edges before the browser
        // measures the DOM (it never does under jsdom; harmless in the browser,
        // where real measurements override these on first frame).
        measured: sz,
      });
    }
  }
  for (const n of flow) {
    if (n.type === "feature") continue;
    const node: Node = {
      id: n.id,
      type: n.type,
      position: n.position,
      data: n.data,
      measured: { width: CHILD_W, height: 80 },
      // Seed handle bounds so edges resolve before the browser measures the DOM.
      // The real <Handle> components re-register on mount in the browser; under
      // jsdom (no layout) these seeds are what let dep edges render in tests.
      handles: handlesFor(n.type),
    };
    if (n.parentId && byId.has(n.parentId)) {
      const parent = byId.get(n.parentId)!;
      node.parentId = n.parentId;
      node.extent = "parent";
      // Rebase absolute grid coords to parent-relative coords.
      node.position = {
        x: PAD,
        y: HEADER + (n.position.y - parent.position.y - ROW_H >= 0
          ? Math.round((n.position.y - parent.position.y - ROW_H) / ROW_H) * ROW_H
          : 0),
      };
    }
    out.push(node);
  }
  // React Flow requires parents before children — features already pushed first.
  return out;
}

export function Canvas({
  project,
  state,
  author,
}: {
  project: string;
  state: State;
  author: boolean;
}) {
  void author; // gates future author affordances (Q7/Q8); unused for now
  const [layout, setLayout] = useState<LayoutJSON>({});
  const [drafts] = useState<Draft[]>([]); // Q7 fills this
  const [nodes, setNodes] = useState<Node[]>([]);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const layoutRef = useRef<LayoutJSON>({});
  layoutRef.current = layout;

  // Load saved layout on mount / project change.
  useEffect(() => {
    let alive = true;
    getLayout(project).then((l) => { if (alive) setLayout(l ?? {}); }).catch(() => {
      if (alive) setLayout({});
    });
    return () => { alive = false; };
  }, [project]);

  // Derive the flow graph from protocol state + saved layout + drafts. SSE state
  // changes re-run this; unmoved nodes keep their layout-saved positions.
  const flow = useMemo(
    () => deriveFlow(state, layout, drafts),
    [state, layout, drafts],
  );

  const edges: Edge[] = useMemo(
    () =>
      flow.edges.map((e) => ({
        id: e.id,
        source: e.source,
        target: e.target,
        style: { stroke: "#484f58" },
      })),
    [flow.edges],
  );

  // Rebuild RF nodes whenever the derived flow changes.
  useEffect(() => {
    setNodes(toRFNodes(flow.nodes));
  }, [flow.nodes]);

  // Debounced layout persistence (single timer).
  const scheduleSave = useCallback((next: LayoutJSON) => {
    setLayout(next);
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => {
      putLayout(project, next).catch(() => {});
    }, 800);
  }, [project]);

  useEffect(() => () => { if (timer.current) clearTimeout(timer.current); }, []);

  const onNodesChange = useCallback((changes: NodeChange[]) => {
    setNodes((nds) => applyNodeChanges(changes, nds));
  }, []);

  // Persist a node's new position into the layout sidecar after a drag.
  const onNodeDragStop = useCallback(
    (_e: unknown, node: Node) => {
      const positions = { ...(layoutRef.current.positions ?? {}) };
      positions[node.id] = { x: node.position.x, y: node.position.y };
      scheduleSave({ ...layoutRef.current, positions });
    },
    [scheduleSave],
  );

  return (
    <div className="flex-1" data-testid="canvas-root">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        onNodesChange={onNodesChange}
        onNodeDragStop={onNodeDragStop}
        fitView
        proOptions={{ hideAttribution: true }}
        colorMode="dark"
      >
        <Background color="#21262d" gap={20} />
        <Controls showInteractive={false} />
      </ReactFlow>
    </div>
  );
}
