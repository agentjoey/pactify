import { useState } from "react";
import type { Task } from "../../lib/types";
import { useDataSource } from "../../lib/datasource";

export interface ReviewGateProps {
  project: string;
  task: Task;
  featureId: string;
  author?: boolean;
  onChanged?: () => void;
  onSeeDiff?: () => void;
  onTakeOver?: () => void;
}

export function ReviewGate({
  project,
  task,
  featureId,
  author,
  onChanged,
  onSeeDiff,
  onTakeOver,
}: ReviewGateProps) {
  const src = useDataSource();
  const canWrite = src.capabilities.canWrite && author;
  const [pending, setPending] = useState<"accept" | "changes" | null>(null);
  const [showReason, setShowReason] = useState(false);
  const [reason, setReason] = useState("");

  async function verb(v: "accept" | "changes") {
    if (!src.verb) return;
    setPending(v);
    try {
      await src.verb(
        project,
        v,
        v === "changes" ? { task: task.id, reason: reason.trim() || undefined } : { task: task.id },
      );
      setReason("");
      setShowReason(false);
      onChanged?.();
    } catch {
      // errors surface via parent refresh / toast; keep the gate open
    } finally {
      setPending(null);
    }
  }

  return (
    <div
      data-testid="review-gate"
      className="rounded-[11px] border p-3"
      style={{
        borderColor: "rgba(224,136,74,0.3)",
        background: "rgba(224,136,74,0.07)",
      }}
    >
      <div className="flex items-center gap-2">
        <span className="text-[13px] text-[var(--color-role-ops)]">⊘</span>
        <span className="text-[12.5px] font-semibold text-[var(--color-text-1)]">
          {task.id} · {featureId}
        </span>
        <span
          className="ml-auto rounded-full px-2 py-0.5 text-[9px] text-[var(--color-role-ops)]"
          style={{
            background: "rgba(224,136,74,0.12)",
            border: "1px solid rgba(224,136,74,0.34)",
          }}
        >
          human decision
        </span>
      </div>

      {/* Evidence is the reviewer's primary read: --color-text-3 measures 3.39:1
          on --bg-code, under the 4.5:1 body-text line. --color-text-2 is 6.80:1.
          Same call FallbackCard already made for its own evidence well. */}
      <div className="mt-2 rounded-lg border border-[var(--color-border-subtle)] bg-[var(--bg-code)] p-2 font-mono text-[10.5px] text-[var(--color-text-2)]">
        {task.evidence || "Awaiting review evidence"}
      </div>

      <div className="mt-3 flex flex-wrap gap-2">
        {showReason ? (
          <form
            className="flex w-full flex-col gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              if (reason.trim()) verb("changes");
            }}
          >
            <input
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder="Reason for changes…"
              autoFocus
              className="w-full rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] px-2 py-1 text-[11px] text-[var(--color-text-1)] outline-none placeholder:text-[var(--color-text-3)]"
            />
            <div className="flex justify-end gap-2">
              <button
                type="button"
                onClick={() => {
                  setShowReason(false);
                  setReason("");
                }}
                className="rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-input)] px-3 py-1 text-[11px] font-semibold text-[var(--color-text-2)] hover:text-[var(--color-text-1)]"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={!reason.trim() || pending === "changes" || !canWrite}
                className="rounded-md px-3 py-1 text-[11px] font-semibold disabled:opacity-50"
                style={{
                  background: "rgba(224,136,74,0.12)",
                  color: "var(--color-role-ops)",
                  border: "1px solid rgba(224,136,74,0.4)",
                }}
              >
                Send changes
              </button>
            </div>
          </form>
        ) : (
          <>
            <button
              type="button"
              data-testid="review-gate-diff"
              onClick={onSeeDiff}
              className="rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-input)] px-3 py-1 text-[11.5px] font-semibold text-[var(--color-text-2)] hover:text-[var(--color-text-1)]"
            >
              See diff
            </button>
            <button
              type="button"
              data-testid="review-gate-accept"
              disabled={!canWrite || pending === "accept"}
              title={canWrite ? undefined : "Remote control needs U3"}
              onClick={() => verb("accept")}
              className="rounded-md px-3 py-1 text-[11.5px] font-semibold text-[var(--color-on-accent)] disabled:opacity-50"
              style={{ background: "var(--color-success)" }}
            >
              Approve merge
            </button>
            <button
              type="button"
              data-testid="review-gate-changes"
              disabled={!canWrite || pending === "changes"}
              title={canWrite ? undefined : "Remote control needs U3"}
              onClick={() => setShowReason(true)}
              className="rounded-md px-3 py-1 text-[11.5px] font-semibold disabled:opacity-50"
              style={{
                background: "rgba(224,136,74,0.12)",
                color: "var(--color-role-ops)",
                border: "1px solid rgba(224,136,74,0.4)",
              }}
            >
              Reject → rework
            </button>
            <button
              type="button"
              data-testid="review-gate-takeover"
              onClick={onTakeOver}
              className="rounded-md border border-[var(--color-border-subtle)] bg-transparent px-3 py-1 text-[11.5px] font-semibold text-[var(--color-text-3)] hover:text-[var(--color-text-1)]"
            >
              Take over
            </button>
          </>
        )}
      </div>
    </div>
  );
}
