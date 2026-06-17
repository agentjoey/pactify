// E2E fixtures — the hermetic protocol snapshot the mock server serves. Shapes
// are byte-for-byte the real serve JSON (internal/serve/dto.go + api.go):
//   • /api/projects item: {id,name,path,project,feature_count,awaiting_count,group?}
//   • /api/registry item: {name,path,group?,status:{valid,seats,lastEventTs?}}
//   • /api/fs/browse: {path,parent,entries:[{name,path,isGit,hasPact}]}
//   • /api/setup/suggest: {bindings:[{seat,kind,roles,drivable}],warnings}
//   • /api/setup/apply: {inited,wired:[{kind,seat,wrote,path,docOnly,snippet?}],notes}
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

// Mutable registry used by the mock server. resetRegistry returns a fresh copy.
export function registry() {
  return [
    { name: PROJECT_ID, path: `/tmp/${PROJECT_ID}`, group: "", status: { valid: true, seats: 2, lastEventTs: "2026-01-01T00:00:00Z" } },
  ];
}

export function projects(registryRows) {
  return registryRows.map((p) => ({
    id: p.name,
    name: p.name,
    path: p.path,
    project: p.name,
    feature_count: p.name === PROJECT_ID ? 1 : 0,
    awaiting_count: 0,
    group: p.group || undefined,
  }));
}

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

// snapshotT2InProgress is the new state pushed by the drag-during-sse test:
// t2 moves assigned → in_progress. It deliberately changes ONLY t2 (t1 untouched)
// so the test can assert the dragged-elsewhere node (t1) is not teleported by the
// SSE-driven merge. awaiting_count stays 0 (no awaiting_review here).
export function snapshotT2InProgress() {
  const s = initialState();
  s.features[0].tasks[1].status = "in_progress";
  return s;
}

// Fs browse tree used by the project wizard e2e. /tmp/p1 already exists; /tmp/new
// is a git repo without .pact/; /tmp/existing has .pact/ and is not pre-registered.
export function browseTree() {
  return {
    "/": { path: "/", parent: "/", entries: [{ name: "tmp", path: "/tmp", isGit: false, hasPact: false }] },
    "/tmp": {
      path: "/tmp",
      parent: "/",
      entries: [
        { name: "p1", path: "/tmp/p1", isGit: true, hasPact: true },
        { name: "new", path: "/tmp/new", isGit: true, hasPact: false },
        { name: "existing", path: "/tmp/existing", isGit: true, hasPact: true },
      ],
    },
  };
}

export function setupSuggest() {
  return {
    bindings: [
      { seat: "claude-opus", kind: "claude-code", roles: ["orchestrator", "reviewer"], drivable: true },
      { seat: "opencode", kind: "opencode", roles: ["worker"], drivable: true },
    ],
    warnings: [],
  };
}
