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
  type Connection,
  applyNodeChanges,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import type { State } from "../lib/types";
import {
  deriveFlow,
  toParentRelative,
  childToAbsolute,
  applyConnect,
  type Draft,
  type DraftFeature,
  type FlowNode,
  type LayoutJSON,
} from "../lib/canvas";
import { getLayout, putLayout } from "../lib/api";
import { TaskNode } from "./nodes/TaskNode";
import { SeatNode } from "./nodes/SeatNode";
import { FeatureGroup } from "./nodes/FeatureGroup";
import { TaskEditor, type FeatureOption } from "./TaskEditor";
import { DispatchModal } from "./DispatchModal";

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
// style and are pushed first (React Flow requires parents before children);
// children get parentId + extent:'parent'. Child positions arrive already
// rebased to parent-relative coords by toParentRelative() — this function does
// NO coordinate math, so there is a single rebase path in the codebase.
function toRFNodes(flow: FlowNode[], staleTasks?: Set<string>): Node[] {
  const rel = toParentRelative(flow);
  // Count child rows per feature for the container bound.
  const rows = new Map<string, number>();
  for (const n of rel) {
    if (n.parentId) rows.set(n.parentId, (rows.get(n.parentId) ?? 0) + 1);
  }

  const out: Node[] = [];
  for (const n of rel) {
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
  for (const n of rel) {
    if (n.type === "feature") continue;
    const rawId = n.id.replace(/^(task|draft):/, "");
    const data = staleTasks?.has(rawId) ? { ...n.data, stale: true } : n.data;
    const node: Node = {
      id: n.id,
      type: n.type,
      position: n.position,
      data,
      measured: { width: CHILD_W, height: 80 },
      // Seed handle bounds so edges resolve before the browser measures the DOM.
      // The real <Handle> components re-register on mount in the browser; under
      // jsdom (no layout) these seeds are what let dep edges render in tests.
      handles: handlesFor(n.type),
    };
    if (n.parentId) {
      node.parentId = n.parentId;
      node.extent = "parent";
    }
    out.push(node);
  }
  return out;
}

// SLUG_RE validates a feature id / branch slug entered in the New-feature form.
const SLUG_RE = /^[a-z0-9][a-z0-9-]*$/;

// nodeBounds returns the absolute bounding box of a React Flow node. Children
// (parentId set) report parent-relative positions, so we add the parent's
// absolute position. Used by the drag-to-dispatch overlap test.
function nodeBounds(
  node: Node,
  all: Node[],
): { x: number; y: number; w: number; h: number } {
  let x = node.position.x;
  let y = node.position.y;
  if (node.parentId) {
    const p = all.find((n) => n.id === node.parentId);
    if (p) {
      x += p.position.x;
      y += p.position.y;
    }
  }
  const w = node.measured?.width ?? CHILD_W;
  const h = node.measured?.height ?? 80;
  return { x, y, w, h };
}

// overlaps is a simple AABB intersection test between two bounding boxes.
function overlaps(
  a: { x: number; y: number; w: number; h: number },
  b: { x: number; y: number; w: number; h: number },
): boolean {
  return a.x < b.x + b.w && a.x + a.w > b.x && a.y < b.y + b.h && a.y + a.h > b.y;
}

export function Canvas({
  project,
  state,
  author,
  staleTasks,
  onSelectTask,
}: {
  project: string;
  state: State;
  author: boolean;
  // Raw task ids that have sat in_progress past the stale threshold (App owns
  // the timestamp map). Rendered as an amber dot on the task node.
  staleTasks?: Set<string>;
  // Clicking a committed task node selects it (drives the RightRail review
  // flow). Receives the raw task id (no "task:" prefix).
  onSelectTask?: (id: string) => void;
}) {
  const [layout, setLayout] = useState<LayoutJSON>({});
  const [drafts, setDrafts] = useState<Draft[]>([]);
  const [draftFeatures, setDraftFeatures] = useState<DraftFeature[]>([]);
  const [nodes, setNodes] = useState<Node[]>([]);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const layoutRef = useRef<LayoutJSON>({});
  layoutRef.current = layout;

  // Build-mode UI state.
  const [editorOpen, setEditorOpen] = useState(false);
  const [editingDraft, setEditingDraft] = useState<Draft | undefined>(undefined);
  const [newFeatureOpen, setNewFeatureOpen] = useState(false);
  const [nfId, setNfId] = useState("");
  const [nfBranch, setNfBranch] = useState("");
  const [notice, setNotice] = useState(""); // transient toast (deps-fixed cue)
  // Dispatch target: a draft dropped onto a seat.
  const [dispatch, setDispatch] = useState<{ draft: Draft; owner: string } | undefined>(undefined);

  // Load saved layout on mount / project change.
  useEffect(() => {
    let alive = true;
    getLayout(project).then((l) => { if (alive) setLayout(l ?? {}); }).catch(() => {
      if (alive) setLayout({});
    });
    return () => { alive = false; };
  }, [project]);

  // Derive the flow graph from protocol state + saved layout + drafts +
  // draft-features. SSE state changes re-run this; unmoved nodes keep their
  // layout-saved positions.
  const flow = useMemo(
    () => deriveFlow(state, layout, drafts, draftFeatures),
    [state, layout, drafts, draftFeatures],
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

  // Rebuild RF nodes whenever the derived flow (or stale set) changes.
  useEffect(() => {
    setNodes(toRFNodes(flow.nodes, staleTasks));
  }, [flow.nodes, staleTasks]);

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

  // Feature options for the editor: committed features + draft features.
  const featureOptions: FeatureOption[] = useMemo(() => {
    const opts: FeatureOption[] = state.features.map((f) => ({ id: f.id, label: f.id }));
    for (const df of draftFeatures) {
      if (!state.features.some((f) => f.id === df.id)) {
        opts.push({ id: df.id, label: `${df.id} (draft)` });
      }
    }
    return opts;
  }, [state.features, draftFeatures]);

  // existingIds: task ids already taken (committed + drafts) for the editor.
  const existingTaskIds = useMemo(() => {
    const ids: string[] = [];
    for (const f of state.features) for (const t of f.tasks) ids.push(t.id);
    for (const d of drafts) ids.push(d.id);
    return ids;
  }, [state.features, drafts]);

  // committedTaskIds: only assigned tasks (for the deps-fixed onConnect rule).
  const committedTaskIds = useMemo(() => {
    const s = new Set<string>();
    for (const f of state.features) for (const t of f.tasks) s.add(t.id);
    return s;
  }, [state.features]);

  // featureOfTask maps a raw task/draft id to its feature, so onConnect can
  // enforce the same-feature dep constraint.
  const featureOfId = useCallback(
    (rawId: string): string | undefined => {
      for (const f of state.features) {
        if (f.tasks.some((t) => t.id === rawId)) return f.id;
      }
      const d = drafts.find((x) => x.id === rawId);
      return d?.feature;
    },
    [state.features, drafts],
  );

  const flashNotice = useCallback((msg: string) => {
    setNotice(msg);
    setTimeout(() => setNotice(""), 4000);
  }, []);

  // onConnect: author draws a dep edge. A→B means "B depends on A" (source is
  // the prerequisite). Only same-feature edges are allowed, and only a DRAFT
  // target can change (committed-task deps are fixed at assign time — surfaced
  // as a notice via applyConnect's contract).
  const onConnect = useCallback(
    (c: Connection) => {
      if (!author) return;
      const raw = (id: string) => id.replace(/^(task|draft):/, "");
      const from = raw(c.source);
      const to = raw(c.target);
      if (from === to) return;
      if (committedTaskIds.has(to)) {
        flashNotice("deps are fixed at assign time");
        return;
      }
      if (featureOfId(from) !== featureOfId(to)) {
        flashNotice("deps must stay within one feature");
        return;
      }
      setDrafts((ds) => applyConnect(ds, c.source, c.target));
    },
    [author, committedTaskIds, featureOfId, flashNotice],
  );

  // Persist a node's new position into the layout sidecar after a drag. The
  // layout stores ABSOLUTE coords for every node (one coordinate system). A
  // child reports a parent-relative position, so convert it back to absolute
  // using its parent's CURRENT absolute position from the live nodes state.
  //
  // Drag-to-dispatch: if a DRAFT node is dropped overlapping a SEAT node, open
  // the DispatchModal with that seat as the prefilled owner instead of saving a
  // layout position (the dispatch flow replaces the draft on success).
  const onNodeDragStop = useCallback(
    (_e: unknown, node: Node, allNodes: Node[]) => {
      // Observe-only dashboards never persist layout drags.
      if (!author) return;
      if (node.type === "draft") {
        const dragBox = nodeBounds(node, allNodes);
        const seat = allNodes.find(
          (n) => n.type === "seat" && overlaps(dragBox, nodeBounds(n, allNodes)),
        );
        if (seat) {
          const rawId = node.id.replace(/^draft:/, "");
          const d = drafts.find((x) => x.id === rawId);
          if (d) {
            setDispatch({ draft: d, owner: seat.id.replace(/^seat:/, "") });
            return; // don't persist a layout move for a dispatch gesture
          }
        }
      }

      let abs = { x: node.position.x, y: node.position.y };
      if (node.parentId) {
        const parent = allNodes.find((n) => n.id === node.parentId);
        if (parent) abs = childToAbsolute(node.position, parent.position);
      }
      const positions = { ...(layoutRef.current.positions ?? {}) };
      positions[node.id] = abs;
      scheduleSave({ ...layoutRef.current, positions });
    },
    [author, drafts, scheduleSave],
  );

  // Click a node:
  //   - a committed task → select it (drives the RightRail review flow);
  //   - a draft (author only) → reopen the editor prefilled for that draft.
  const onNodeClick = useCallback(
    (_e: unknown, node: Node) => {
      if (node.type === "task") {
        onSelectTask?.(node.id.replace(/^task:/, ""));
        return;
      }
      if (!author || node.type !== "draft") return;
      const rawId = node.id.replace(/^draft:/, "");
      const d = drafts.find((x) => x.id === rawId);
      if (d) {
        setEditingDraft(d);
        setEditorOpen(true);
      }
    },
    [author, drafts, onSelectTask],
  );

  // --- build-mode handlers -------------------------------------------------
  const addFeature = () => {
    const id = nfId.trim();
    const branch = nfBranch.trim();
    if (!SLUG_RE.test(id) || !SLUG_RE.test(branch)) return;
    if (state.features.some((f) => f.id === id) || draftFeatures.some((f) => f.id === id)) return;
    setDraftFeatures((dfs) => [...dfs, { id, branch }]);
    setNfId("");
    setNfBranch("");
    setNewFeatureOpen(false);
  };

  const saveDraft = (d: Draft) => {
    setDrafts((ds) => {
      const i = ds.findIndex((x) => x.id === d.id);
      if (i >= 0) {
        const next = ds.slice();
        next[i] = d;
        return next;
      }
      return [...ds, d];
    });
    setEditorOpen(false);
    setEditingDraft(undefined);
  };

  const deleteDraft = () => {
    if (editingDraft) setDrafts((ds) => ds.filter((x) => x.id !== editingDraft.id));
    setEditorOpen(false);
    setEditingDraft(undefined);
  };

  const branchForFeature = (featureId: string): string => {
    const f = state.features.find((x) => x.id === featureId);
    if (f) return f.branch;
    const df = draftFeatures.find((x) => x.id === featureId);
    return df?.branch ?? "";
  };

  const roster = state.agents.map((a) => a.id);

  const nfIdBad = nfId.length > 0 && !SLUG_RE.test(nfId.trim());
  const nfBranchBad = nfBranch.length > 0 && !SLUG_RE.test(nfBranch.trim());
  const nfValid = SLUG_RE.test(nfId.trim()) && SLUG_RE.test(nfBranch.trim());

  return (
    <div className="relative flex-1" data-testid="canvas-root">
      {author && (
        <div className="absolute left-2 top-2 z-10 flex items-center gap-2">
          {!newFeatureOpen ? (
            <button
              className="rounded border border-[#30363d] bg-[#161b22] px-2.5 py-1 text-xs text-[#e6edf3] hover:border-[#8b949e]"
              onClick={() => setNewFeatureOpen(true)}
            >
              + New feature
            </button>
          ) : (
            <div className="flex items-center gap-1 rounded border border-[#30363d] bg-[#161b22] px-2 py-1">
              <input
                aria-label="feature id"
                className="w-24 rounded border border-[#30363d] bg-[#0d1117] px-1.5 py-0.5 text-xs text-[#e6edf3]"
                placeholder="feature id"
                value={nfId}
                onChange={(e) => setNfId(e.target.value)}
              />
              <input
                aria-label="feature branch"
                className="w-28 rounded border border-[#30363d] bg-[#0d1117] px-1.5 py-0.5 font-mono text-xs text-[#e6edf3]"
                placeholder="feat/x"
                value={nfBranch}
                onChange={(e) => setNfBranch(e.target.value)}
              />
              <button
                className="rounded bg-[#238636] px-2 py-0.5 text-xs font-semibold text-white disabled:opacity-50"
                onClick={addFeature}
                disabled={!nfValid}
              >
                Add
              </button>
              <button
                className="rounded border border-[#30363d] px-2 py-0.5 text-xs text-[#8b949e]"
                onClick={() => { setNewFeatureOpen(false); setNfId(""); setNfBranch(""); }}
              >
                ✕
              </button>
              {(nfIdBad || nfBranchBad) && (
                <span className="text-[10px] text-[#f85149]">slug: [a-z0-9][a-z0-9-]*</span>
              )}
            </div>
          )}
          <button
            className="rounded border border-[#30363d] bg-[#161b22] px-2.5 py-1 text-xs text-[#e6edf3] hover:border-[#8b949e] disabled:opacity-50"
            onClick={() => { setEditingDraft(undefined); setEditorOpen(true); }}
            disabled={featureOptions.length === 0}
          >
            + New task
          </button>
        </div>
      )}

      {notice && (
        <div
          data-testid="canvas-notice"
          className="absolute left-1/2 top-2 z-10 -translate-x-1/2 rounded border border-[#d29922] bg-[#161b22] px-3 py-1 text-xs text-[#d29922]"
        >
          {notice}
        </div>
      )}

      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        onNodesChange={onNodesChange}
        onNodeDragStop={onNodeDragStop}
        onNodeClick={onNodeClick}
        onConnect={onConnect}
        nodesDraggable={author}
        nodesConnectable={author}
        fitView
        proOptions={{ hideAttribution: true }}
        colorMode="dark"
      >
        <Background color="#21262d" gap={20} />
        <Controls showInteractive={false} />
      </ReactFlow>

      {editorOpen && (
        <TaskEditor
          initial={editingDraft}
          features={featureOptions}
          existingIds={existingTaskIds}
          onSave={saveDraft}
          onDelete={editingDraft ? deleteDraft : undefined}
          onClose={() => { setEditorOpen(false); setEditingDraft(undefined); }}
        />
      )}

      {dispatch && (
        <DispatchModal
          project={project}
          draft={dispatch.draft}
          owner={dispatch.owner}
          roster={roster}
          branch={branchForFeature(dispatch.draft.feature)}
          onDispatched={() => {
            // Remove the draft locally; SSE delivers the committed task.
            setDrafts((ds) => ds.filter((x) => x.id !== dispatch.draft.id));
            setDispatch(undefined);
          }}
          onClose={() => setDispatch(undefined)}
        />
      )}
    </div>
  );
}
