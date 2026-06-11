// Task lifecycle projection — the shared "genome" both the kanban card and the
// canvas task node read to draw their medallion icon, status wash and the four
// lifecycle progress bars. See plan T4 + spec §3/§4.

// lifecycleStage folds a protocol status onto the 4-stage delivery track:
//   0 assigned → 1 in_progress → 2 review (awaiting_review / changes_requested)
//   → 3 accepted. Unknown statuses (drafts, anything unexpected) sit at 0.
export function lifecycleStage(status: string): 0 | 1 | 2 | 3 {
  switch (status) {
    case "in_progress":
      return 1;
    case "awaiting_review":
    case "changes_requested":
      return 2;
    case "accepted":
      return 3;
    case "assigned":
    default:
      return 0;
  }
}

// statusColorVar resolves a status to the CSS color (a tokens.css var) that
// drives the card's --st: the medallion squircle, the 9% background wash and the
// done/current lifecycle bars. Neutral statuses use text-2 so they recede; the
// review flow borrows reviewer/worker/danger hues to echo the kanban columns.
export function statusColorVar(status: string): string {
  switch (status) {
    case "in_progress":
    case "accepted":
      return "var(--color-role-dev)";
    case "awaiting_review":
      return "var(--color-role-design)";
    case "changes_requested":
      return "var(--color-danger)";
    case "assigned":
    default:
      return "var(--color-text-2)";
  }
}
