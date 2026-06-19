import { useCallback, useEffect, useMemo, useRef, useState, type Dispatch, type SetStateAction } from "react";
import type { State } from "../lib/types";
import {
  normalizeLayout,
  LAYOUT_V,
  mergeOfficePos,
  type Draft,
  type DraftFeature,
  type LayoutJSON,
} from "../lib/canvas";
import { getLayout, putLayout } from "../lib/api";
import { TaskEditor, type FeatureOption } from "./TaskEditor";
import { DispatchModal } from "./DispatchModal";
import { Toolbar } from "./canvas/Toolbar";
import { SeatRoster } from "./canvas/SeatRoster";
import { OfficeView } from "./canvas/OfficeView";
import { CanvasSkeleton } from "./Skeleton";

const SLUG_RE = /^[a-z0-9][a-z0-9-]*$/;

// nextId returns the smallest `${prefix}${N}` (N≥1) not already present in
// `existing`. Used to seed the draft task/feature id forms with a sensible
// default so authors don't have to hand-type ids (the value stays editable and
// slug validation is unchanged).
function nextId(existing: string[], prefix: string): string {
  const taken = new Set<number>();
  const re = new RegExp(`^${prefix}([1-9][0-9]*)$`);
  for (const id of existing) {
    const m = re.exec(id);
    if (m) taken.add(Number(m[1]));
  }
  let n = 1;
  while (taken.has(n)) n++;
  return `${prefix}${n}`;
}

export function Canvas({
  project,
  state,
  author,
  replaying,
  pulses,
  onSelectTask,
  drafts,
  setDrafts,
  draftFeatures,
  setDraftFeatures,
  loading,
}: {
  project: string;
  state: State;
  author: boolean;
  replaying?: boolean;
  pulses?: Set<string>;
  onSelectTask?: (id: string) => void;
  drafts: Draft[];
  setDrafts: Dispatch<SetStateAction<Draft[]>>;
  draftFeatures: DraftFeature[];
  setDraftFeatures: Dispatch<SetStateAction<DraftFeature[]>>;
  loading?: boolean;
}) {
  const [layout, setLayout] = useState<LayoutJSON>({ v: LAYOUT_V });
  const [layoutLoaded, setLayoutLoaded] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const layoutRef = useRef<LayoutJSON>({ v: LAYOUT_V });

  const [editorOpen, setEditorOpen] = useState(false);
  const [editingDraft, setEditingDraft] = useState<Draft | undefined>(undefined);
  const [newFeatureOpen, setNewFeatureOpen] = useState(false);
  const [nfId, setNfId] = useState("");
  const [nfBranch, setNfBranch] = useState("");
  const [dispatch, setDispatch] = useState<{ draft: Draft; owner?: string } | undefined>(undefined);

  useEffect(() => {
    layoutRef.current = layout;
  }, [layout]);

  useEffect(() => {
    let alive = true;
    getLayout(project)
      .then((l) => {
        if (!alive) return;
        setLayout(normalizeLayout(l));
        setLayoutLoaded(true);
      })
      .catch(() => {
        if (!alive) return;
        setLayout({ v: LAYOUT_V });
        setLayoutLoaded(true);
      });
    return () => { alive = false; };
  }, [project]);

  const schedulePut = useCallback((next: LayoutJSON) => {
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => {
      putLayout(project, next).catch(() => {});
    }, 800);
  }, [project]);

  const scheduleSave = useCallback((next: LayoutJSON) => {
    setLayout(next);
    schedulePut(next);
  }, [schedulePut]);

  useEffect(() => () => { if (timer.current) clearTimeout(timer.current); }, []);

  const saveOfficePos = useCallback(
    (seatId: string, pos: { x: number; y: number }) => {
      scheduleSave(mergeOfficePos(layoutRef.current, seatId, pos));
    },
    [scheduleSave],
  );

  const materializeOffice = useCallback(
    (entries: Record<string, { x: number; y: number }>) => {
      if (Object.keys(entries).length === 0) return;
      const next: LayoutJSON = {
        ...layoutRef.current,
        v: LAYOUT_V,
        office: { ...(layoutRef.current.office ?? {}), ...entries },
      };
      scheduleSave(next);
    },
    [scheduleSave],
  );

  const dispatchDraftToSeat = useCallback(
    (d: Draft, seatId: string) => {
      if (replaying) return;
      setDispatch({ draft: d, owner: seatId });
    },
    [replaying],
  );

  const featureOptions: FeatureOption[] = useMemo(() => {
    const opts: FeatureOption[] = state.features.map((f) => ({ id: f.id, label: f.id }));
    for (const df of draftFeatures) {
      if (!state.features.some((f) => f.id === df.id)) {
        opts.push({ id: df.id, label: `${df.id} (draft)` });
      }
    }
    return opts;
  }, [state.features, draftFeatures]);

  const existingTaskIds = useMemo(() => {
    const ids: string[] = [];
    for (const f of state.features) for (const t of f.tasks) ids.push(t.id);
    for (const d of drafts) ids.push(d.id);
    return ids;
  }, [state.features, drafts]);

  const existingFeatureIds = useMemo(() => {
    const ids = state.features.map((f) => f.id);
    for (const df of draftFeatures) ids.push(df.id);
    return ids;
  }, [state.features, draftFeatures]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        if (newFeatureOpen) { setNewFeatureOpen(false); setNfId(""); setNfBranch(""); return; }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [newFeatureOpen]);

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

  if (loading) return <CanvasSkeleton />;

  return (
    <div
      className={`canvas-stage relative flex-1${author && !replaying ? " author" : ""}`}
      data-testid="canvas-root"
    >
      <div className="canvas-grid" aria-hidden />

      <SeatRoster agents={state.agents} />

      <Toolbar
        author={author}
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

      {layoutLoaded && (
        <OfficeView
          state={state}
          layout={layout}
          project={project}
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
            setDrafts((ds) => ds.filter((x) => x.id !== dispatch.draft.id));
            setDispatch(undefined);
          }}
          onClose={() => setDispatch(undefined)}
        />
      )}
    </div>
  );
}
