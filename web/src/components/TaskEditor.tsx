import { useState } from "react";
import type { Draft } from "../lib/canvas";

// TASK_ID_RE / FEATURE rules mirror the protocol slug constraints: a task id is
// a lowercase slug, kebab allowed but must start alphanumeric.
const TASK_ID_RE = /^[a-z0-9][a-z0-9-]*$/;

export type FeatureOption = { id: string; label: string };

// TaskEditor is an author-only modal for composing a Draft (a task that has not
// been assigned yet — NO server call). It opens blank for "New task" or
// prefilled when an existing draft node is clicked (editing mode), in which
// case a delete-draft button appears. Save returns the assembled Draft to the
// caller, which owns the local drafts list.
export function TaskEditor({
  initial,
  features,
  existingIds,
  onSave,
  onDelete,
  onClose,
}: {
  initial?: Draft;
  features: FeatureOption[];
  existingIds: string[]; // task ids already taken (committed + other drafts)
  onSave: (d: Draft) => void;
  onDelete?: () => void;
  onClose: () => void;
}) {
  const editing = !!initial;
  const [id, setId] = useState(initial?.id ?? "");
  const [feature, setFeature] = useState(initial?.feature ?? features[0]?.id ?? "");
  const [specMd, setSpecMd] = useState(initial?.specMd ?? "");

  const idTrimmed = id.trim();
  const slugBad = idTrimmed.length > 0 && !TASK_ID_RE.test(idTrimmed);
  // In edit mode the draft keeps its own id, so a clash with itself is allowed.
  const dup = !editing && existingIds.includes(idTrimmed);
  const canSave = TASK_ID_RE.test(idTrimmed) && !dup && !!feature;

  const save = () => {
    if (!canSave) return;
    onSave({ id: idTrimmed, feature, specMd, deps: initial?.deps ?? [] });
  };

  return (
    <div
      data-testid="task-editor"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60"
      onClick={onClose}
    >
      <div
        className="w-[520px] max-w-[92vw] rounded-lg border border-[#30363d] bg-[#161b22] p-4 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-sm font-semibold text-[#e6edf3]">
            {editing ? `Edit draft · ${initial!.id}` : "New task"}
          </h2>
          <button
            className="text-xs text-[#8b949e] hover:text-[#e6edf3]"
            onClick={onClose}
            aria-label="close"
          >
            ✕
          </button>
        </div>

        <label className="block text-[10px] font-semibold uppercase tracking-wide text-[#8b949e]">
          Task id
        </label>
        <input
          aria-label="task id"
          className="mt-1 w-full rounded border border-[#30363d] bg-[#0d1117] px-2 py-1 font-mono text-sm text-[#e6edf3] disabled:opacity-60"
          value={id}
          disabled={editing}
          onChange={(e) => setId(e.target.value)}
          placeholder="t1"
        />
        {slugBad && (
          <p className="mt-1 text-[10px] text-[#f85149]">
            id must match [a-z0-9][a-z0-9-]*
          </p>
        )}
        {dup && <p className="mt-1 text-[10px] text-[#f85149]">id already in use</p>}

        <label className="mt-3 block text-[10px] font-semibold uppercase tracking-wide text-[#8b949e]">
          Feature
        </label>
        <select
          aria-label="feature"
          className="mt-1 w-full rounded border border-[#30363d] bg-[#0d1117] px-2 py-1 text-sm text-[#e6edf3]"
          value={feature}
          onChange={(e) => setFeature(e.target.value)}
        >
          {features.map((f) => (
            <option key={f.id} value={f.id}>
              {f.label}
            </option>
          ))}
        </select>

        <label className="mt-3 block text-[10px] font-semibold uppercase tracking-wide text-[#8b949e]">
          Spec (markdown)
        </label>
        <textarea
          aria-label="spec markdown"
          className="mt-1 h-40 w-full resize-y rounded border border-[#30363d] bg-[#0d1117] px-2 py-1 font-mono text-xs text-[#e6edf3]"
          value={specMd}
          onChange={(e) => setSpecMd(e.target.value)}
          placeholder="# Goal&#10;..."
        />

        <div className="mt-4 flex items-center gap-2">
          <button
            className="rounded bg-[#238636] px-3 py-1 text-xs font-semibold text-white disabled:cursor-not-allowed disabled:opacity-50"
            onClick={save}
            disabled={!canSave}
          >
            {editing ? "Save draft" : "Add draft"}
          </button>
          {editing && onDelete && (
            <button
              className="rounded border border-[#f85149] px-3 py-1 text-xs text-[#f85149] hover:bg-[#f85149]/10"
              onClick={onDelete}
            >
              Delete draft
            </button>
          )}
          <button
            className="ml-auto rounded border border-[#30363d] px-3 py-1 text-xs text-[#8b949e]"
            onClick={onClose}
          >
            Cancel
          </button>
        </div>
      </div>
    </div>
  );
}
