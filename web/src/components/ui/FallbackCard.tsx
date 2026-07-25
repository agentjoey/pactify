import { useCallback, useEffect, useRef, useState } from "react";
import { ProposalGoneError, type FallbackProposal } from "../../lib/api";
import { useDataSource } from "../../lib/datasource";
import { useVisiblePoll } from "../../lib/useVisiblePoll";
import { Button } from "./Button";

export interface FallbackCardProps {
  project: string;
  /** Write capability (hosted read-only sources render the card without actions). */
  canWrite: boolean;
  /** Called after a successful approval so the caller can refresh run state. */
  onApproved?: () => void;
}

/** Identity of a proposal, so dismissing one does not hide the next. */
function keyOf(p: FallbackProposal): string {
  return `${p.seat}|${p.task}|${p.toRole}`;
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
 *
 * The escalation happens WHILE the operator is watching a run, so the card
 * polls: a mount-once fetch would mean the one scenario it exists for never
 * renders it.
 */
export function FallbackCard({ project, canWrite, onApproved }: FallbackCardProps) {
  const src = useDataSource();
  const [proposal, setProposal] = useState<FallbackProposal | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [dismissed, setDismissed] = useState("");

  // Every piece of state describes ONE project's proposal. Switching projects
  // without clearing it would show project A's seat and role swap while the
  // approve button posts to project B.
  const shown = useRef(project);
  useEffect(() => {
    shown.current = project;
    setProposal(null);
    setError("");
    setDismissed("");
  }, [project]);

  const read = src.getFallbackProposal;
  const load = useCallback(() => {
    if (!read) return;
    const forProject = project;
    read(project)
      .then((p) => {
        if (shown.current === forProject) setProposal(p);
      })
      .catch(() => {
        // A proposal we cannot read is treated as "none pending": the card must
        // never invite an approval it is not sure about.
        if (shown.current === forProject) setProposal({ pending: false });
      });
  }, [read, project]);

  useVisiblePoll(load, 10_000);

  if (!proposal?.pending || dismissed === keyOf(proposal)) return null;

  // Approving needs a source that can reach the machine holding the proposal.
  const approver = src.approveFallback;
  const actionable = canWrite && !!approver;

  async function approve() {
    if (!approver) return;
    const forProject = project;
    setPending(true);
    setError("");
    try {
      await approver(forProject);
      if (shown.current !== forProject) return;
      setProposal({ pending: false });
      onApproved?.();
    } catch (e) {
      if (shown.current !== forProject) return;
      if (e instanceof ProposalGoneError) {
        // Already handled elsewhere — retire the card instead of offering a
        // retry that cannot succeed.
        setProposal({ pending: false });
        onApproved?.();
        return;
      }
      // Keep the card and the proposal: the run is still paused and the
      // operator can retry once they have cleared the cause.
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      if (shown.current === forProject) setPending(false);
    }
  }

  return (
    <section
      data-testid="fallback-card"
      aria-labelledby="fallback-card-title"
      className="rounded-[11px] border p-3"
      style={{
        borderColor: "rgba(224,136,74,0.3)",
        background: "rgba(224,136,74,0.07)",
      }}
    >
      <div className="flex items-center gap-2">
        <span aria-hidden="true" className="text-[13px] text-[var(--color-role-ops)]">
          ⇄
        </span>
        <h3
          id="fallback-card-title"
          className="text-[12.5px] font-semibold text-[var(--color-text-1)]"
        >
          {proposal.seat} could not run{proposal.task ? ` · ${proposal.task}` : ""}
        </h3>
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
        // The driver's failure text is the operator's only diagnostic: it must
        // be readable (--color-text-2, not the 3.3:1 --color-text-3) and keep
        // its line breaks — LastFail is routinely multi-line.
        <div className="mt-2 whitespace-pre-wrap break-words rounded-lg border border-[var(--color-border-subtle)] bg-[var(--bg-code)] p-2 font-mono text-[10.5px] text-[var(--color-text-2)]">
          {proposal.reason}
        </div>
      ) : null}

      {error ? (
        <div
          data-testid="fallback-error"
          role="alert"
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
        {actionable ? (
          <>
            <Button data-testid="fallback-approve" size="sm" loading={pending} onClick={approve}>
              Approve &amp; resume
            </Button>
            <Button
              data-testid="fallback-dismiss"
              variant="ghost"
              size="sm"
              disabled={pending}
              title="Hides this card. The run stays paused — approve it later here or with `pactify orchestrate --resume --approve-fallback`."
              onClick={() => setDismissed(keyOf(proposal))}
            >
              Dismiss
            </Button>
          </>
        ) : (
          <span data-testid="fallback-readonly" className="text-[10.5px] text-[var(--color-text-2)]">
            Read-only view — approve from the machine running this project.
          </span>
        )}
      </div>
    </section>
  );
}
