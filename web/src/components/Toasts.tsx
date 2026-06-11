import type { State } from "../lib/types";
import { allTasks } from "../lib/derive";

// diffAwaiting compares two consecutive state snapshots and returns the ids of
// tasks that newly ENTERED awaiting_review (were a different status — or absent
// — in prev, are awaiting_review in next). Pure + deterministic so the toast
// trigger is unit-testable without React. A task already awaiting_review in
// prev does NOT re-fire.
export function diffAwaiting(prev: State, next: State): string[] {
  const prevStatus = new Map<string, string>();
  for (const b of allTasks(prev)) prevStatus.set(b.task.id, b.task.status);
  const out: string[] = [];
  for (const b of allTasks(next)) {
    if (b.task.status === "awaiting_review" && prevStatus.get(b.task.id) !== "awaiting_review") {
      out.push(b.task.id);
    }
  }
  return out;
}

// kind drives the left-border accent: the default (review notifications) uses
// the design hue; "error" is danger-tinted so a failed author action reads as a
// problem, not a status ping.
export type Toast = { id: number; text: string; kind?: "error" };

// Toasts renders the active toast stack (newest at the bottom). The 5s lifetime
// and max-3 cap are owned by App, which feeds this list; this component is a
// pure presenter.
export function Toasts({ toasts }: { toasts: Toast[] }) {
  if (toasts.length === 0) return null;
  return (
    <div data-testid="toasts" className="pointer-events-none fixed bottom-4 right-4 z-[60] flex flex-col gap-2">
      {toasts.map((t) => (
        <div
          key={t.id}
          data-testid={t.kind === "error" ? "toast-error" : "toast"}
          className={`pointer-events-auto rounded-md border border-[var(--color-border-subtle)] border-l-2 bg-[var(--color-bg-raised)] px-3 py-2 text-xs text-[var(--color-text-1)] shadow-[var(--shadow-raised)] ${
            t.kind === "error"
              ? "border-l-[var(--color-danger)]"
              : "border-l-[var(--color-role-design)]"
          }`}
        >
          {t.text}
        </div>
      ))}
    </div>
  );
}
