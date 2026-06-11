import { useState } from "react";
import type { Draft } from "../lib/canvas";
import { Modal } from "./ui/Modal";
import { Button } from "./ui/Button";
import { Input } from "./ui/Input";
import { Select } from "./ui/Select";

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
  defaultId,
  features,
  existingIds,
  onSave,
  onDelete,
  onClose,
}: {
  initial?: Draft;
  // Auto-generated id suggestion for a NEW draft (e.g. "t3"). Pre-fills the id
  // field as an editable default; ignored in edit mode (the draft keeps its id).
  // Slug validation is unchanged — the user may overwrite it freely.
  defaultId?: string;
  features: FeatureOption[];
  existingIds: string[]; // task ids already taken (committed + other drafts)
  onSave: (d: Draft) => void;
  onDelete?: () => void;
  onClose: () => void;
}) {
  const editing = !!initial;
  const [id, setId] = useState(initial?.id ?? defaultId ?? "");
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

  const label =
    "block text-[10px] font-semibold uppercase tracking-wide text-[var(--color-text-3)]";

  return (
    <Modal
      testId="task-editor"
      title={editing ? `Edit draft · ${initial!.id}` : "New task"}
      onClose={onClose}
      footer={
        <>
          <Button onClick={save} disabled={!canSave}>
            {editing ? "Save draft" : "Add draft"}
          </Button>
          {editing && onDelete && (
            <Button variant="danger" onClick={onDelete}>
              Delete draft
            </Button>
          )}
          <Button variant="ghost" className="ml-auto" onClick={onClose}>
            Cancel
          </Button>
        </>
      }
    >
      <label className={label}>Task id</label>
      <Input
        aria-label="task id"
        className="mt-1 font-mono"
        value={id}
        disabled={editing}
        onChange={(e) => setId(e.target.value)}
        placeholder="t1"
      />
      {slugBad && (
        <p className="mt-1 text-[10px] text-[var(--color-danger)]">
          id must match [a-z0-9][a-z0-9-]*
        </p>
      )}
      {dup && <p className="mt-1 text-[10px] text-[var(--color-danger)]">id already in use</p>}

      <label className={`mt-3 ${label}`}>Feature</label>
      <Select
        aria-label="feature"
        className="mt-1"
        value={feature}
        onChange={(e) => setFeature(e.target.value)}
      >
        {features.map((f) => (
          <option key={f.id} value={f.id}>
            {f.label}
          </option>
        ))}
      </Select>

      <label className={`mt-3 ${label}`}>Spec (markdown)</label>
      <textarea
        aria-label="spec markdown"
        className="mt-1 h-40 w-full resize-y rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-2.5 py-1.5 font-mono text-xs text-[var(--color-text-1)] outline-none transition-colors duration-[var(--motion-micro)] focus:border-[var(--color-border-strong)] placeholder:text-[var(--color-text-3)]"
        value={specMd}
        onChange={(e) => setSpecMd(e.target.value)}
        placeholder="# Goal&#10;..."
      />
    </Modal>
  );
}
