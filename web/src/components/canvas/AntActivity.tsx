import { MessengerAnt, CarrierAnt } from "./edges/AntEdge";

// AntActivity — the always-on, state-driven ant motion overlay for a canvas task
// node (Phase 3 increment A). It plays one of three motions that read at a
// glance, NO event history required (derived purely from the task's live
// status):
//   in_progress      → a carrier ant crawls back and forth (steady "working")
//   awaiting_review  → a messenger ant orbits the node (holding for review)
//   changes_requested→ an amber messenger frets back and forth (rework)
// Other statuses (assigned / accepted / idle) are calm — no ant. The motion
// lives in CSS (.ant-act-*), gated by prefers-reduced-motion; under reduced
// motion we render nothing and the StatusPill carries the meaning.

function prefersReducedMotion(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches
  );
}

type Spec = { cls: string; color: string; Ant: typeof MessengerAnt };
const ACTIVITY: Record<string, Spec> = {
  in_progress: { cls: "ant-work", color: "var(--color-role-dev-ink)", Ant: CarrierAnt },
  awaiting_review: { cls: "ant-wait", color: "var(--color-warn)", Ant: MessengerAnt },
  changes_requested: { cls: "ant-changes", color: "var(--color-role-ops)", Ant: MessengerAnt },
};

export function AntActivity({ status }: { status: string }) {
  const spec = ACTIVITY[status];
  if (!spec) return null;
  if (prefersReducedMotion()) return null;
  const Ant = spec.Ant;
  return (
    <div className={`ant-act ${spec.cls}`} data-testid="ant-activity" data-activity={status} aria-hidden>
      <span className="ant-act-runner">
        <svg width="13" height="9" viewBox="-12 -7 24 14">
          <Ant color={spec.color} />
        </svg>
      </span>
    </div>
  );
}
