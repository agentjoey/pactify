// E2E fixtures — the hermetic protocol snapshot the mock server serves. Shapes
// are byte-for-byte the real serve JSON (internal/serve/dto.go + api.go):
//   • /api/projects item: {id,name,path,project,feature_count,awaiting_count}
//     (NO author/seat field — author identity comes from GET /api/acting-seat).
//   • /api/projects/{id}/state: StateDTO {project,agents,features[…tasks],awaiting_count}.
//   • GET /api/acting-seat: {seat} — non-empty ⇒ the dashboard authors.
//
// Roster (spec §4 / Task 5): claude-opus = orchestrator+reviewer, opencode = worker.
// One feature f1 (branch feat-f1) with two tasks:
//   t1 owner=opencode reviewer=claude-opus status=in_progress
//   t2 owner=opencode reviewer=claude-opus status=assigned deps=[t1]
//
// Resulting Office statuses (lib/office.deriveOffice):
//   opencode   → BUSY (owns t1 in_progress)
//   claude-opus→ IDLE (reviews nothing awaiting_review, owns nothing) ← drop target.

export const PROJECT_ID = "p1";

// The acting seat — author view. claude-opus can author (orchestrator).
export const ACTING_SEAT = "claude-opus";

export const projects = () => [
  {
    id: PROJECT_ID,
    name: PROJECT_ID,
    path: `/tmp/${PROJECT_ID}`,
    project: PROJECT_ID,
    feature_count: 1,
    awaiting_count: 0,
  },
];

// initialState returns a FRESH StateDTO object each call (the mock server mutates
// its working copy via /__test/snapshot, so the seed must never be aliased).
export function initialState() {
  return {
    project: PROJECT_ID,
    agents: [
      { id: "claude-opus", roles: ["orchestrator", "reviewer"] },
      { id: "opencode", roles: ["worker"] },
    ],
    features: [
      {
        id: "f1",
        branch: "feat-f1",
        status: "active",
        tasks: [
          {
            id: "t1",
            owner: "opencode",
            reviewer: "claude-opus",
            status: "in_progress",
            spec: ".pact/tasks/t1.md",
            evidence: "",
            deps: [],
          },
          {
            id: "t2",
            owner: "opencode",
            reviewer: "claude-opus",
            status: "assigned",
            spec: ".pact/tasks/t2.md",
            evidence: "",
            deps: ["t1"],
          },
        ],
      },
    ],
    awaiting_count: 0,
  };
}

// snapshotT2AwaitingReview is the new state pushed by the drag-during-sse test:
// t2 moves assigned → in_progress. It deliberately changes ONLY t2 (t1 untouched)
// so the test can assert the dragged-elsewhere node (t1) is not teleported by the
// SSE-driven merge. awaiting_count stays 0 (no awaiting_review here).
export function snapshotT2InProgress() {
  const s = initialState();
  s.features[0].tasks[1].status = "in_progress";
  return s;
}
