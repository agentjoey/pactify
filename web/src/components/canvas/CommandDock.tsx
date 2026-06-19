import { useState } from "react";
import { CableMark } from "../shell/CableMark";

// CommandDock — the canvas's signature NL command bar (dark handoff §Canvas). A
// frosted pill pinned bottom-center: a goal input + suggested recipe chips + a
// concurrency segment + Run. Submitting (Run or ⌘↵) hands the goal to the
// existing dispatch flow via onRun; the dock itself owns no backend.
const RECIPES = ["配方 review-harden", "planner 拆解"];
const CONCURRENCY = [1, 2, 4] as const;

export function CommandDock({ onRun }: { onRun: (goal: string, opts: { concurrency: number }) => void }) {
  const [goal, setGoal] = useState("");
  const [concurrency, setConcurrency] = useState<number>(2);

  function run() {
    const g = goal.trim();
    if (!g) return;
    onRun(g, { concurrency });
  }

  return (
    <div
      data-testid="command-dock"
      className="pointer-events-auto absolute bottom-[18px] left-1/2 z-20 w-[600px] max-w-[calc(100%-300px)] -translate-x-1/2 rounded-[16px] border p-[13px]"
      style={{
        background: "color-mix(in srgb, var(--color-bg-raised) 86%, transparent)",
        borderColor: "var(--color-border-strong)",
        backdropFilter: "blur(16px) saturate(1.1)",
        WebkitBackdropFilter: "blur(16px) saturate(1.1)",
        boxShadow: "0 18px 48px rgba(0,0,0,.55)",
      }}
    >
      {/* Row 1 — cable mark + goal input + ⌘↵ hint */}
      <div className="flex items-center gap-2.5">
        <CableMark />
        <input
          data-testid="command-dock-input"
          value={goal}
          onChange={(e) => setGoal(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) run();
          }}
          placeholder="描述一个目标，⌘↵ 拆解派发…"
          className="min-w-0 flex-1 bg-transparent text-[13px] text-[var(--color-text-1)] outline-none placeholder:text-[var(--color-text-3)]"
        />
        <span className="mono rounded-[5px] border border-[var(--color-border-subtle)] px-1.5 py-0.5 text-[10px] text-[var(--color-text-3)]">
          ⌘↵
        </span>
      </div>

      {/* Row 2 — suggestions + concurrency + Run */}
      <div className="mt-[11px] flex items-center gap-2">
        <span className="text-[10.5px] text-[var(--color-text-3)]">建议</span>
        {RECIPES.map((r) => (
          <button
            key={r}
            type="button"
            data-testid="command-dock-recipe"
            onClick={() => setGoal(r)}
            className="rounded-[7px] border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] px-2.5 py-1 text-[11px] text-[var(--color-text-2)] hover:text-[var(--color-text-1)]"
          >
            {r}
          </button>
        ))}
        <div className="ml-auto flex items-center gap-2">
          <div className="inline-flex rounded-[7px] border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] p-[3px]">
            {CONCURRENCY.map((n) => {
              const on = n === concurrency;
              return (
                <button
                  key={n}
                  type="button"
                  data-testid="command-dock-conc"
                  aria-pressed={on}
                  onClick={() => setConcurrency(n)}
                  className="rounded-[5px] px-2.5 py-1 text-[11px] font-medium"
                  style={on ? { background: "#222b3a", color: "var(--color-text-1)", boxShadow: "var(--shadow-card)" } : { color: "var(--color-text-3)" }}
                >
                  {n}
                </button>
              );
            })}
          </div>
          <button
            type="button"
            data-testid="command-dock-run"
            onClick={run}
            disabled={!goal.trim()}
            className="rounded-[7px] px-3.5 py-1.5 text-[12px] font-medium disabled:opacity-40"
            style={{ background: "var(--color-role-design)", color: "var(--color-bg-page)" }}
          >
            Run ▸
          </button>
        </div>
      </div>
    </div>
  );
}
