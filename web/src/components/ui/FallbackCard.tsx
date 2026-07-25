import { useEffect, useState } from "react";
import { getFallbackProposal, approveFallback, type FallbackProposal } from "../../lib/api";
import { Button } from "./Button";

export interface FallbackCardProps {
  project: string;
  /** Write capability (hosted read-only sources render the card without actions). */
  canWrite: boolean;
  /** Called after a successful approval so the caller can refresh run state. */
  onApproved?: () => void;
}

/**
 * FallbackCard surfaces the pending fallback proposal an env-class escalation
 * left: the driver could not run this seat at all (quota, auth, a missing
 * binary), and proposes running it under another role profile instead.
 *
 * Approving resumes the paused run adopting the proposal FOR THAT RUN ONLY —
 * the same semantics as `pactify orchestrate --resume --approve-fallback`.
 * Swapping which agent does the work is a human decision, so the card asks
 * rather than acting on its own; it carries the same "human decision" framing
 * as ReviewGate, the other card that pauses for a person.
 */
/**
 * writeJSON prefixes every failure with the endpoint it called
 * ("/api/projects/x/...: orchestrate is already running"). That path is for a
 * log, not for the person deciding whether to swap agents — keep only what the
 * server actually said.
 */
function serverSays(message: string): string {
  const m = /^\/api\/\S*:\s*(.+)$/s.exec(message);
  return m ? m[1] : message;
}

export function FallbackCard({ project, canWrite, onApproved }: FallbackCardProps) {
  const [proposal, setProposal] = useState<FallbackProposal | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [dismissed, setDismissed] = useState(false);

  useEffect(() => {
    let alive = true;
    getFallbackProposal(project)
      .then((p) => {
        if (alive) setProposal(p);
      })
      .catch(() => {
        // A proposal we cannot read is treated as "none pending": the card must
        // never invite an approval it is not sure about.
        if (alive) setProposal({ pending: false });
      });
    return () => {
      alive = false;
    };
  }, [project]);

  if (!proposal?.pending || dismissed) return null;

  async function approve() {
    setPending(true);
    setError("");
    try {
      await approveFallback(project);
      setProposal({ pending: false });
      onApproved?.();
    } catch (e) {
      // Keep the card and the proposal: the run is still paused and the
      // operator can retry once they have cleared the cause.
      setError(serverSays(e instanceof Error ? e.message : String(e)));
    } finally {
      setPending(false);
    }
  }

  return (
    <div
      data-testid="fallback-card"
      className="rounded-[11px] border p-3"
      style={{
        borderColor: "rgba(224,136,74,0.3)",
        background: "rgba(224,136,74,0.07)",
      }}
    >
      <div className="flex items-center gap-2">
        <span className="text-[13px] text-[var(--color-role-ops)]">⇄</span>
        <span className="text-[12.5px] font-semibold text-[var(--color-text-1)]">
          {proposal.seat} could not run{proposal.task ? ` · ${proposal.task}` : ""}
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

      <div className="mt-2 text-[11.5px] text-[var(--color-text-2)]">
        Run it as{" "}
        <span className="font-semibold text-[var(--color-text-1)]">{proposal.toRole}</span>
        {proposal.fromRole ? (
          <>
            {" "}
            instead of{" "}
            <span className="font-semibold text-[var(--color-text-1)]">{proposal.fromRole}</span>
          </>
        ) : null}
        {" "}— this run only.
      </div>

      {proposal.reason ? (
        <div className="mt-2 rounded-lg border border-[var(--color-border-subtle)] bg-[var(--bg-code)] p-2 font-mono text-[10.5px] text-[var(--color-text-3)]">
          {proposal.reason}
        </div>
      ) : null}

      {error ? (
        <div
          data-testid="fallback-error"
          className="mt-2 rounded-lg border p-2 text-[10.5px]"
          style={{
            borderColor: "color-mix(in srgb, var(--color-danger) 34%, transparent)",
            background: "color-mix(in srgb, var(--color-danger) 10%, transparent)",
            color: "var(--color-danger)",
          }}
        >
          {error}
        </div>
      ) : null}

      <div className="mt-3 flex flex-wrap items-center gap-2">
        {canWrite ? (
          <>
            <Button
              data-testid="fallback-approve"
              size="sm"
              loading={pending}
              onClick={approve}
            >
              Approve &amp; resume
            </Button>
            <Button
              data-testid="fallback-dismiss"
              variant="ghost"
              size="sm"
              disabled={pending}
              onClick={() => setDismissed(true)}
            >
              Dismiss
            </Button>
          </>
        ) : (
          <span data-testid="fallback-readonly" className="text-[10.5px] text-[var(--color-text-3)]">
            Read-only view — approve from the machine running this project.
          </span>
        )}
      </div>
    </div>
  );
}
