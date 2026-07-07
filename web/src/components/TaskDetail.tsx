import { useEffect, useRef, useState } from "react";
import type { PactEventDetail } from "../lib/types";
import { useDataSource } from "../lib/datasource";
import { relTime } from "../lib/reltime";
import { Badge } from "./ui/Badge";

type BadgeColor = Parameters<typeof Badge>[0]["color"];

function eventColor(eventType: string): BadgeColor {
  switch (eventType) {
    case "checkpoint":
      return "role-dev";
    case "assign":
      return "role-design";
    case "accept":
      return "success";
    case "changes_requested":
      return "danger";
    case "merge":
      return "role-product";
    default:
      return "role-ops";
  }
}

function prefersReducedMotion(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches
  );
}

function tsISO(d: PactEventDetail): string {
  if (typeof d.body.ts === "string") return d.body.ts;
  return new Date(d.ts).toISOString();
}

function strBody(body: Record<string, unknown>, key: string): string {
  const v = body[key];
  return typeof v === "string" ? v : "";
}

function strPayload(body: Record<string, unknown>, key: string): string {
  const payload = body.payload;
  if (!payload || typeof payload !== "object") return "";
  const v = (payload as Record<string, unknown>)[key];
  return typeof v === "string" ? v : "";
}

export function TaskDetail({
  project,
  taskId,
  onClose,
}: {
  project: string;
  taskId: string;
  onClose?: () => void;
}) {
  const src = useDataSource();
  const [events, setEvents] = useState<PactEventDetail[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState("");
  const panelRef = useRef<HTMLElement>(null);

  // Hosted-mode sources expose the decrypted event history; local sources render
  // the existing RightRail instead, so this panel gracefully degrades to null.
  // NB: this guard must NOT early-return before the hooks below — the parent
  // keeps this component mounted and only toggles taskId (""→selected), so a
  // conditional return here would change the hook count between renders and
  // crash the whole tree ("rendered more hooks than during the previous
  // render"). Keep every hook unconditional; gate the effect bodies and the
  // final render instead.
  const getEvents = src.getEvents;
  const active = Boolean(taskId && getEvents);

  useEffect(() => {
    if (!active || !getEvents) return;
    let alive = true;
    setLoading(true);
    setErr("");
    setEvents(null);
    getEvents(project)
      .then((evs) => {
        if (!alive) return;
        const filtered = evs
          .filter((e) => e.task === taskId || strBody(e.body, "task_id") === taskId)
          .sort((a, b) => b.ts - a.ts);
        setEvents(filtered);
      })
      .catch((e) => setErr(e instanceof Error ? e.message : String(e)))
      .finally(() => alive && setLoading(false));
    return () => {
      alive = false;
    };
  }, [project, taskId, getEvents, active]);

  useEffect(() => {
    if (!active) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose?.();
      }
    };
    window.addEventListener("keydown", onKey, true);
    panelRef.current?.focus();
    return () => window.removeEventListener("keydown", onKey, true);
  }, [onClose, active]);

  const reduced = prefersReducedMotion();

  // All hooks have run unconditionally above; only now is it safe to bail out.
  if (!active) return null;

  return (
    <>
      <div
        data-testid="task-detail-scrim"
        onClick={onClose}
        className="absolute inset-0 z-40 bg-black/10"
        style={
          reduced
            ? undefined
            : { animation: `panel-scrim-in var(--motion-layout) var(--motion-ease)` }
        }
      />
      <aside
        ref={panelRef}
        tabIndex={-1}
        data-testid="task-detail-panel"
        role="dialog"
        aria-modal="true"
        aria-label={`Task ${taskId} history`}
        className="absolute right-3 top-3 bottom-3 z-50 flex w-[330px] flex-col overflow-y-auto rounded-2xl border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] shadow-[var(--shadow-raised)]"
        style={
          reduced
            ? undefined
            : { animation: `panel-slide-in var(--motion-layout) var(--motion-ease)` }
        }
      >
        {/* Header */}
        <div className="rounded-t-2xl border-b border-[var(--color-border-subtle)] bg-[linear-gradient(170deg,color-mix(in_srgb,var(--color-role-design)_8%,transparent),transparent_70%)] px-4 pb-3 pt-3.5">
          <div className="mono text-[11px] text-[var(--color-text-3)]">{taskId}</div>
          <div className="mt-0.5 flex items-center justify-between text-[15px] font-[650] text-[var(--color-text-1)]">
            <span>Event history</span>
            <button
              type="button"
              data-testid="task-detail-close"
              onClick={onClose}
              className="rounded-md px-2 py-1 text-[11px] text-[var(--color-text-3)] hover:text-[var(--color-text-1)]"
              aria-label="Close"
            >
              ✕
            </button>
          </div>
        </div>

        {/* Body */}
        <div className="flex-1 px-4 py-3">
          {loading && (
            <div data-testid="task-detail-loading" className="text-[11px] text-[var(--color-text-3)]">
              Loading event history…
            </div>
          )}
          {err && (
            <div data-testid="task-detail-error" className="text-[11px] text-[var(--color-danger)]">
              {err}
            </div>
          )}
          {!loading && !err && events && events.length === 0 && (
            <div data-testid="task-detail-empty" className="text-[11px] text-[var(--color-text-3)]">
              (no events for this task)
            </div>
          )}
          {!loading && !err && events && events.length > 0 && (
            <div className="flex flex-col gap-3">
              {events.map((e) => (
                <div
                  key={e.seq}
                  data-testid="task-detail-event"
                  className="rounded-xl border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] px-3 py-2.5"
                >
                  <div className="mb-1.5 flex items-center justify-between">
                    <Badge color={eventColor(e.eventType)}>{e.eventType.replace(/_/g, " ")}</Badge>
                    <span className="mono text-[10px] text-[var(--color-text-3)]">
                      {relTime(tsISO(e)) || "now"}
                    </span>
                  </div>
                  <EventFields eventType={e.eventType} body={e.body} />
                  {e.feature && (
                    <div className="mt-1.5 mono text-[10px] text-[var(--color-text-3)]">
                      feature · {e.feature}
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      </aside>
    </>
  );
}

function EventFields({
  eventType,
  body,
}: {
  eventType: string;
  body: Record<string, unknown>;
}) {
  switch (eventType) {
    case "checkpoint": {
      const evidence = strPayload(body, "evidence") || strBody(body, "evidence");
      return evidence ? (
        <div>
          <div className="text-[9.5px] uppercase tracking-[.6px] text-[var(--color-text-3)]">Evidence</div>
          <pre className="mono mt-1 whitespace-pre-wrap text-[10.5px] leading-[1.6] text-[var(--color-role-dev-ink)]">
            {evidence}
          </pre>
        </div>
      ) : null;
    }
    case "changes_requested": {
      const reason = strPayload(body, "reason");
      return reason ? (
        <div>
          <div className="text-[9.5px] uppercase tracking-[.6px] text-[var(--color-text-3)]">Reason</div>
          <div className="mt-1 text-[11px] leading-[1.6] text-[var(--color-text-1)]">{reason}</div>
        </div>
      ) : null;
    }
    case "assign": {
      const owner = strPayload(body, "owner");
      const reviewer = strPayload(body, "reviewer");
      const spec = strPayload(body, "spec");
      return (
        <div className="flex flex-col gap-1 text-[11px] leading-[1.6] text-[var(--color-text-1)]">
          {owner && (
            <div>
              <span className="text-[var(--color-text-3)]">owner</span>{" "}
              <span className="mono">{owner}</span>
            </div>
          )}
          {reviewer && (
            <div>
              <span className="text-[var(--color-text-3)]">reviewer</span>{" "}
              <span className="mono">{reviewer}</span>
            </div>
          )}
          {spec && (
            <div>
              <span className="text-[var(--color-text-3)]">spec</span>{" "}
              <span className="mono text-[var(--color-role-dev-ink)]">{spec}</span>
            </div>
          )}
        </div>
      );
    }
    default:
      return null;
  }
}
