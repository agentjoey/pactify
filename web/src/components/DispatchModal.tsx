import { useState } from "react";
import type { Draft } from "../lib/canvas";
import { postTask, postVerb } from "../lib/api";
import { humanizeError } from "../lib/protocolErrors";
import { Modal } from "./ui/Modal";
import { Button } from "./ui/Button";
import { Select } from "./ui/Select";

// dispatchPayload assembles the assign verb body from a draft + chosen reviewer.
// Pure + exported so the wire shape (owner≠reviewer guard, deps passthrough) is
// unit-testable without touching the network. Throws on the owner==reviewer
// invariant — the modal disables Confirm before it can reach this, but the
// guard keeps the contract explicit for callers and tests.
export type AssignPayload = {
  task: string;
  feature: string;
  branch: string;
  owner: string;
  reviewer: string;
  spec: string;
  deps: string[];
};

export function dispatchPayload(
  draft: Draft,
  owner: string,
  reviewer: string,
  branch: string,
): AssignPayload {
  if (!owner || !reviewer) {
    throw new Error("owner and reviewer are required");
  }
  if (owner === reviewer) {
    throw new Error("owner and reviewer must differ");
  }
  return {
    task: draft.id,
    feature: draft.feature,
    branch,
    owner,
    reviewer,
    // spec is a PATH reference (the markdown body was already written via postTask);
    // inlining the body would corrupt STATE.yml's single-line spec field.
    spec: `.pact/tasks/${draft.id}.md`,
    deps: draft.deps, // deps pass through verbatim; server fixes them at assign
  };
}

// DispatchModal confirms turning a Draft into a real assigned task. Owner is the
// seat the draft was dropped onto (prefilled, read-only). Reviewer is chosen
// from the roster minus the owner (server enforces owner≠reviewer; we mirror it
// client-side so the picker can never select an invalid pair). On confirm it
// POSTs the task then runs the assign verb; success removes the draft locally
// (SSE brings the committed task), failure keeps the modal open with the
// server's message verbatim in red.
export function DispatchModal({
  project,
  draft,
  owner: initialOwner,
  roster,
  branch,
  onDispatched,
  onClose,
}: {
  project: string;
  draft: Draft;
  owner?: string; // set by the drop gesture; undefined when opened via the dispatch button
  roster: string[];
  branch: string;
  onDispatched: () => void;
  onClose: () => void;
}) {
  const [owner, setOwner] = useState(initialOwner ?? "");
  const reviewers = roster.filter((r) => r !== owner);
  const [reviewer, setReviewer] = useState(reviewers[0] ?? "");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  const canConfirm = !!owner && !!reviewer && reviewer !== owner && !busy;

  const confirm = async () => {
    if (!canConfirm) return;
    setBusy(true);
    setErr("");
    try {
      const body = dispatchPayload(draft, owner, reviewer, branch);
      // spec_md carries the markdown; the server writes it to .pact/tasks/<id>.md
      // and the assign verb references it. Two steps, one mutex server-side.
      await postTask(project, { id: draft.id, spec_md: draft.specMd });
      await postVerb(project, "assign", body as unknown as Record<string, unknown>);
      onDispatched();
    } catch (e) {
      setErr(humanizeError(e instanceof Error ? e.message : String(e)));
      setBusy(false);
    }
  };

  const label = "text-[10px] font-semibold uppercase tracking-wide text-[var(--color-text-3)]";
  const readonly =
    "mt-1 rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-2 py-1 text-[var(--color-text-1)]";

  return (
    <Modal
      testId="dispatch-modal"
      title={`Dispatch · ${draft.id}`}
      onClose={onClose}
      footer={
        <>
          <Button onClick={confirm} disabled={!canConfirm}>
            {busy ? "Dispatching…" : "Confirm dispatch"}
          </Button>
          <Button variant="ghost" className="ml-auto" onClick={onClose}>
            Cancel
          </Button>
        </>
      }
    >
      <div className="grid grid-cols-2 gap-3 text-xs">
        <div>
          <div className={label}>Owner</div>
          {initialOwner ? (
            <div className={readonly}>{owner}</div>
          ) : (
            <Select
              aria-label="owner"
              className="mt-1"
              value={owner}
              onChange={(e) => {
                setOwner(e.target.value);
                if (reviewer === e.target.value) setReviewer("");
              }}
            >
              <option value="">(choose owner)</option>
              {roster.map((r) => (
                <option key={r} value={r}>{r}</option>
              ))}
            </Select>
          )}
        </div>
        <div>
          <label className={label}>Reviewer</label>
          <Select
            aria-label="reviewer"
            className="mt-1"
            value={reviewer}
            onChange={(e) => setReviewer(e.target.value)}
          >
            {reviewers.length === 0 && <option value="">(no eligible reviewer)</option>}
            {reviewers.map((r) => (
              <option key={r} value={r}>
                {r}
              </option>
            ))}
          </Select>
        </div>
        <div>
          <div className={label}>Branch</div>
          <div className={`${readonly} font-mono`}>{branch || "—"}</div>
        </div>
        <div>
          <div className={label}>Deps</div>
          <div className={`${readonly} font-mono text-[var(--color-text-2)]`}>
            {draft.deps.length ? draft.deps.join(", ") : "(none)"}
          </div>
        </div>
      </div>

      <div className="mt-3">
        <div className={label}>Spec preview</div>
        <pre className="mt-1 max-h-40 overflow-auto rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] p-2 text-[10px] text-[var(--color-text-1)]">
          {draft.specMd || "(empty)"}
        </pre>
      </div>

      {err && (
        <p className="mt-3 whitespace-pre-wrap text-xs text-[var(--color-danger)]">{err}</p>
      )}
    </Modal>
  );
}
