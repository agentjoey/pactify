export interface ToolbarProps {
  author: boolean;
  newFeatureOpen: boolean;
  onOpenNewFeature: () => void;
  onCloseNewFeature: () => void;
  nfId: string;
  setNfId: (v: string) => void;
  nfBranch: string;
  setNfBranch: (v: string) => void;
  nfValid: boolean;
  nfIdBad: boolean;
  nfBranchBad: boolean;
  onAddFeature: () => void;
  onOpenNewTask: () => void;
  newTaskDisabled: boolean;
}

export function Toolbar({
  author,
  newFeatureOpen,
  onOpenNewFeature,
  onCloseNewFeature,
  nfId,
  setNfId,
  nfBranch,
  setNfBranch,
  nfValid,
  nfIdBad,
  nfBranchBad,
  onAddFeature,
  onOpenNewTask,
  newTaskDisabled,
}: ToolbarProps) {
  return (
    <div className="canvas-tbar" data-testid="canvas-toolbar">
      {author &&
        (newFeatureOpen ? (
          <div className="flex items-center gap-1">
            <input
              aria-label="feature id"
              className="w-24 rounded-md border border-[var(--color-border-strong)] bg-[var(--color-bg-page)] px-1.5 py-0.5 text-xs text-[var(--color-text-1)]"
              placeholder="feature id"
              value={nfId}
              onChange={(e) => setNfId(e.target.value)}
            />
            <input
              aria-label="feature branch"
              className="mono w-28 rounded-md border border-[var(--color-border-strong)] bg-[var(--color-bg-page)] px-1.5 py-0.5 text-xs text-[var(--color-text-1)]"
              placeholder="feat/x"
              value={nfBranch}
              onChange={(e) => setNfBranch(e.target.value)}
            />
            <button
              className="rounded-md bg-[var(--color-success)] px-2 py-0.5 text-xs font-semibold text-[#0d0e14] disabled:opacity-50"
              onClick={onAddFeature}
              disabled={!nfValid}
            >
              Add
            </button>
            <button
              className="tbtn"
              aria-label="cancel new feature"
              onClick={onCloseNewFeature}
            >
              ✕
            </button>
            {(nfIdBad || nfBranchBad) && (
              <span className="text-[10px] text-[var(--color-danger)]">
                slug: [a-z0-9][a-z0-9-]*
              </span>
            )}
          </div>
        ) : (
          <>
            <button className="tbtn" onClick={onOpenNewFeature}>
              <span className="ic">▢</span>Feature
            </button>
            <button
              className="tbtn"
              onClick={onOpenNewTask}
              disabled={newTaskDisabled}
            >
              <span className="ic">＋</span>Task
            </button>
          </>
        ))}
    </div>
  );
}
