import type { State, Task } from "./types";
import { unmetDeps, flatTasks } from "./comms";

// --- Office mode model (T9, spec §3) --------------------------------------
//
// deriveOffice folds a snapshot into one desk per JOINED seat plus a shipped
// tray. Pure: reads only its argument, never mutates. The view (T10) maps these
// shapes straight onto desk stations / zones / status badges.

export type DeskStatus = "busy" | "review_due" | "waiting" | "idle";

// One row in a desk's "等回音 waiting on" zone. `on` is a SEAT id when reason is
// "review" (the reviewer we're waiting on) and a TASK id when reason is "dep"
// (the unmet prerequisite blocking us).
export interface WaitingItem { task: Task; on: string; reason: "review" | "dep" }

export interface DeskModel {
  seatId: string;
  roles: string[];
  status: DeskStatus;
  doing: Task[];           // tasks they own in assigned | in_progress | changes_requested
  inbox: Task[];           // awaiting_review tasks where they are reviewer
  waitingOn: WaitingItem[]; // own awaiting_review (on=reviewer, reason "review")
                            // + own non-accepted tasks per unmet dep (on=depId, reason "dep")
}

// deriveOffice — desks for joined seats (roster order) + shipped tray.
//
// Status precedence (spec §3, NORMATIVE): busy > review_due > waiting > idle.
//   busy       = owns an in_progress task (green border + equalizer)
//   review_due = inbox non-empty (a review is owed)
//   waiting    = waitingOn non-empty, nothing busy/review (output parked elsewhere)
//   idle       = none of the above (drop-target hint)
//
// DECISION — "only-assigned" desk: a desk whose only work is `assigned` (never
// `in_progress`) is NOT busy (spec: BUSY = owns in_progress specifically), has an
// empty inbox and empty waitingOn (assigned ≠ awaiting_review, and no unmet dep),
// so it resolves to IDLE even though `doing` is non-empty. We surface those tasks
// in `doing` regardless; the status reads idle-with-doing. This is the resolution
// the plan asks us to pick and document — `assigned` work sits on the desk but the
// agent has not started, so it is not "busy", and idle's drop hint is harmless.
export function deriveOffice(state: State): { desks: DeskModel[]; shipped: Task[] } {
  const tasks = flatTasks(state);
  const byId = new Map(tasks.map((t) => [t.id, t]));

  const desks: DeskModel[] = state.agents.map((seat) => {
    // changes_requested is REWORK bounced back to the owner — per board5 the
    // parcel returns to the owner's desk, so it lives in `doing` (otherwise a
    // changes_requested task would vanish from every zone in the office view).
    const doing = tasks.filter(
      (t) =>
        t.owner === seat.id &&
        (t.status === "assigned" || t.status === "in_progress" || t.status === "changes_requested"),
    );
    const inbox = tasks.filter(
      (t) => t.reviewer === seat.id && t.status === "awaiting_review",
    );

    const waitingOn: WaitingItem[] = [];
    for (const t of tasks) {
      if (t.owner !== seat.id) continue;
      // (a) own work handed off for review — waiting on the reviewer.
      if (t.status === "awaiting_review") {
        waitingOn.push({ task: t, on: t.reviewer, reason: "review" });
      }
      // (b) own non-accepted work blocked by unmet deps — one item per blocker.
      // Reuses comms.ts unmetDeps so "blocked" can never drift between lenses.
      if (t.status !== "accepted") {
        for (const dep of unmetDeps(t, byId)) {
          waitingOn.push({ task: t, on: dep, reason: "dep" });
        }
      }
    }

    const ownsInProgress = doing.some((t) => t.status === "in_progress");
    const status: DeskStatus = ownsInProgress
      ? "busy"
      : inbox.length > 0
        ? "review_due"
        : waitingOn.length > 0
          ? "waiting"
          : "idle";

    return { seatId: seat.id, roles: seat.roles, status, doing, inbox, waitingOn };
  });

  // shipped: accepted tasks across all features. Appearance order in `tasks` is
  // stable (feature order, then task order); the tray shows most-recent-first, so
  // we reverse — the last-appearing accepted task slides in at the front.
  const shipped = tasks.filter((t) => t.status === "accepted").reverse();

  return { desks, shipped };
}
