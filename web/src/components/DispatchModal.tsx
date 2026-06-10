import { useState } from "react";
import type { Draft } from "../lib/canvas";
import { postTask, postVerb } from "../lib/api";

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
  if (owner === reviewer) {
    throw new Error("owner and reviewer must differ");
  }
  return {
    task: draft.id,
    feature: draft.feature,
    branch,
    owner,
    reviewer,
    spec: draft.specMd,
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
  owner,
  roster,
  branch,
  onDispatched,
  onClose,
}: {
  project: string;
  draft: Draft;
  owner: string;
  roster: string[];
  branch: string;
  onDispatched: () => void;
  onClose: () => void;
}) {
  const reviewers = roster.filter((r) => r !== owner);
  const [reviewer, setReviewer] = useState(reviewers[0] ?? "");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  const canConfirm = !!reviewer && reviewer !== owner && !busy;

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
      setErr(e instanceof Error ? e.message : String(e));
      setBusy(false);
    }
  };

  return (
    <div
      data-testid="dispatch-modal"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60"
      onClick={onClose}
    >
      <div
        className="w-[520px] max-w-[92vw] rounded-lg border border-[#30363d] bg-[#161b22] p-4 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-sm font-semibold text-[#e6edf3]">Dispatch · {draft.id}</h2>
          <button className="text-xs text-[#8b949e] hover:text-[#e6edf3]" onClick={onClose} aria-label="close">
            ✕
          </button>
        </div>

        <div className="grid grid-cols-2 gap-3 text-xs">
          <div>
            <div className="text-[10px] font-semibold uppercase tracking-wide text-[#8b949e]">Owner</div>
            <div className="mt-1 rounded border border-[#30363d] bg-[#0d1117] px-2 py-1 text-[#e6edf3]">{owner}</div>
          </div>
          <div>
            <label className="text-[10px] font-semibold uppercase tracking-wide text-[#8b949e]">Reviewer</label>
            <select
              aria-label="reviewer"
              className="mt-1 w-full rounded border border-[#30363d] bg-[#0d1117] px-2 py-1 text-[#e6edf3]"
              value={reviewer}
              onChange={(e) => setReviewer(e.target.value)}
            >
              {reviewers.length === 0 && <option value="">(no eligible reviewer)</option>}
              {reviewers.map((r) => (
                <option key={r} value={r}>
                  {r}
                </option>
              ))}
            </select>
          </div>
          <div>
            <div className="text-[10px] font-semibold uppercase tracking-wide text-[#8b949e]">Branch</div>
            <div className="mt-1 rounded border border-[#30363d] bg-[#0d1117] px-2 py-1 font-mono text-[#e6edf3]">
              {branch || "—"}
            </div>
          </div>
          <div>
            <div className="text-[10px] font-semibold uppercase tracking-wide text-[#8b949e]">Deps</div>
            <div className="mt-1 rounded border border-[#30363d] bg-[#0d1117] px-2 py-1 font-mono text-[#8b949e]">
              {draft.deps.length ? draft.deps.join(", ") : "(none)"}
            </div>
          </div>
        </div>

        <div className="mt-3">
          <div className="text-[10px] font-semibold uppercase tracking-wide text-[#8b949e]">Spec preview</div>
          <pre className="mt-1 max-h-40 overflow-auto rounded border border-[#30363d] bg-[#0d1117] p-2 text-[10px] text-[#e6edf3]">
            {draft.specMd || "(empty)"}
          </pre>
        </div>

        {err && <p className="mt-3 whitespace-pre-wrap text-xs text-[#f85149]">{err}</p>}

        <div className="mt-4 flex items-center gap-2">
          <button
            className="rounded bg-[#238636] px-3 py-1 text-xs font-semibold text-white disabled:cursor-not-allowed disabled:opacity-50"
            onClick={confirm}
            disabled={!canConfirm}
          >
            {busy ? "Dispatching…" : "Confirm dispatch"}
          </button>
          <button className="ml-auto rounded border border-[#30363d] px-3 py-1 text-xs text-[#8b949e]" onClick={onClose}>
            Cancel
          </button>
        </div>
      </div>
    </div>
  );
}
