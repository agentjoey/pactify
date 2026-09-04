import { useCallback, useEffect, useId, useRef, useState } from "react";
import { ProposalGoneError, type FallbackProposal } from "../../lib/api";
import { useDataSource } from "../../lib/datasource";
import { useVisiblePoll } from "../../lib/useVisiblePoll";
import { Button } from "./Button";

export interface FallbackCardsProps {
  project: string;
  /** Write capability (hosted read-only sources render the cards without actions). */
  canWrite: boolean;
  /** Called after a successful approval so the caller can refresh run state. */
  onApproved?: () => void;
}

/**
 * Identity of a proposal, so retiring one does not hide the next. Scope leads
 * because it is the proposal's true key on the server (one file per scope): two
 * concurrent features can escalate the same seat, and those are two separate
 * human decisions that must not share a dismissal.
 */
function keyOf(p: FallbackProposal): string {
  return `${p.scope}|${p.seat}|${p.task}|${p.toRole}`;
}

/**
 * FallbackCards renders the project's pending fallback proposals — ONE CARD PER
 * PROPOSAL, never one card listing several. One card = one person's decision:
 * approving a swap for feature A is a separate act from approving one for
 * feature B, and a --max-concurrency > 1 run pauses each feature independently,
 * so several can be pending at once.
 *
 * The escalation happens WHILE the operator is watching a run, so this polls: a
 * mount-once fetch would mean the one scenario the cards exist for never renders
 * them.
 */
export function FallbackCards({ project, canWrite, onApproved }: FallbackCardsProps) {
  const src = useDataSource();
  const [proposals, setProposals] = useState<FallbackProposal[]>([]);
  // Proposals this operator has finished with (dismissed, or approved from
  // here). Held by identity, not by list position, and kept here rather than
  // inside a card so a retired card leaves NO empty slot in the stack.
  const [retired, setRetired] = useState<ReadonlySet<string>>(() => new Set());

  // Every proposal here describes ONE project. Switching projects without
  // clearing them would show project A's seat and role swap while the approve
  // button posts to project B.
  const shown = useRef(project);
  useEffect(() => {
    shown.current = project;
    setProposals([]);
    setRetired(new Set());
  }, [project]);

  const read = src.getFallbackProposals;
  const load = useCallback(() => {
    if (!read) return;
    const forProject = project;
    read(project)
      .then((list) => {
        if (shown.current !== forProject) return;
        setProposals(list);
        // A retirement only has to outlive the proposal it hides. Dropping keys
        // the server no longer lists keeps "dismissed" from silencing a future
        // escalation that happens to be identical.
        setRetired((prev) => {
          if (prev.size === 0) return prev;
          const live = new Set(list.map(keyOf));
          const next = new Set<string>();
          for (const k of prev) if (live.has(k)) next.add(k);
          return next.size === prev.size ? prev : next;
        });
      })
      .catch(() => {
        // Proposals we cannot read are treated as "none pending": a card must
        // never invite an approval it is not sure about.
        if (shown.current === forProject) setProposals([]);
      });
  }, [read, project]);

  useVisiblePoll(load, 10_000);

  const retire = useCallback((key: string) => {
    setRetired((prev) => new Set(prev).add(key));
  }, []);

  const visible = proposals.filter((p) => !retired.has(keyOf(p)));
  if (visible.length === 0) return null;

  return (
    // Same gap as the Dashboard column that holds this group, so N proposals
    // read as N peers in one stack rather than a nested sub-list.
    <div data-testid="fallback-cards" className="flex flex-col gap-[14px]">
      {visible.map((p) => {
        const key = keyOf(p);
        return (
          // Keyed by proposal identity: a card owns its own in-flight and error
          // state, and that state must travel with the proposal, not with a
          // list slot.
          <FallbackCard
            key={key}
            project={project}
            proposal={p}
            canWrite={canWrite}
            onRetire={() => retire(key)}
            onApproved={onApproved}
          />
        );
      })}
    </div>
  );
}

export interface FallbackCardProps {
  project: string;
  /** The one proposal this card decides. */
  proposal: FallbackProposal;
  canWrite: boolean;
  /** This decision is finished: drop the card from the stack. */
  onRetire: () => void;
  onApproved?: () => void;
}

/**
 * FallbackCard surfaces ONE pending fallback proposal an env-class escalation
 * left: the driver could not run this seat at all (quota, auth, a missing
 * binary), and proposes running it under another role profile instead.
 *
 * Approving resumes the paused run adopting the proposal FOR THAT RUN ONLY —
 * the same semantics as `pactify orchestrate --resume --approve-fallback <task>`.
 * Swapping which agent does the work is a human decision, so the card asks
 * rather than acting on its own; it carries the same "human decision" framing
 * as ReviewGate, the other card that pauses for a person.
 */
export function FallbackCard({
  project,
  proposal,
  canWrite,
  onRetire,
  onApproved,
}: FallbackCardProps) {
  const src = useDataSource();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // Unique per instance: N stacked cards must not all label themselves with the
  // same element id, which would point every card's heading at the first one.
  const titleId = useId();

  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  // Approving needs a source that can reach the machine holding the proposal,
  // AND a task id: the server keys approval by task and 404s a body naming no
  // pending task, so an approve button without one could only ever fail while
  // looking to this card like someone else had already handled it.
  const approver = src.approveFallback;
  const task = proposal.task ?? "";
  const actionable = canWrite && !!approver && !!task;

  async function approve() {
    if (!approver || !task) return;
    setBusy(true);
    setError("");
    try {
      await approver(project, task);
      if (!mounted.current) return;
      onRetire();
      onApproved?.();
    } catch (e) {
      if (!mounted.current) return;
      if (e instanceof ProposalGoneError) {
        // Already handled elsewhere — retire the card instead of offering a
        // retry that cannot succeed.
        onRetire();
        onApproved?.();
        return;
      }
      // Keep the card and the proposal: the run is still paused and the
      // operator can retry once they have cleared the cause.
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      if (mounted.current) setBusy(false);
    }
  }

  return (
    <section
      data-testid="fallback-card"
      data-scope={proposal.scope}
      aria-labelledby={titleId}
      className="rounded-[11px] border p-3"
      style={{
        borderColor: "rgba(224,136,74,0.3)",
        background: "rgba(224,136,74,0.07)",
      }}
    >
      {/* The glyph and the pill are fixed; the title takes what is left and
          wraps inside it. Seat and task ids are long and the column narrows as
          the stack grows, so the title must be the thing that gives. */}
      <div className="flex items-start gap-2">
        <span
          aria-hidden="true"
          className="shrink-0 leading-[1.35] text-[13px] text-[var(--color-role-ops)]"
        >
          ⇄
        </span>
        <h3
          id={titleId}
          className="min-w-0 flex-1 break-words leading-[1.35] text-[12.5px] font-semibold text-[var(--color-text-1)]"
        >
          {proposal.seat} could not run{proposal.task ? ` · ${proposal.task}` : ""}
        </h3>
        <span
          className="shrink-0 rounded-full px-2 py-0.5 text-[9px] leading-[1.6] text-[var(--color-role-ops)]"
          style={{
            background: "rgba(224,136,74,0.12)",
            border: "1px solid rgba(224,136,74,0.34)",
          }}
        >
          human decision
        </span>
      </div>

      {/* Which run partition this decision belongs to — the thing that tells two
          stacked cards apart when they share a seat. Shown only when it names
          something: "all" is the unfiltered serial run, where there is exactly
          one partition and the label would distinguish nothing. */}
      {proposal.scope && proposal.scope !== "all" ? (
        <div className="mt-1.5 text-[10.5px] text-[var(--color-text-2)]">
          feature <span className="font-mono text-[var(--color-text-1)]">{proposal.scope}</span>
        </div>
      ) : null}

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
            <Button data-testid="fallback-approve" size="sm" loading={busy} onClick={approve}>
              Approve &amp; resume
            </Button>
            <Button
              data-testid="fallback-dismiss"
              variant="ghost"
              size="sm"
              disabled={busy}
              title="Hides this card. The run stays paused — approve it later here or with `pactify orchestrate --resume --approve-fallback`."
              onClick={onRetire}
            >
              Dismiss
            </Button>
          </>
        ) : (
          <span data-testid="fallback-readonly" className="text-[10.5px] text-[var(--color-text-2)]">
            {canWrite && !!approver && !task
              ? "This proposal names no task — approve it with `pactify orchestrate --resume --approve-fallback <task>`."
              : "Read-only view — approve from the machine running this project."}
          </span>
        )}
      </div>
    </section>
  );
}
