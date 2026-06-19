// StatusPill — the pact-state language as one atom. Every place a task/seat/
// feature shows its state uses this, so the state vocabulary is consistent and
// color-coded app-wide (cards, nodes, Live, kanban). `live` states carry a
// gently pulsing dot (work in flight); `shipped` is a filled pill (a terminal,
// celebrated state). Colors come from tokens, so the light/dark palette drives
// them.
export type PactStatus =
  | "assigned"
  | "in_progress"
  | "awaiting_review"
  | "changes_requested"
  | "accepted"
  | "shipped"
  | "escalated"
  | "idle";

type Spec = { label: string; color: string; live?: boolean; filled?: boolean };

const MAP: Record<PactStatus, Spec> = {
  assigned: { label: "assigned", color: "var(--color-text-3)" },
  in_progress: { label: "working", color: "var(--color-role-design)", live: true },
  awaiting_review: { label: "review", color: "var(--color-warn)" },
  changes_requested: { label: "changes", color: "var(--color-role-ops)" },
  accepted: { label: "accepted", color: "var(--color-success)" },
  shipped: { label: "shipped", color: "var(--color-success)", filled: true },
  escalated: { label: "escalated", color: "var(--color-danger)", live: true },
  idle: { label: "idle", color: "var(--color-text-3)" },
};

// normalize maps orchestrate action/phase strings and loose inputs onto a
// PactStatus, so callers can pass either a task status or a runtime phase.
export function normalizeStatus(s: string): PactStatus {
  switch (s) {
    case "run_owner":
    case "owner working":
      return "in_progress";
    case "run_reviewer":
    case "reviewer reviewing":
      return "awaiting_review";
    case "merge":
    case "merging feature":
    case "done":
      return "shipped";
    case "stuck":
      return "escalated";
    default:
      return (s in MAP ? (s as PactStatus) : "idle");
  }
}

// statusColor resolves any task/phase string to the pact-state language color
// (the same hue the StatusPill shows), so transition cues elsewhere (the canvas
// pulse) read with the SAME color vocabulary: accepted→green, changes→amber,
// awaiting→warn, working→blue, escalated→red.
export function statusColor(s: string): string {
  return MAP[normalizeStatus(s)].color;
}

export function StatusPill({
  status,
  label,
  className = "",
}: {
  status: PactStatus;
  label?: string;
  className?: string;
}) {
  const s = MAP[status];
  const text = label ?? s.label;
  if (s.filled) {
    return (
      <span
        data-testid="status-pill"
        data-status={status}
        className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10.5px] font-medium ${className}`}
        style={{ background: s.color, color: "var(--color-bg-page)" }}
      >
        {text}
      </span>
    );
  }
  return (
    <span
      data-testid="status-pill"
      data-status={status}
      className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10.5px] font-medium ${className}`}
      style={{
        color: s.color,
        background: `color-mix(in srgb, ${s.color} 12%, transparent)`,
        border: `1px solid color-mix(in srgb, ${s.color} 30%, transparent)`,
      }}
    >
      <span
        aria-hidden
        className={s.live ? "status-pill-dot-live" : ""}
        style={{
          width: 5,
          height: 5,
          borderRadius: 999,
          background: s.color,
          display: "inline-block",
        }}
      />
      {text}
    </span>
  );
}
