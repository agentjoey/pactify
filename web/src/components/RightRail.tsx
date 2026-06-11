import { useEffect, useRef, useState } from "react";
import type { State, PactEvent } from "../lib/types";
import { findTask, canMergeFeature } from "../lib/derive";
import { postVerb } from "../lib/api";
import { relTime } from "../lib/reltime";
import { casteForRoles } from "../lib/ants";
import { Ant } from "./ui/ants/Ant";
import { Badge } from "./ui/Badge";
import { Button } from "./ui/Button";

// RightRail v2 — task detail panel (plan T11, mockup board4 §④).
//
// When `selected` is a task id the panel slides in from the right OVER the
// kanban/canvas content; a scrim dims the content behind and closes the panel
// on click. Esc closes too — the panel installs its own keydown listener while
// open and stops propagation so it closes BEFORE the canvas Esc chains. Closing
// deselects (onSelect("")). When nothing is selected the component renders null,
// so the old always-visible rail disappears and the board/canvas take full
// width.
//
// Content per board4 §④: header (id·feature mono, title, status Badge,
// owner→reviewer ant chips, time-in-flight) · Spec · Evidence · Timeline (this
// session's SSE events filtered to the task) · action row (Accept / Request
// changes) + merge affordance, all gated as the previous rail gated them.

// rolesOf resolves a seat id to its roster roles (for the ant caste). Unknown
// seats (or empty id) → [] which casteForRoles maps to the builder caste.
function rolesOf(state: State, seatId: string): string[] {
  return state.agents.find((a) => a.id === seatId)?.roles ?? [];
}

// specIsPath: a spec value is treated as a repo path (vs. inline text) when it
// looks like one — contains a slash or ends in .md. Real task specs are stored
// as a path to `.pact/tasks/<id>.md`; the board's inline-code prose is a mockup
// artifact, so we keep real rendering to two cases: path line or plain text.
function specIsPath(spec: string): boolean {
  return spec.includes("/") || spec.endsWith(".md");
}

function prefersReducedMotion(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches
  );
}

export function RightRail({
  state,
  events,
  selected,
  project,
  author,
  onSelect,
}: {
  state: State;
  events: PactEvent[];
  selected: string;
  project?: string;
  author?: boolean;
  onSelect?: (id: string) => void;
}) {
  const bt = selected ? findTask(state, selected) : undefined;
  const [reason, setReason] = useState("");
  const [showChanges, setShowChanges] = useState(false);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  const close = () => onSelect?.("");
  // Whether the panel actually RENDERS. `selected` alone is not enough: after a
  // project switch (or in replay) the id may not resolve to a task — gating the
  // listeners on `open` keeps an invisible panel from stealing Esc from real
  // modals and the canvas chains.
  const open = !!selected && !!bt;
  const panelRef = useRef<HTMLElement>(null);

  // The panel owns its own Esc while open, stopping propagation so it closes
  // before the canvas/menu Esc chains see the key. Capture phase so we win the
  // race against any window-level listener those chains install.
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        close();
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  // Focus management (WAI-ARIA dialog, same contract as ui/Modal): move focus
  // into the panel on open, restore it to the opener on close. The aside is
  // focusable (tabIndex -1) so there is always a target even before the action
  // buttons render.
  useEffect(() => {
    if (!open) return;
    const prev = document.activeElement as HTMLElement | null;
    panelRef.current?.focus();
    return () => prev?.focus();
  }, [open]);

  // Reset the per-task transient UI (changes note + error) when the target
  // task changes or the panel closes.
  useEffect(() => {
    setReason("");
    setShowChanges(false);
    setErr("");
    setBusy(false);
  }, [selected]);

  if (!selected || !bt) return null;

  const { task, feature } = bt;
  const reviewable = task.status === "awaiting_review";
  const mergeable = canMergeFeature(state, feature);
  const reduced = prefersReducedMotion();

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setErr("");
    try {
      await fn();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const ownerCaste = casteForRoles(rolesOf(state, task.owner));
  const reviewerCaste = casteForRoles(rolesOf(state, task.reviewer));

  // Per-task timeline from this session's SSE events. The events prop only
  // accumulates events seen since the page loaded — history before load is NOT
  // shown (labelled "this session"). Newest-first.
  const timeline = events.filter((e) => e.task_id === task.id).reverse();

  // Time-in-flight: relTime of the EARLIEST observed event for this task this
  // session (the first assign/join/in_progress-ish event we saw). If no event
  // for the task is in the stream — e.g. it pre-dates this session — the chip
  // is omitted (relTime("") === "").
  const firstEventTs = timeline.length ? timeline[timeline.length - 1].ts : "";
  const inFlight = relTime(firstEventTs);

  return (
    <>
      {/* Scrim: dims the content behind and closes on click. */}
      <div
        data-testid="panel-scrim"
        onClick={close}
        className="absolute inset-0 z-40 bg-black/45"
        style={
          reduced
            ? undefined
            : {
                animation: `panel-scrim-in var(--motion-layout) var(--motion-ease)`,
              }
        }
      />
      <aside
        ref={panelRef}
        tabIndex={-1}
        data-testid="task-panel"
        role="dialog"
        aria-modal="true"
        aria-label={`Task ${task.id}`}
        className="absolute right-0 top-0 bottom-0 z-50 flex w-[340px] flex-col overflow-y-auto border-l border-[var(--color-border-subtle)] bg-[#20222F] shadow-[0_10px_32px_rgba(0,0,0,.4)]"
        style={
          reduced
            ? undefined
            : { animation: `panel-slide-in var(--motion-layout) var(--motion-ease)` }
        }
      >
        {/* Header */}
        <div className="border-b border-white/[0.07] bg-[linear-gradient(170deg,rgba(147,180,242,.08),transparent_70%)] px-4 pb-3 pt-3.5">
          <div className="mono text-[11px] text-white/45">
            {task.id} · {feature}
          </div>
          <div className="mt-0.5 flex items-center gap-[9px] text-[15px] font-[650] text-white">
            <span>{task.id}</span>
            <Badge color="role-design">{task.status.replace(/_/g, " ")}</Badge>
          </div>
          <div className="mt-[9px] flex gap-3.5 text-[10.5px] text-white/50">
            <span className="flex items-center gap-1">
              {task.owner && <Ant caste={ownerCaste} size={14} title={task.owner} />}
              <span className="mono">{task.owner || "—"}</span>
              <span className="text-white/30">→</span>
              {task.reviewer && <Ant caste={reviewerCaste} size={14} title={task.reviewer} />}
              <span className="mono">{task.reviewer || "—"}</span>
            </span>
            {inFlight && <span>{inFlight} in flight</span>}
          </div>
        </div>

        {/* Spec */}
        <div className="border-b border-white/[0.06] px-4 py-3">
          <div className="mb-2 text-[9.5px] uppercase tracking-[.6px] text-white/35">
            Spec{task.spec && specIsPath(task.spec) ? ` · ${task.spec}` : ""}
          </div>
          {task.spec ? (
            specIsPath(task.spec) ? (
              <div>
                <div className="mono text-[10.5px] text-[#9FE8BE]">{task.spec}</div>
                <div className="mt-1 text-[10px] text-white/35">(task spec file in repo)</div>
              </div>
            ) : (
              <div className="text-[11.5px] leading-[1.65] text-white/75">{task.spec}</div>
            )
          ) : (
            <div className="text-[11.5px] text-white/40">(no spec)</div>
          )}
        </div>

        {/* Evidence */}
        <div className="border-b border-white/[0.06] px-4 py-3">
          <div className="mb-2 text-[9.5px] uppercase tracking-[.6px] text-white/35">Evidence</div>
          <pre
            className={[
              "mono whitespace-pre-wrap text-[10.5px] leading-[1.6]",
              task.evidence ? "text-[#9FE8BE]" : "text-white/40",
            ].join(" ")}
          >
            {task.evidence || "(none yet)"}
          </pre>
        </div>

        {/* Timeline */}
        <div className="border-b border-white/[0.06] px-4 py-3">
          <div className="mb-2 text-[9.5px] uppercase tracking-[.6px] text-white/35">
            Timeline <span className="text-white/25">· this session</span>
          </div>
          {timeline.length === 0 ? (
            <div className="text-[11px] text-white/40">(no events this session)</div>
          ) : (
            timeline.map((e) => {
              const caste = casteForRoles(rolesOf(state, e.agent_id));
              // TODO(W3): each entry gets a "replay to here" jump that calls
              // enterReplay for this event's position (timeline→replay tie-in).
              return (
                <div key={e.event_id} data-testid="panel-timeline-entry" className="mb-2.5 flex gap-[9px]">
                  <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-[6px]">
                    <Ant caste={caste} size={14} title={e.agent_id} />
                  </span>
                  <div className="text-[11px] leading-[1.45] text-white/75">
                    <b>{e.event_type}</b>
                    {e.feature ? <> · {e.feature}</> : null}
                    <br />
                    <span className="mono text-[10px] text-white/40">
                      {e.agent_id} · {relTime(e.ts) || "now"}
                    </span>
                  </div>
                </div>
              );
            })
          )}
        </div>

        {/* Action row — author + live (App passes author && !replaying) +
            awaiting_review, preserving the previous rail's gating exactly. */}
        {author && project && reviewable && (
          <div className="px-4 py-3">
            <div className="flex gap-2">
              <Button
                variant="primary"
                className="flex-1"
                disabled={busy}
                onClick={() => run(() => postVerb(project, "accept", { task: task.id }))}
              >
                ✓ Accept
              </Button>
              <Button
                variant="ghost"
                className="flex-1"
                disabled={busy}
                onClick={() => setShowChanges((s) => !s)}
              >
                ↺ Changes…
              </Button>
            </div>
            {showChanges && (
              <div className="mt-2">
                <textarea
                  aria-label="changes reason"
                  className="h-16 w-full resize-y rounded border border-[var(--color-border-subtle)] bg-[#0d1117] px-1.5 py-1 text-[11px] text-[var(--color-text-1)]"
                  placeholder="reason (required to request changes)"
                  value={reason}
                  onChange={(ev) => setReason(ev.target.value)}
                />
                <Button
                  variant="danger"
                  size="sm"
                  className="mt-1.5"
                  disabled={busy || !reason.trim()}
                  onClick={() =>
                    run(async () => {
                      await postVerb(project, "changes", { task: task.id, reason: reason.trim() });
                      setReason("");
                      setShowChanges(false);
                    })
                  }
                >
                  Request changes
                </Button>
              </div>
            )}
          </div>
        )}

        {/* Merge affordance — kept from the previous rail: shown for authors in
            the selected task's feature context, enabled only when every task in
            the feature is accepted (server is authoritative). */}
        {author && project && (
          <div className="px-4 pb-4 pt-1">
            <div className="mb-1 text-[9.5px] uppercase tracking-[.6px] text-white/35">
              Feature · {feature}
            </div>
            <Button
              size="sm"
              disabled={busy || !mergeable}
              title={mergeable ? "" : "all tasks must be accepted"}
              onClick={() => run(() => postVerb(project, "merge", { feature }))}
            >
              Merge {feature}
            </Button>
          </div>
        )}

        {err && <p className="px-4 pb-4 whitespace-pre-wrap text-[11px] text-[var(--color-danger)]">{err}</p>}
      </aside>
    </>
  );
}
