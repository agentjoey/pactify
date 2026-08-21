export interface Seat { id: string; roles: string[]; kind?: string }
export interface Task { id: string; owner: string; status: string; reviewer: string; spec: string; evidence: string; deps?: string[]; reviewers?: string[]; quorum?: number; accepts?: string[] }
export interface Feature { id: string; branch: string; status: string; tasks: Task[] }
export interface State { project: string; agents: Seat[]; features: Feature[]; awaiting_count: number }
export interface PactEvent { event_id: string; ts: string; agent_id: string; role: string; event_type: string; task_id: string; feature: string; payload: Record<string, unknown> }

/** Decrypted pact event with its relay header preserved. Returned by hosted-mode sources. */
export interface PactEventDetail {
  seq: number;
  eventType: string;
  task?: string | null;
  feature?: string | null;
  ts: number;
  body: Record<string, unknown>;
}

export interface ProjectMeta { id: string; name: string; path: string; project: string; feature_count: number; awaiting_count: number; group?: string }
export interface BoardTask { task: Task; feature: string }

// --- Replay (M3.3b) ---
//
// One row of the timeline index (serve GET /timeline). n is 1-based position in
// the log; task/feature are present only when the event carries them.
export interface TimelineEvent { n: number; ts: string; type: string; actor: string; task?: string; feature?: string }
export interface Timeline { total: number; events: TimelineEvent[] }

// --- Ops view (M3.3a) ---

// RegistryEntry is one registered project's identity + folded status. status.valid
// is false when pact validation failed (error carries the verbatim reason). seats
// is the roster size; lastEventTs is the most recent log event ts (omitted/empty
// for an empty log).
export interface RegistryEntry {
  name: string;
  path: string;
  group?: string;
  status: { valid: boolean; error?: string; seats: number; lastEventTs?: string };
}

// WiringStatus is the content-aware wiring state of one agent kind for the
// selected project. wired comes from a content probe (config key or baked entry).
// global flags machine-global config (desktop kinds); docOnly flags TOML kinds
// (codex) that Wire never writes — only a snippet is shown.
export interface WiringStatus {
  kind: string;
  wired: boolean;
  detail: string;
  path: string;
  global: boolean;
  docOnly: boolean;
}

// SeatInfo is one roster seat folded with join provenance. lastJoin (when present)
// is the most recent join's self-reported client/version/ts; clientChanged is true
// when the two most recent joins both named a client and the names differ.
export interface SeatInfo {
  id: string;
  roles: string[];
  lastJoin?: { client: string; version: string; ts: string; prevClient?: string };
  clientChanged: boolean;
}

// WireResult is the POST /wiring/{kind} response. snippet is present for docOnly
// kinds (the config block to copy into the agent's TOML by hand).
export interface WireResult {
  wrote: boolean;
  global: boolean;
  path: string;
  docOnly: boolean;
  snippet?: string;
}

export interface OrchestrateStatus {
  feature: string;
  task: string;
  seat: string;
  action: "run_owner" | "run_reviewer" | "merge" | "stuck" | "idle" | "done";
  phase: string;
  escalated: boolean;
  reason?: string;
  done: boolean;
  total: number;
  accepted: number;
  iter: number;
  updated_at: string;
  // Self-repair loop progress. Present (omitempty) only while phase === "fixing":
  // fix_round is the current repair attempt, fix_max the bound.
  fix_round?: number;
  fix_max?: number;
}

export interface OrchestrateStatusResponse {
  present: boolean;
  status?: OrchestrateStatus;
  // Tier / resolved effort for the status' current task, computed server-side
  // (tierui-backend). Both are omitted when the status names no task or the
  // task's spec has no tier line; effort === "" is the COMMON case (only kinds
  // declaring EffortArgs apply a budget) — render it as a neutral note, not as
  // a missing value. The status is a SINGLE snapshot: under --concurrency>1 it
  // jumps between concurrent tasks, so a badge built from these must only be
  // attached to the chip whose id === status.task.
  tier?: string;
  effort?: string;
}

// Aggregated per-feature statuses from a parallel orchestrate run (one entry per
// concurrent feature). present=false when no parallel run has happened.
export interface ParallelStatusResponse {
  present: boolean;
  features?: OrchestrateStatus[];
}

// Recipes (#11): named orchestrate templates that expand a one-line goal into a
// pact task graph.
export interface RecipeItem {
  name: string;
  description: string;
}

export interface ExpandedTaskItem {
  id: string;
  spec: string;
  deps?: string[];
}

// Project setup wizard (#1): proposed seat roster from the machine's registered
// agents + any gaps that would block the pact loop.
export interface SetupBinding {
  seat: string;
  kind: string;
  roles: string[];
  drivable: boolean;
}

export interface SetupSuggestResponse {
  bindings: SetupBinding[];
  warnings: string[];
}

export interface FsBrowseEntry {
  name: string;
  path: string;
  isGit: boolean;
  hasPact: boolean;
}

export interface FsBrowseResponse {
  path: string;
  parent: string;
  entries: FsBrowseEntry[];
}

// Multi-machine roster surfaced by hosted-mode sources (relay account machines).
export interface Machine {
  machineId: string;
  host?: string;
  agentKinds: string[];
  workdirs?: string[];
  online: boolean;
  lastSeenAt: number;
}

// Planner-generated task graph surfaced for human review before `plan apply`
// assigns it (#7 planner review).
export interface PlanTaskReview {
  id: string;
  owner: string;
  reviewer: string;
  spec: string;
  verify: string;
  deps?: string[];
  // Exec tiering (tierui-backend): tier is the NORMALIZED L0..L3 read from the
  // task's spec file (the engine's own source). tier_missing distinguishes
  // "spec has no tier line" (planner missed the mandatory tier → NO TIER warn
  // badge) from an explicit `tier: L1`; the engine collapses both to L1, the
  // UI must not. tier_conflict is a human-readable note set when the manifest
  // and the spec disagree (the spec value wins — it's what tier carries).
  tier?: string;
  tier_missing?: boolean;
  tier_conflict?: string;
  // Planner's advisory labels, passed through unchanged; rendered as one muted
  // text line (`correctness · frontend`), never as badges.
  dimension?: string;
  role?: string;
}

export interface PlanReviewResponse {
  present: boolean;
  feature?: string;
  branch?: string;
  tasks?: PlanTaskReview[];
  valid: boolean;
  error?: string;
}
