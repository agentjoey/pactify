import { useEffect, useRef, useState } from "react";
import type { Seat, PlanReviewResponse } from "../../lib/types";
import { useDataSource } from "../../lib/datasource";
import { slugify } from "../../lib/slug";

type Phase = "compose" | "generating" | "review" | "dispatching" | "done" | "error";

export function DispatchPanel({
  project,
  roster,
  open,
  onClose,
  onGoLive,
  initialGoal,
}: {
  project: string;
  roster: Seat[];
  open: boolean;
  onClose: () => void;
  onGoLive: () => void;
  // Seeds the goal field when the panel opens (e.g. from the canvas NL dock).
  initialGoal?: string;
}) {
  const src = useDataSource();
  // Dispatch generates + runs an orchestrate plan → needs the drive capability,
  // not just write (the relay source can't orchestrate remotely yet).
  const canWrite = src.capabilities.canOrchestrate;
  const [phase, setPhase] = useState<Phase>("compose");
  const [goal, setGoal] = useState("");
  const [feature, setFeature] = useState("");
  const [featureTouched, setFeatureTouched] = useState(false);
  const [review, setReview] = useState<PlanReviewResponse | null>(null);
  const [error, setError] = useState("");
  const [assigned, setAssigned] = useState(0);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // Keep the feature slug in sync with the goal until the user edits it by hand.
  useEffect(() => {
    if (!featureTouched) setFeature(slugify(goal));
  }, [goal, featureTouched]);

  useEffect(() => () => { if (pollRef.current) clearInterval(pollRef.current); }, []);

  // Seed the goal from the canvas NL dock when the panel opens with one.
  useEffect(() => {
    if (open && initialGoal) {
      setGoal(initialGoal);
      setPhase("compose");
    }
  }, [open, initialGoal]);

  if (!open) return null;

  const canGenerate = goal.trim() !== "" && /^[a-z0-9][a-z0-9-]*$/.test(feature) && roster.length > 0 && canWrite;

  async function startGenerate() {
    if (!canWrite) return;
    setError("");
    setPhase("generating");
    try {
      await src.generatePlan!(project, { goal, feature });
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setPhase("error");
      return;
    }
    pollRef.current = setInterval(async () => {
      try {
        const st = await src.getPlanGenStatus!(project);
        if (st.state === "done") {
          if (pollRef.current) clearInterval(pollRef.current);
          const rv = await src.getPlanReview!(project, feature);
          setReview(rv);
          setPhase("review");
        } else if (st.state === "error") {
          if (pollRef.current) clearInterval(pollRef.current);
          setError(st.error ?? "generation failed");
          setPhase("error");
        }
      } catch { /* transient; keep polling */ }
    }, 200);
  }

  async function dispatch() {
    if (!canWrite) return;
    setPhase("dispatching");
    try {
      const r = await src.applyPlan!(project, feature);
      setAssigned(r.assigned);
      await src.runOrchestrate!(project, { feature });
      setPhase("done");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setPhase("error");
    }
  }

  function regenerate() {
    setReview(null);
    setPhase("compose");
  }

  return (
    <div
      data-testid="dispatch-panel"
      className="fixed right-0 top-0 z-50 flex h-full w-[360px] flex-col border-l border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] shadow-[var(--shadow-overlay)]"
    >
      <div className="flex items-center justify-between border-b border-[var(--color-border-subtle)] px-4 py-3">
        <span className="text-[13px] font-semibold">◉ Dispatch</span>
        <button type="button" aria-label="close" onClick={onClose} className="text-[var(--color-text-3)] hover:text-[var(--color-text-1)]">✕</button>
      </div>

      <div className="flex-1 overflow-auto p-4">
        {(phase === "compose" || phase === "generating") && (
          <div className="flex flex-col gap-3">
            <label className="text-[11px] text-[var(--color-text-3)]">Goal
              <textarea
                data-testid="dispatch-goal"
                value={goal}
                onChange={(e) => setGoal(e.target.value)}
                rows={3}
                placeholder="e.g. add 2fa login"
                className="mt-1 w-full rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] p-2 text-[12px] text-[var(--color-text-1)]"
              />
            </label>
            <label className="text-[11px] text-[var(--color-text-3)]">Feature id
              <input
                data-testid="dispatch-feature"
                value={feature}
                onChange={(e) => { setFeatureTouched(true); setFeature(e.target.value); }}
                className="mt-1 w-full rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] p-2 text-[12px] text-[var(--color-text-1)]"
              />
            </label>
            {roster.length === 0 && <p className="text-[11px] text-[var(--color-warn)]">Add seats in Settings before dispatching.</p>}
            {phase === "generating" ? (
              <p data-testid="dispatch-generating" className="text-[12px] text-[var(--color-text-2)]">Planner is decomposing your goal…</p>
            ) : (
              <button
                type="button"
                data-testid="dispatch-generate"
                disabled={!canGenerate}
                title={canWrite ? undefined : "Remote control needs U3"}
                onClick={startGenerate}
                className="rounded-md bg-[var(--color-text-1)] px-3 py-1.5 text-[12px] font-medium text-[var(--color-bg-surface)] disabled:opacity-40"
              >Generate</button>
            )}
          </div>
        )}

        {phase === "review" && review && (
          <div data-testid="dispatch-review" className="flex flex-col gap-3">
            <div className="text-[11px] text-[var(--color-text-3)]">Feature: {review.feature}</div>
            <ul className="flex flex-col gap-2">
              {(review.tasks ?? []).map((t) => (
                <li key={t.id} className="rounded-md border border-[var(--color-border-subtle)] p-2 text-[11px]">
                  <div className="font-medium text-[var(--color-text-1)]">{t.id}</div>
                  <div className="text-[var(--color-text-3)]">{t.owner} → {t.reviewer}{t.deps?.length ? ` · deps: ${t.deps.join(",")}` : ""}</div>
                </li>
              ))}
            </ul>
            {review.valid === false && <p className="text-[11px] text-[var(--color-danger)]">{review.error}</p>}
            <div className="flex gap-2">
              <button type="button" data-testid="dispatch-regen" onClick={regenerate} className="rounded-md border border-[var(--color-border-subtle)] px-3 py-1.5 text-[12px]">↻ Regenerate</button>
              <button
                type="button"
                data-testid="dispatch-confirm"
                disabled={review.valid === false || !canWrite}
                title={canWrite ? undefined : "Remote control needs U3"}
                onClick={dispatch}
                className="rounded-md bg-[var(--color-success)] px-3 py-1.5 text-[12px] font-medium text-white disabled:opacity-40"
              >✓ Dispatch</button>
            </div>
          </div>
        )}

        {phase === "dispatching" && <p className="text-[12px] text-[var(--color-text-2)]">Dispatching…</p>}

        {phase === "done" && (
          <div data-testid="dispatch-done" className="flex flex-col gap-3">
            <p className="text-[12px] text-[var(--color-text-1)]">Dispatched — {assigned} task(s) assigned, orchestrating.</p>
            <button type="button" data-testid="dispatch-golive" onClick={onGoLive} className="rounded-md border border-[var(--color-border-subtle)] px-3 py-1.5 text-[12px]">Go to Live →</button>
          </div>
        )}

        {phase === "error" && (
          <div data-testid="dispatch-error" className="flex flex-col gap-3">
            <p className="text-[12px] text-[var(--color-danger)]">{error}</p>
            <button type="button" onClick={() => setPhase("compose")} className="rounded-md border border-[var(--color-border-subtle)] px-3 py-1.5 text-[12px]">Back</button>
          </div>
        )}
      </div>
    </div>
  );
}
