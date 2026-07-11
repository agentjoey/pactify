import type { Task } from "../../lib/types";

type PipelineStatus = "assigned" | "in_progress" | "awaiting_review" | "changes_requested" | "accepted";

const META: Record<PipelineStatus, { glyph: string; color: string }> = {
  assigned: { glyph: "·", color: "var(--color-text-3)" },
  in_progress: { glyph: "⚡", color: "var(--color-role-design)" },
  awaiting_review: { glyph: "◉", color: "var(--color-warn)" },
  changes_requested: { glyph: "↺", color: "var(--color-role-ops)" },
  accepted: { glyph: "✓", color: "var(--color-success)" },
};

export interface MiniPipelineProps {
  tasks: Task[];
  merge?: boolean;
  testId?: string;
}

export function MiniPipeline({ tasks, merge, testId }: MiniPipelineProps) {
  return (
    <div data-testid={testId} className="flex flex-wrap items-center gap-0">
      {tasks.map((t, i) => {
        const status = (t.status as PipelineStatus) in META ? (t.status as PipelineStatus) : "assigned";
        const meta = META[status];
        const working = status === "in_progress";
        return (
          <div key={t.id} className="flex items-center">
            <span
              className="inline-flex items-center gap-[5px] rounded-lg border px-[9px] py-[5px] font-mono text-[10.5px]"
              style={{
                color: meta.color,
                background: `color-mix(in srgb, ${meta.color} 12%, transparent)`,
                borderColor: `color-mix(in srgb, ${meta.color} 30%, transparent)`,
              }}
            >
              {working ? <EqBars color={meta.color} /> : <span>{meta.glyph}</span>}
              {t.id}
            </span>
            {i < tasks.length - 1 && <span className="h-[2px] w-[14px] bg-[var(--border)]" />}
          </div>
        );
      })}
      {merge && (
        <>
          {tasks.length > 0 && <span className="h-[2px] w-[14px] bg-[var(--border)]" />}
          <span className="inline-flex items-center gap-[5px] rounded-lg border border-dashed border-[rgba(255,255,255,0.16)] px-[9px] py-[5px] font-mono text-[10.5px] text-[var(--color-text-3)]">
            ▸ merge
          </span>
        </>
      )}
    </div>
  );
}

function EqBars({ color }: { color: string }) {
  return (
    <span className="inline-flex items-end gap-[1.5px]" style={{ height: 9 }}>
      <span
        className="live-eq-bar rounded-sm"
        style={{ width: 2, height: "100%", background: color, animationDelay: "0s" }}
      />
      <span
        className="live-eq-bar rounded-sm"
        style={{ width: 2, height: "100%", background: color, animationDelay: "0.2s" }}
      />
      <span
        className="live-eq-bar rounded-sm"
        style={{ width: 2, height: "100%", background: color, animationDelay: "0.4s" }}
      />
    </span>
  );
}
