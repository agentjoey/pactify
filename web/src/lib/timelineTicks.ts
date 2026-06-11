// timelineTicks — pure tick-coloring for the replay ruler (board4 §③).
//
// Each timeline event gets a tick on the ruler, colored by its event TYPE so the
// shape of a project's history reads at a glance. Color mapping (spec §5 / board4
// caption), expressed as CSS custom-property references so it tracks the token
// palette:
//
//   init, assign           → --color-warn         (amber)  orchestration
//   join, checkpoint, accept → --color-success     (green)  worker/verify flow
//   changes (changes_requested) → --color-danger    (red)    rework requested
//   merge                  → --color-role-design  (blue)   feature shipped
//   awaiting_review        → --color-role-design  (blue)   review-flow
//   unknown / anything else → --color-text-3       (gray)
//
// The engine emits `changes_requested` (not `changes`) and never an
// `awaiting_review` event type (that is a derived STATUS) — both are mapped here
// anyway so synthetic/derived timelines and the spec's shorthand both color
// correctly.
export function tickColorVar(type: string): string {
  switch (type.toLowerCase()) {
    case "init":
    case "assign":
      return "var(--color-warn)";
    case "join":
    case "checkpoint":
    case "accept":
      return "var(--color-success)";
    case "changes":
    case "changes_requested":
      return "var(--color-danger)";
    case "merge":
    case "awaiting_review":
      return "var(--color-role-design)";
    default:
      return "var(--color-text-3)";
  }
}
