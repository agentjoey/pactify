import { useCallback, useEffect, useMemo, useRef, useState, type Dispatch, type MouseEvent as ReactMouseEvent, type SetStateAction } from "react";
import {
  ReactFlow,
  useReactFlow,
  useViewport,
  type Node,
  type Edge,
  type NodeTypes,
  type EdgeTypes,
  type NodeChange,
  type Connection,
  type FinalConnectionState,
  applyNodeChanges,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import type { State } from "../lib/types";
import {
  deriveGraph,
  placeNew,
  mergeNodes,
  normalizeLayout,
  LAYOUT_V,
  applyConnect,
  assignAntFlags,
  isValidDep,
  mergeOfficePos,
  nextId,
  CHILD_W,
  type Draft,
  type DraftFeature,
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
import { OfficeView } from "./canvas/OfficeView";
import { ConnectionLine } from "./canvas/ConnectionLine";
import { ConnectingFlag } from "./canvas/ConnectingFlag";
import { CanvasSkeleton } from "./Skeleton";

// CanvasMode picks which surface the stage shows. Office (agents-as-subject) is
// the DEFAULT landing (spec §3); Plan is the existing feature/task-frame canvas.
export type CanvasMode = "office" | "plan";

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

// dragDispatchSeat decides whether a drag-stop is a drag-to-dispatch gesture and,
// if so, returns the overlapped seat node. Drag-to-dispatch is a SINGLE-draft
// gesture: it fires only when the primary dragged node is a draft AND exactly one
// node moved (movedCount === 1). A multi-select group drag (draft + others)
// returns null even if the draft overlaps a seat, so the caller persists every
// dragged node's position instead of opening the modal. Pure + exported so the
// guard is unit-testable without driving a real RF drag (impossible under jsdom).
export function dragDispatchSeat(
  node: Node,
  movedCount: number,
  allNodes: Node[],
): Node | null {
  if (node.type !== "draft" || movedCount !== 1) return null;
  const dragBox = nodeBounds(node, allNodes);
  return (
    allNodes.find(
      (n) => n.type === "seat" && overlaps(dragBox, nodeBounds(n, allNodes)),
    ) ?? null
  );
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
  initialMode = "office",
  loading,
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
  // Session-local landing mode. Defaults to "office" (the spec-default landing);
  // tests that exercise Plan-mode behaviors pass initialMode="plan" so their
  // semantics are unchanged — the mode segment switch is otherwise the only way
  // in. Mode is NOT persisted to layout.
  initialMode?: CanvasMode;
  // First-load only: a project is current but its first snapshot hasn't landed
  // yet → show a dim skeleton stage instead of an empty graph.
  loading?: boolean;
}) {
  // Office | Plan — session-local, not persisted.
  const [mode, setMode] = useState<CanvasMode>(initialMode);
  // Comms overlay toggle — component-local, default OFF. It's a display lens
  // (derived wait edges + node markers merged into the rendered graph), never
  // persisted to layout.json.
  const [comms, setComms] = useState(false);
  const [layout, setLayout] = useState<LayoutJSON>({});
  // layoutLoaded gates materialization until THIS project's server layout has
  // landed. Without it, the materialization effect fires against an empty (or
  // the previous project's) layout the instant the project changes — placeNew
  // would read a stale layoutRef and PUT a default grid over the server's real
  // positions (reverse race). The load effect clears it to false synchronously
  // on every project switch and the load promise flips it true (success OR
  // failure), after which the effect re-runs against the real layout.
  const [layoutLoaded, setLayoutLoaded] = useState(false);
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
  // Feature focus mode (T15): the feature id whose frame header was clicked. When
  // set, the RF wrapper gets a `focus-active` class + a `data-focus` attr; CSS
  // dims/desaturates every node/edge NOT belonging to that feature. Display-only
  // — it never touches layout or the derived graph. null = no focus.
  const [focusFeature, setFocusFeature] = useState<string | null>(null);
  // Dispatch target: a draft dropped onto a seat.
  const [dispatch, setDispatch] = useState<{ draft: Draft; owner?: string } | undefined>(undefined);
  const [draggingDraft, setDraggingDraft] = useState(false);
  // A connection drag is in progress (lifted from ConnectingFlag, which reads
  // useConnection inside <ReactFlow>). Folded into the stage className below so
  // it survives Canvas re-renders — a direct classList.toggle on the stage would
  // be clobbered the next time the controlled className is rewritten.
  const [connecting, setConnecting] = useState(false);

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
  // the primary path; the button exists for discoverability). useCallback over
  // [replaying, drafts] so it always closes over the CURRENT drafts list — a
  // stale closure would resolve a deleted/old draft (or miss a new one) when the
  // injected onDispatch fires. injectCallbacks lists it as a dep, so draft nodes
  // re-mint their data when drafts changes (draft data already churns on every
  // graph change; non-draft node references stay stable and are unaffected).
  const openDispatchFor = useCallback((draftId: string) => {
    if (replaying) return; // read-only replay: dispatch is unreachable
    const d = drafts.find((x) => x.id === draftId);
    if (d) setDispatch({ draft: d });
  }, [replaying, drafts]);

  // Load saved layout on mount / project change.
  useEffect(() => {
    let alive = true;
    // Per-project display state must not leak across a project switch (Canvas
    // is not keyed by project): a stale focus would dim the entire new graph.
    setFocusFeature(null);
    setMenu(null);
    setRenaming(null);
    // Clear the OLD project's layout + nodes and re-gate materialization
    // SYNCHRONOUSLY. This wipes the previous project's layoutRef so a placeNew
    // running before the new layout lands can't read stale positions, and
    // blocks the materialization effect (layoutLoaded=false) until the server
    // layout for THIS project resolves.
    setLayout({ v: LAYOUT_V });
    setNodes([]);
    setLayoutLoaded(false);
    // Normalize the opaque server blob into a v2 layout: a legacy (no `v`) layout
    // is discarded so every node re-materializes from the deterministic grid
    // (spec §1.3) — a stale v1 absolute-coord blob must never poison v2. Flip
    // layoutLoaded true on BOTH success and failure: a load error still yields a
    // usable default-grid materialization, it just won't merge server positions.
    getLayout(project)
      .then((l) => { if (alive) { setLayout(normalizeLayout(l)); setLayoutLoaded(true); } })
      .catch(() => { if (alive) { setLayout({ v: LAYOUT_V }); setLayoutLoaded(true); } });
    return () => { alive = false; };
  }, [project]);

  // Derive the position-LESS graph (node identities + data + dep edges) from
  // protocol state + drafts + draft-features. Position is NOT here — layout owns
  // it (materialized once by placeNew, thereafter changed only by user drag).
  // SSE state changes re-run this; the materialization effect below folds the new
  // graph into the RF node array by merge-by-id so unmoved nodes never teleport.
  const graph = useMemo(
    () => deriveGraph(state, drafts, draftFeatures),
    [state, drafts, draftFeatures],
  );

  // Dep edges render as ant-crawl edges (type:"ant"): a carrier ant + cargo
  // cube crawls source→target. The base dashed look is unchanged (style
  // passthrough); `ant` is decided by the cap pass in `display` below.
  const edges: Edge[] = useMemo(
    () =>
      graph.edges.map((e) => ({
        id: e.id,
        source: e.source,
        target: e.target,
        type: "ant",
        style: { stroke: "#484f58", strokeDasharray: "5 4" },
        data: { kind: "dep" as const, color: "#484f58", ant: false },
      })),
    [graph.edges],
  );

  // injectCallbacks re-attaches the per-node callbacks (draft onDispatch /
  // feature onFocus) and the stale marker onto the merged node array. mergeNodes
  // keeps a node's `data` REFERENCE stable when nothing changed (so RF skips
  // re-render); to preserve that, we only produce a NEW data object for a node
  // when its injected fields actually change — a stale flag only flips when the
  // node's stale status differs from what's already on the node. Unchanged nodes
  // pass through by reference.
  const injectCallbacks = useCallback(
    (merged: Node[]): Node[] =>
      merged.map((n) => {
        if (n.type === "draft") {
          // onDispatch closes over the raw id; re-attaching every merge is fine
          // (data already churns for drafts) — keep it simple and correct.
          return { ...n, data: { ...n.data, onDispatch: () => openDispatchFor(n.id.replace(/^draft:/, "")) } };
        }
        if (n.type === "feature") {
          const fid = (n.data as { id?: string }).id ?? n.id.replace(/^feature:/, "");
          return { ...n, data: { ...n.data, onFocus: () => setFocusFeature((cur) => (cur === fid ? null : fid)) } };
        }
        if (n.type === "task") {
          const raw = n.id.replace(/^task:/, "");
          const want = !!staleTasks?.has(raw);
          const has = !!(n.data as { stale?: boolean }).stale;
          // Only mint a new data object when the stale state actually changed —
          // otherwise return the node by reference so RF's merge stays stable.
          if (want === has) return n;
          return { ...n, data: { ...n.data, stale: want } };
        }
        return n;
      }),
    [staleTasks, openDispatchFor],
  );

  // schedulePut debounces a layout PUT WITHOUT touching local state. The
  // materialization effect uses this: it always setLayout()s locally (so every
  // surface — observer/replay included — renders the freshly placed nodes), then
  // separately schedules a PUT only when author && !replaying. scheduleSave below
  // (setLayout + PUT) is the combined path the drag/office handlers still use.
  const schedulePut = useCallback((next: LayoutJSON) => {
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => {
      putLayout(project, next).catch(() => {});
    }, 800);
  }, [project]);

  // Debounced layout persistence (single timer): local state + PUT together.
  const scheduleSave = useCallback((next: LayoutJSON) => {
    setLayout(next);
    schedulePut(next);
  }, [schedulePut]);

  useEffect(() => () => { if (timer.current) clearTimeout(timer.current); }, []);

  // Single materialization effect (spec §1.4). Whenever the graph changes (SSE
  // snapshot, drafts, draft-features) or stale set changes:
  //   1. placeNew assigns a one-time position to every node with NO layout entry.
  //   2. If any new entries, fold them into layout: setLayout LOCALLY ALWAYS (so
  //      observer/replay still render), and PUT only when author && !replaying.
  //   3. mergeNodes(prev, graph, next) folds graph + layout into the RF node
  //      array by merge-by-id — unmoved nodes keep their prev position/measured/
  //      selected/dragging, so a drag-in-progress node is never teleported.
  // Ordering contract: placeNew runs BEFORE mergeNodes, so a brand-new node has a
  // layout position to merge in (mergeNodes falls back to {0,0} only if it didn't).
  useEffect(() => {
    // Gate: don't materialize until THIS project's server layout has landed.
    // Before that, layoutRef holds the cleared default ({v}) — placeNew would
    // grid-place every node and (author) PUT that default over the server's
    // real positions. Once getLayout resolves, layoutLoaded flips true and this
    // effect re-runs against the real layout, merging server positions.
    if (!layoutLoaded) return;
    const add = placeNew(layoutRef.current, graph);
    let layoutForMerge = layoutRef.current;
    if (Object.keys(add).length > 0) {
      const next: LayoutJSON = {
        ...layoutRef.current,
        v: LAYOUT_V,
        positions: { ...(layoutRef.current.positions ?? {}), ...add },
      };
      layoutForMerge = next;
      setLayout(next); // local materialization ALWAYS (every surface renders)
      if (author && !replaying) schedulePut(next);
    }
    setNodes((prev) => injectCallbacks(mergeNodes(prev, graph, layoutForMerge)));
  }, [graph, injectCallbacks, author, replaying, schedulePut, layoutLoaded]);

  // Office desk position persistence (T10). Writes ONLY the additive `office`
  // sidecar key — `positions` (Plan-mode coords) is carried through untouched —
  // and rides the same debounced PUT path as Plan drags.
  const saveOfficePos = useCallback(
    (seatId: string, pos: { x: number; y: number }) => {
      scheduleSave(mergeOfficePos(layoutRef.current, seatId, pos));
    },
    [scheduleSave],
  );

  // Office desk materialization (T4, spec §3). OfficeView reports the freshly
  // placed seats (those with no `office` entry) once on first render; we fold
  // them into layout.office — mirroring the Plan placeNew fold: setLayout LOCALLY
  // ALWAYS (so observer/replay render the same deterministic grid), and PUT only
  // when author && !replaying. The additive `office` key never disturbs the Plan
  // `positions`. Idempotent on OfficeView's side, so a re-fire with the same seats
  // can't happen, but we still guard against an empty batch.
  const materializeOffice = useCallback(
    (entries: Record<string, { x: number; y: number }>) => {
      if (Object.keys(entries).length === 0) return;
      const next: LayoutJSON = {
        ...layoutRef.current,
        v: LAYOUT_V,
        office: { ...(layoutRef.current.office ?? {}), ...entries },
      };
      setLayout(next); // local materialization ALWAYS (every surface renders)
      if (author && !replaying) schedulePut(next);
    },
    [author, replaying, schedulePut],
  );

  // Office drop/click-to-dispatch: open DispatchModal with the dropped draft +
  // the target seat as owner (reuses the exact Plan dispatch flow).
  const dispatchDraftToSeat = useCallback(
    (d: Draft, seatId: string) => {
      if (replaying) return;
      setDispatch({ draft: d, owner: seatId });
    },
    [replaying],
  );

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
  // human notice. NOTE: in practice the notice branches below are NEVER reached —
  // isValidConnection rejects every invalid landing, so RF refuses to call
  // onConnect for them at all (the drop only routes through onConnectEnd, which
  // owns the notices). They're kept here as a defensive fallback in case a build
  // ever fires onConnect without consulting isValidConnection.
  const onConnect = useCallback(
    (c: Connection) => {
      if (!author) return;
      const raw = (id: string) => id.replace(/^(task|draft):/, "");
      const from = raw(c.source);
      const to = raw(c.target);
      if (from === to) return;
      if (committedTaskIds.has(to)) {
        flashNotice("依赖在 assign 时已固定,不能再改");
        return;
      }
      if (featureOfId(from) !== featureOfId(to)) {
        flashNotice("依赖必须和任务在同一个 feature 内");
        return;
      }
      if (!isValidDep(depGraph, from, to)) {
        flashNotice("依赖关系会形成环");
        return;
      }
      setDrafts((ds) => applyConnect(ds, c.source, c.target));
    },
    [author, committedTaskIds, featureOfId, flashNotice, depGraph, setDrafts],
  );

  // onConnectEnd: RF's isValidConnection rejects EVERY invalid landing during the
  // drag, so RF refuses to call onConnect for any of them — all three rule
  // notices ("已固定" / 同 feature / 成环) would be unreachable through onConnect.
  // This handler is therefore the SOLE surface for those notices: on release we
  // re-run the same three-rule routing here (same order as onConnect: committed →
  // cross-feature → cycle) and flash the matching cue. Guards: no author / replay
  // → nothing; isValid === null means the pointer was released over empty space
  // (not over a handle) — no notice at all; isValid === false is a real invalid
  // landing over a target handle, which is what we route on.
  const onConnectEnd = useCallback(
    (_e: MouseEvent | TouchEvent, cs: FinalConnectionState) => {
      if (!author || replaying) return;
      if (cs.isValid !== false) return; // null = released in empty space → no notice
      const fromNode = cs.fromNode;
      const toNode = cs.toNode;
      if (!fromNode || !toNode) return;
      const raw = (id: string) => id.replace(/^(task|draft):/, "");
      const from = raw(fromNode.id);
      const to = raw(toNode.id);
      if (from === to) return;
      if (committedTaskIds.has(to)) {
        flashNotice("依赖在 assign 时已固定,不能再改");
        return;
      }
      if (featureOfId(from) !== featureOfId(to)) {
        flashNotice("依赖必须和任务在同一个 feature 内");
        return;
      }
      if (!isValidDep(depGraph, from, to)) {
        flashNotice("依赖关系会形成环");
      }
    },
    [author, replaying, committedTaskIds, featureOfId, flashNotice, depGraph],
  );

  // Persist dragged nodes' new positions into the layout sidecar after a drag.
  // Layout v2 stores RF-NATIVE coords directly: top-level nodes absolute, child
  // nodes parent-relative — exactly what React Flow reports in node.position, so
  // there is ZERO coordinate conversion here (the v1 childToAbsolute rebase is
  // gone). RF v12 passes the DRAGGED selection as the handler's third argument;
  // we write one layout entry per dragged node and save once.
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
    (_e: unknown, node: Node, dragged: Node[]) => {
      setDraggingDraft(false);
      setGuides([]); // clear snap guides
      // Replay is read-only and observe-only dashboards never persist drags.
      if (replaying || !author) return;
      // RF v12: `dragged` is the moved selection only — use the live full list
      // for parent/seat resolution. Fall back to the single node for callers
      // (or RF builds) that don't pass the third arg.
      const allNodes = nodesRef.current;
      const moved = dragged && dragged.length > 0 ? dragged : [node];

      // Drag-to-dispatch is a single-draft gesture: a draft dropped on a seat
      // dispatches instead of saving a layout move. dragDispatchSeat guards on
      // moved.length === 1, so a multi-select group drag (e.g. a draft + a task)
      // NEVER dispatches — it falls through to persist every dragged node's
      // position. Without the guard a group drag whose draft happens to overlap a
      // seat would open the modal and swallow the other nodes' position writes.
      const seat = dragDispatchSeat(node, moved.length, allNodes);
      if (seat) {
        const rawId = node.id.replace(/^draft:/, "");
        const d = drafts.find((x) => x.id === rawId);
        if (d) {
          setDispatch({ draft: d, owner: seat.id.replace(/^seat:/, "") });
          return; // don't persist a layout move for a dispatch gesture
        }
      }

      // Write one layout entry per dragged node. node.position is already the
      // RF-native value layout v2 stores (top-level absolute / child parent-
      // relative) — zero conversion. Save once for the whole selection.
      const positions = { ...(layoutRef.current.positions ?? {}) };
      for (const n of moved) {
        positions[n.id] = { x: n.position.x, y: n.position.y };
      }
      scheduleSave({ ...layoutRef.current, v: LAYOUT_V, positions });
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

  // Esc chain + Del key. Esc resolves in order: context menu → inline rename →
  // selection → feature focus → draft form. Del removes selected drafts. Both
  // respect a typing guard (don't hijack keys while an input/textarea is focused).
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
        if (focusFeature) { setFocusFeature(null); return; }
        if (newFeatureOpen) { setNewFeatureOpen(false); setNfId(""); setNfBranch(""); return; }
      }
      if ((e.key === "Delete" || e.key === "Backspace") && !typing && !renaming) {
        deleteSelected();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [menu, renaming, newFeatureOpen, focusFeature, deleteSelected]);

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

    // Feature focus dimming (T15): when a feature is focused, every node NOT in
    // its subtree (the frame + its child task/draft nodes) gets a `focus-dim`
    // class; same for edges whose endpoints leave the focused subtree. CSS owns
    // the visual (opacity 25% + desaturate) — this never touches layout.
    if (!focusFeature) return { nodes: merged.nodes, edges: antEdges };
    const frameId = `feature:${focusFeature}`;
    const inFocus = (n: Node) => n.id === frameId || n.parentId === frameId;
    const focusedNodeIds = new Set(merged.nodes.filter(inFocus).map((n) => n.id));
    const dimNode = (n: Node): Node =>
      inFocus(n)
        ? n
        : { ...n, className: [n.className, "focus-dim"].filter(Boolean).join(" ") };
    const dimEdge = (e: Edge): Edge =>
      focusedNodeIds.has(e.source) && focusedNodeIds.has(e.target)
        ? e
        : { ...e, className: [e.className, "focus-dim"].filter(Boolean).join(" ") };
    return {
      nodes: merged.nodes.map(dimNode),
      edges: antEdges.map(dimEdge),
    };
  }, [nodes, edges, pulses, commsResult, state, focusFeature]);
  const displayNodes = display.nodes;
  const displayEdges = display.edges;

  if (loading) return <CanvasSkeleton />;

  return (
    <div
      ref={stageRef}
      className={`canvas-stage relative flex-1${author && !replaying ? " author" : ""}${draggingDraft ? " dragging-draft" : ""}${focusFeature ? " focus-active" : ""}${connecting ? " connecting" : ""}`}
      data-testid="canvas-root"
      data-focus={focusFeature ?? undefined}
    >
      {/* Ambient stage layers (board2-canvas-v2 `.stage`): two role-color glows
          (page token sits behind via the .canvas-stage background) + a masked
          dot grid. Both purely decorative, behind the flow. */}
      <div className="canvas-grid" aria-hidden />

      {/* Mode segment (top-left) — Office | Plan. Office is the default landing
          (spec §3). Session-local; not persisted. */}
      <div className="office-modeseg" data-testid="canvas-modeseg">
        <button
          className={mode === "plan" ? "on" : ""}
          data-testid="mode-plan"
          onClick={() => setMode("plan")}
        >
          Plan
        </button>
        <button
          className={mode === "office" ? "on" : ""}
          data-testid="mode-office"
          onClick={() => setMode("office")}
        >
          Office
        </button>
      </div>

      {/* Authoring toolbar — SHARED across both modes (Task 4, spec §3): New
          Feature / New Task render in Office and Plan alike; the comms lens pill
          is Plan-only (showComms). The inline New-feature form it opens is also
          shared (rendered just below), so the office surface can author features.
          Gated on layoutLoaded only for OfficeView; the Toolbar itself is always
          available. */}
      <Toolbar
        author={author}
        comms={comms}
        showComms={mode === "plan"}
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

      {mode === "office" && layoutLoaded && (
        <OfficeView
          state={state}
          layout={layout}
          author={author}
          replaying={replaying}
          pulses={pulses}
          drafts={drafts}
          onSaveOffice={saveOfficePos}
          onSelectTask={onSelectTask}
          onDispatchDraft={dispatchDraftToSeat}
          onOpenNewTask={() => { setEditingDraft(undefined); setEditorOpen(true); }}
          onOpenNewFeature={() => { setNfId(nextId(existingFeatureIds, "f")); setNewFeatureOpen(true); }}
          onMaterializeOffice={materializeOffice}
        />
      )}

      {mode === "plan" && (
      <>
      {/* Feature focus chip (T15): shown while a feature is focused; the ✕ exits
          (same as Esc). Sits in the top chrome row. */}
      {focusFeature && (
        <button
          type="button"
          data-testid="focus-chip"
          className="absolute right-3 top-2 z-20 flex items-center gap-1.5 rounded-full border border-[var(--color-border-subtle)] bg-[var(--color-bg-raised)] px-2.5 py-1 text-[11px] text-[var(--color-text-1)] shadow-[var(--shadow-raised)] hover:border-[var(--color-text-3)]"
          onClick={() => setFocusFeature(null)}
          title="退出聚焦(Esc)"
        >
          <span>
            focusing <span className="mono">«{focusFeature}»</span>
          </span>
          <span aria-hidden className="text-[var(--color-text-3)]">✕</span>
        </button>
      )}

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
        onConnectEnd={onConnectEnd}
        isValidConnection={isValidConnection}
        connectionLineComponent={ConnectionLine}
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
        <ConnectingFlag onChange={setConnecting} />
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
      </>
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
