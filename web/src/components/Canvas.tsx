import { useCallback, useEffect, useMemo, useRef, useState, type Dispatch, type MouseEvent as ReactMouseEvent, type SetStateAction } from "react";
import {
  ReactFlow,
  Position,
  useReactFlow,
  useViewport,
  type Node,
  type Edge,
  type NodeTypes,
  type EdgeTypes,
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
  assignAntFlags,
  isValidDep,
  nextId,
  type Draft,
  type DraftFeature,
  type FlowNode,
  type LayoutJSON,
  type AntEdgeKind,
} from "../lib/canvas";
import { getLayout, putLayout } from "../lib/api";
import { deriveComms, mergeComms } from "../lib/comms";
import { TaskNode } from "./nodes/TaskNode";
import { SeatNode } from "./nodes/SeatNode";
import { FeatureGroup } from "./nodes/FeatureGroup";
import { TaskEditor, type FeatureOption } from "./TaskEditor";
import { DispatchModal } from "./DispatchModal";
import { ContextMenu, type MenuTarget } from "./canvas/ContextMenu";
import { Toolbar } from "./canvas/Toolbar";
import { Hud } from "./canvas/Hud";
import { AntEdge } from "./canvas/edges/AntEdge";

const nodeTypes: NodeTypes = {
  task: TaskNode,
  seat: SeatNode,
  feature: FeatureGroup,
  draft: TaskNode, // drafts reuse TaskNode; data.draft drives the dashed style
};

const edgeTypes: EdgeTypes = {
  ant: AntEdge, // dep + comms wait edges crawl an ant along the path (T8)
};

// prefersReducedMotion reads the OS setting once per call. matchMedia is absent
// in some jsdom configs — treat that as "no preference" (allow motion).
function prefersReducedMotion(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches
  );
}

// FitOnEntry runs a fitView the first time a given project's nodes render,
// then again only when the project changes — NOT on every SSE snapshot (that
// would yank the viewport on each live update). App clears the snapshot to
// EMPTY on project switch, so `ready` only turns true once the NEW project's
// nodes exist — the fit always frames the right graph. Must live inside
// <ReactFlow> (it reads the flow store via useReactFlow). Reduced-motion gets
// an instant (0ms) fit instead of the 300ms ease.
function FitOnEntry({ project, ready }: { project: string; ready: boolean }) {
  const { fitView } = useReactFlow();
  const fittedFor = useRef<string | null>(null);
  useEffect(() => {
    if (!ready) return;
    if (fittedFor.current === project) return;
    fittedFor.current = project;
    fitView({ duration: prefersReducedMotion() ? 0 : 300 });
  }, [project, ready, fitView]);
  return null;
}

// SnapGuides renders the drag-time alignment lines (T8). Guides arrive in FLOW
// space (node x/y); this transforms them to screen space via the live viewport
// so the 1px lines track zoom/pan. Must render inside <ReactFlow> (uses the
// flow store). Display-only — never persisted.
function SnapGuides({ guides }: { guides: { axis: "v" | "h"; pos: number }[] }) {
  const { x: vx, y: vy, zoom } = useViewport();
  if (guides.length === 0) return null;
  return (
    <div className="canvas-guides" data-testid="canvas-guides" aria-hidden>
      {guides.map((g, i) =>
        g.axis === "v" ? (
          <div key={i} className="guide v" style={{ left: g.pos * zoom + vx }} />
        ) : (
          <div key={i} className="guide h" style={{ top: g.pos * zoom + vy }} />
        ),
      )}
    </div>
  );
}

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
export function toRFNodes(flow: FlowNode[], staleTasks?: Set<string>): Node[] {
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
      // Committed tasks stay visually inside their feature. Drafts must be able
      // to LEAVE the group to reach the seat rail (drag-to-dispatch), so they
      // get no extent clamp.
      if (n.type === "task") node.extent = "parent";
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
  replaying,
  staleTasks,
  pulses,
  onSelectTask,
  drafts,
  setDrafts,
  draftFeatures,
  setDraftFeatures,
}: {
  project: string;
  state: State;
  author: boolean;
  // Read-only replay mode: drag/drop/dispatch short-circuit and author
  // affordances are unreachable. App also passes author=false while replaying,
  // but this is an explicit belt-and-suspenders guard on the gesture handlers.
  replaying?: boolean;
  // Raw task ids that have sat in_progress past the stale threshold (App owns
  // the timestamp map). Rendered as an amber dot on the task node.
  staleTasks?: Set<string>;
  // Raw task ids to pulse once (live SSE diff). App owns the set; a task node
  // whose id is present gets a transient `pulse` class (role-colored glow).
  pulses?: Set<string>;
  // Clicking a committed task node selects it (drives the RightRail review
  // flow). Receives the raw task id (no "task:" prefix).
  onSelectTask?: (id: string) => void;
  // Build-mode drafts are owned by App (lifted state) so they survive the
  // Canvas unmount caused by view switching. Canvas mutates them via the
  // setters exactly as it did when they were component-local.
  drafts: Draft[];
  setDrafts: Dispatch<SetStateAction<Draft[]>>;
  draftFeatures: DraftFeature[];
  setDraftFeatures: Dispatch<SetStateAction<DraftFeature[]>>;
}) {
  // Comms overlay toggle — component-local, default OFF. It's a display lens
  // (derived wait edges + node markers merged into the rendered graph), never
  // persisted to layout.json.
  const [comms, setComms] = useState(false);
  const [layout, setLayout] = useState<LayoutJSON>({});
  const [nodes, setNodes] = useState<Node[]>([]);
  // React Flow v12 passes only the DRAGGED nodes as the drag handlers' third
  // argument — parent/seat lookups need the FULL node list, kept fresh here.
  const nodesRef = useRef<Node[]>([]);
  useEffect(() => { nodesRef.current = nodes; }, [nodes]);
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
  const [dispatch, setDispatch] = useState<{ draft: Draft; owner?: string } | undefined>(undefined);
  const [draggingDraft, setDraggingDraft] = useState(false);

  // Context menu (T8) — open position + target, gated to author && !replaying.
  const [menu, setMenu] = useState<MenuTarget | null>(null);
  // Snap guides (T8) — alignment lines drawn while dragging; cleared on stop.
  const [guides, setGuides] = useState<{ axis: "v" | "h"; pos: number }[]>([]);
  // Inline rename (T8) — a draft whose title is being edited in place.
  const [renaming, setRenaming] = useState<{ id: string; x: number; y: number; value: string } | null>(null);
  // The stage element, so context-menu / rename coords are stage-relative.
  const stageRef = useRef<HTMLDivElement>(null);
  // Selection is read from nodesRef (the single source of truth) wherever Del/
  // Esc need it — a separate id set would desync when the rebuild effect
  // replaces the node array wholesale (SSE snapshots, project switches) and
  // leave "ghost" selections that delete the wrong drafts.
  const selectedIds = useCallback(
    () => nodesRef.current.filter((n) => n.selected).map((n) => n.id),
    [],
  );

  // Secondary dispatch entry: the button on a draft node (the drag gesture is
  // the primary path; the button exists for discoverability).
  function openDispatchFor(draftId: string) {
    if (replaying) return; // read-only replay: dispatch is unreachable
    const d = drafts.find((x) => x.id === draftId);
    if (d) setDispatch({ draft: d });
  }

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

  // Dep edges render as ant-crawl edges (type:"ant"): a carrier ant + cargo
  // cube crawls source→target. The base dashed look is unchanged (style
  // passthrough); `ant` is decided by the cap pass in `display` below.
  const edges: Edge[] = useMemo(
    () =>
      flow.edges.map((e) => ({
        id: e.id,
        source: e.source,
        target: e.target,
        type: "ant",
        style: { stroke: "#484f58", strokeDasharray: "5 4" },
        data: { kind: "dep" as const, color: "#484f58", ant: false },
      })),
    [flow.edges],
  );

  // Rebuild RF nodes whenever the derived flow (or stale set) changes.
  useEffect(() => {
    setNodes(
      toRFNodes(flow.nodes, staleTasks).map((n) =>
        n.type === "draft"
          ? { ...n, data: { ...n.data, onDispatch: () => openDispatchFor(n.id.replace(/^draft:/, "")) } }
          : n,
      ),
    );
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

  // existingFeatureIds: feature ids already taken (committed + draft features),
  // used to seed the New-feature form's default id.
  const existingFeatureIds = useMemo(() => {
    const ids = state.features.map((f) => f.id);
    for (const df of draftFeatures) ids.push(df.id);
    return ids;
  }, [state.features, draftFeatures]);

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

  // depGraph: the id→deps view + feature/committed lookups isValidDep needs.
  // Rebuilt from committed tasks + drafts whenever either changes.
  const depGraph = useMemo(() => {
    const deps = new Map<string, string[]>();
    for (const f of state.features) for (const t of f.tasks) deps.set(t.id, t.deps ?? []);
    for (const d of drafts) deps.set(d.id, d.deps);
    return { deps, featureOf: featureOfId, committed: committedTaskIds };
  }, [state.features, drafts, featureOfId, committedTaskIds]);

  // isValidConnection paints the not-allowed cursor on invalid drop targets
  // WHILE dragging (cross-feature / self / committed-target / cycle), so the
  // author gets live feedback before releasing. Mirrors onConnect's rule set
  // through the single isValidDep helper.
  const isValidConnection = useCallback(
    (c: Connection | Edge): boolean => {
      const raw = (id: string) => id.replace(/^(task|draft):/, "");
      return isValidDep(depGraph, raw(c.source), raw(c.target));
    },
    [depGraph],
  );

  // onConnect: author draws a dep edge. A→B means "B depends on A" (source is
  // the prerequisite). Validation routes through isValidDep; invalid drops get a
  // human notice (the drag-time cursor already discouraged them).
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
      if (!isValidDep(depGraph, from, to)) {
        flashNotice("that would create a dependency cycle");
        return;
      }
      setDrafts((ds) => applyConnect(ds, c.source, c.target));
    },
    [author, committedTaskIds, featureOfId, flashNotice, depGraph, setDrafts],
  );

  // Persist a node's new position into the layout sidecar after a drag. The
  // layout stores ABSOLUTE coords for every node (one coordinate system). A
  // child reports a parent-relative position, so convert it back to absolute
  // using its parent's CURRENT absolute position from the live nodes state.
  //
  // Drag-to-dispatch: if a DRAFT node is dropped overlapping a SEAT node, open
  // the DispatchModal with that seat as the prefilled owner instead of saving a
  // layout position (the dispatch flow replaces the draft on success).
  const onNodeDragStart = useCallback((_e: unknown, node: Node) => {
    if (replaying) return;
    if (node.type === "draft") setDraggingDraft(true);
  }, [replaying]);

  // Snap guides (T8): while dragging, if the dragged node's absolute x or y is
  // within ±6px of ANOTHER node's x/y, surface a 1px guide line (computed in
  // flow space; SnapGuides transforms to screen). Display-only.
  const SNAP = 6;
  const onNodeDrag = useCallback((_e: unknown, node: Node) => {
    if (replaying) return;
    const all = nodesRef.current;
    const me = nodeBounds(node, all);
    const next: { axis: "v" | "h"; pos: number }[] = [];
    for (const other of all) {
      if (other.id === node.id || other.type === "feature") continue;
      const ob = nodeBounds(other, all);
      if (Math.abs(ob.x - me.x) <= SNAP) next.push({ axis: "v", pos: ob.x });
      if (Math.abs(ob.y - me.y) <= SNAP) next.push({ axis: "h", pos: ob.y });
    }
    setGuides(next);
  }, [replaying]);

  const onNodeDragStop = useCallback(
    (_e: unknown, node: Node) => {
      setDraggingDraft(false);
      setGuides([]); // clear snap guides
      // Replay is read-only and observe-only dashboards never persist drags.
      if (replaying || !author) return;
      // RF v12: the callback's nodes arg is the dragged selection only — use
      // the live full list for parent/seat resolution.
      const allNodes = nodesRef.current;
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
    [author, replaying, drafts, scheduleSave],
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

  // Dbl-click a DRAFT node → inline rename input over the card. Committed tasks
  // are not renamable (their id is protocol-fixed). Author && !replaying only.
  const onNodeDoubleClick = useCallback(
    (e: ReactMouseEvent, node: Node) => {
      if (!author || replaying || node.type !== "draft") return;
      e.stopPropagation();
      const rawId = node.id.replace(/^draft:/, "");
      const { x, y } = stageXY(e);
      setRenaming({ id: rawId, x, y, value: rawId });
    },
    [author, replaying],
  );

  // stageXY maps a browser event to stage-relative coords for menu/rename
  // placement (the overlay is absolutely positioned within the stage div).
  const stageXY = (e: { clientX: number; clientY: number }) => {
    const r = stageRef.current?.getBoundingClientRect();
    return { x: e.clientX - (r?.left ?? 0), y: e.clientY - (r?.top ?? 0) };
  };

  // Context-menu openers (author && !replaying only — the menu component is
  // gated at the render site too).
  const onPaneContextMenu = useCallback(
    (e: ReactMouseEvent | MouseEvent) => {
      if (!author || replaying) return;
      e.preventDefault();
      const { x, y } = stageXY(e);
      // Flow position is "if cheap" — default placement is acceptable per the
      // plan; pass the stage coords through for a best-effort cursor hint.
      setMenu({ kind: "pane", x, y, flow: { x, y } });
    },
    [author, replaying],
  );

  const onNodeContextMenu = useCallback(
    (e: ReactMouseEvent, node: Node) => {
      if (!author || replaying) return;
      if (node.type !== "task" && node.type !== "draft") return;
      e.preventDefault();
      const { x, y } = stageXY(e);
      const rawId = node.id.replace(/^(task|draft):/, "");
      setMenu({ kind: "node", x, y, id: rawId, draft: node.type === "draft" });
    },
    [author, replaying],
  );

  // Inline draft rename commit: Enter validates the new id is a free slug, then
  // rewrites the draft id (and any dep references to it). Esc cancels.
  const commitRename = useCallback(() => {
    setRenaming((r) => {
      if (!r) return null;
      const next = r.value.trim();
      if (next && next !== r.id && SLUG_RE.test(next) && !existingTaskIds.includes(next)) {
        setDrafts((ds) =>
          ds.map((d) => ({
            ...d,
            id: d.id === r.id ? next : d.id,
            deps: d.deps.map((dep) => (dep === r.id ? next : dep)),
          })),
        );
      }
      return null;
    });
  }, [existingTaskIds, setDrafts]);

  // Del removes the selected DRAFTS (and selected draft features). Committed
  // tasks/features are never deletable here. Author && !replaying only.
  const deleteSelected = useCallback(() => {
    if (!author || replaying) return;
    const sel = new Set(selectedIds());
    if (sel.size === 0) return;
    const draftIds = new Set(
      [...sel].filter((id) => id.startsWith("draft:")).map((id) => id.replace(/^draft:/, "")),
    );
    const featIds = new Set(
      [...sel]
        .filter((id) => id.startsWith("feature:"))
        .map((id) => id.replace(/^feature:/, ""))
        // only DRAFT features (not committed) are deletable
        .filter((fid) => draftFeatures.some((df) => df.id === fid) && !state.features.some((f) => f.id === fid)),
    );
    if (draftIds.size > 0) {
      setDrafts((ds) => ds.filter((d) => !draftIds.has(d.id)));
    }
    if (featIds.size > 0) {
      setDraftFeatures((dfs) => dfs.filter((df) => !featIds.has(df.id)));
    }
  }, [author, replaying, draftFeatures, state.features, setDrafts, setDraftFeatures]);

  // Esc chain + Del key. Esc closes the menu first, then clears selection, then
  // closes the draft form. Del removes selected drafts. Both respect a typing
  // guard (don't hijack keys while an input/textarea is focused).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const el = document.activeElement as HTMLElement | null;
      const typing =
        !!el &&
        (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.isContentEditable);
      if (e.key === "Escape") {
        if (menu) { setMenu(null); return; }
        if (renaming) { setRenaming(null); return; }
        if (selectedIds().length > 0) {
          setNodes((nds) => nds.map((n) => (n.selected ? { ...n, selected: false } : n)));
          return;
        }
        if (newFeatureOpen) { setNewFeatureOpen(false); setNfId(""); setNfBranch(""); return; }
      }
      if ((e.key === "Delete" || e.key === "Backspace") && !typing && !renaming) {
        deleteSelected();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [menu, renaming, newFeatureOpen, deleteSelected]);

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

  // Comms lens result, derived from the snapshot only when the overlay is on.
  const commsResult = useMemo(
    () => (comms ? deriveComms(state) : null),
    [comms, state],
  );

  // display is the DISPLAY layer fed to React Flow. It starts from the
  // layout-pristine `nodes` state + base dep `edges`, then layers on (a) the
  // pulse class for live-changed tasks and (b) the comms overlay when the
  // toggle is on. ONE mergeComms call over the pulse-augmented nodes, so the
  // node and edge outputs can never disagree. Nothing here is ever written back
  // to the layout sidecar — the drag-persistence path reads the pristine
  // `nodes` state / layoutRef, not these.
  const display = useMemo(() => {
    let out = nodes;
    if (pulses && pulses.size > 0) {
      out = out.map((n) => {
        const raw = n.id.replace(/^(task|draft):/, "");
        if ((n.type === "task" || n.type === "draft") && pulses.has(raw)) {
          const roleVar = (n.data as { roleColor?: string }).roleColor ?? "--role-dev";
          return {
            ...n,
            className: [n.className, "pulse"].filter(Boolean).join(" "),
            style: { ...n.style, ["--pulse-color" as string]: `var(${roleVar})` },
          };
        }
        return n;
      });
    }
    const merged = commsResult
      ? mergeComms(out, edges, commsResult, state)
      : { nodes: out, edges };

    // Ant-crawl cap pass (T8): over the FINAL edge set (dep + any comms wait
    // edges), grant the scarce ant-animation budget to at most ANT_CAP edges,
    // comms WAIT edges first. reduced-motion → no ant anywhere. This is the
    // single place `data.ant` is set true; AntEdge only animates when it's true.
    const eligible = merged.edges
      .filter((e) => e.type === "ant")
      .map((e) => ({ id: e.id, kind: (e.data as { kind?: AntEdgeKind })?.kind ?? "dep" }));
    const antIds = assignAntFlags(eligible, prefersReducedMotion());
    const antEdges = merged.edges.map((e) =>
      e.type === "ant"
        ? { ...e, data: { ...(e.data ?? {}), ant: antIds.has(e.id) } }
        : e,
    );

    return { nodes: merged.nodes, edges: antEdges };
  }, [nodes, edges, pulses, commsResult, state]);
  const displayNodes = display.nodes;
  const displayEdges = display.edges;

  return (
    <div
      ref={stageRef}
      className={`canvas-stage relative flex-1${draggingDraft ? " dragging-draft" : ""}`}
      data-testid="canvas-root"
    >
      {/* Ambient stage layers (board2-canvas-v2 `.stage`): two role-color glows
          (page token sits behind via the .canvas-stage background) + a masked
          dot grid. Both purely decorative, behind the flow. */}
      <div className="canvas-grid" aria-hidden />

      {/* Frosted toolbar (top-left): author Feature/Task + Comms lens pill. */}
      <Toolbar
        author={author}
        comms={comms}
        onToggleComms={() => setComms((v) => !v)}
        newFeatureOpen={newFeatureOpen}
        onOpenNewFeature={() => { setNfId(nextId(existingFeatureIds, "f")); setNewFeatureOpen(true); }}
        onCloseNewFeature={() => { setNewFeatureOpen(false); setNfId(""); setNfBranch(""); }}
        nfId={nfId}
        setNfId={setNfId}
        nfBranch={nfBranch}
        setNfBranch={setNfBranch}
        nfValid={nfValid}
        nfIdBad={nfIdBad}
        nfBranchBad={nfBranchBad}
        onAddFeature={addFeature}
        onOpenNewTask={() => { setEditingDraft(undefined); setEditorOpen(true); }}
        newTaskDisabled={featureOptions.length === 0}
      />

      {/* Comms legend — frosted pill below the toolbar when the lens is on. */}
      {comms && (
        <div className="canvas-legend" data-testid="comms-legend">
          <span>
            <i style={{ borderColor: "var(--color-role-design)" }} />
            waiting
          </span>
          <span>
            <span className="inline-block h-2 w-2 rounded-sm border border-[var(--color-warn)]" />
            blocked
          </span>
          <span style={{ opacity: 0.6 }}>◐ idle</span>
        </div>
      )}

      {notice && (
        <div
          data-testid="canvas-notice"
          className="absolute left-1/2 top-2 z-10 -translate-x-1/2 rounded-md border border-[var(--color-warn)] bg-[var(--color-bg-surface)] px-3 py-1 text-xs text-[var(--color-warn)]"
        >
          {notice}
        </div>
      )}

      <ReactFlow
        nodes={displayNodes}
        edges={displayEdges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        onNodesChange={onNodesChange}
        onNodeDragStart={onNodeDragStart}
        onNodeDrag={onNodeDrag}
        onNodeDragStop={onNodeDragStop}
        onNodeClick={onNodeClick}
        onNodeDoubleClick={onNodeDoubleClick}
        onNodeContextMenu={onNodeContextMenu}
        onPaneContextMenu={onPaneContextMenu}
        onPaneClick={() => setMenu(null)}
        onConnect={onConnect}
        isValidConnection={isValidConnection}
        connectionRadius={30}
        nodesDraggable={author}
        nodesConnectable={author}
        // Marquee multi-select on Shift+drag (React Flow's default
        // selectionKeyCode), plain left-drag still pans the canvas, and
        // right-click is left free for the context menu (panOnDrag must NOT
        // include button 2, or React Flow swallows the pane contextmenu). Node
        // drag-to-dispatch is a NODE drag, unaffected by pane panOnDrag.
        selectionKeyCode="Shift"
        panOnDrag
        // React Flow's built-in Backspace delete would remove ANY selected node
        // (committed tasks/seats included) via applyNodeChanges, bypassing the
        // draft-only deleteSelected path below. Deletion is exclusively ours.
        deleteKeyCode={null}
        fitView
        proOptions={{ hideAttribution: true }}
        colorMode="dark"
        style={{ background: "transparent" }}
      >
        <FitOnEntry project={project} ready={displayNodes.length > 0} />
        <Hud />
        <SnapGuides guides={guides} />
      </ReactFlow>

      {/* Context menu (author && !replaying) — board2-canvas-v2 `.ctx`. */}
      {menu && author && !replaying && (
        <ContextMenu
          target={menu}
          onClose={() => setMenu(null)}
          onNewFeature={() => { setNfId(nextId(existingFeatureIds, "f")); setNewFeatureOpen(true); }}
          onNewTask={() => { setEditingDraft(undefined); setEditorOpen(true); }}
          onDispatch={(id) => openDispatchFor(id)}
          onEdit={(id) => {
            const d = drafts.find((x) => x.id === id);
            if (d) { setEditingDraft(d); setEditorOpen(true); }
          }}
          onViewSpec={(id) => onSelectTask?.(id)}
          onDelete={(id) => setDrafts((ds) => ds.filter((x) => x.id !== id))}
        />
      )}

      {/* Inline draft rename (author && !replaying). */}
      {renaming && author && !replaying && (
        <input
          className="canvas-rename"
          data-testid="canvas-rename"
          autoFocus
          style={{ left: renaming.x, top: renaming.y, width: 120 }}
          value={renaming.value}
          onChange={(e) => setRenaming((r) => (r ? { ...r, value: e.target.value } : r))}
          onKeyDown={(e) => {
            if (e.key === "Enter") { e.preventDefault(); commitRename(); }
            else if (e.key === "Escape") { e.preventDefault(); setRenaming(null); }
          }}
          onBlur={() => setRenaming(null)}
        />
      )}

      {editorOpen && (
        <TaskEditor
          initial={editingDraft}
          defaultId={nextId(existingTaskIds, "t")}
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
