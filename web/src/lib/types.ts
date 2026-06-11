export interface Seat { id: string; roles: string[] }
export interface Task { id: string; owner: string; status: string; reviewer: string; spec: string; evidence: string; deps?: string[] }
export interface Feature { id: string; branch: string; status: string; tasks: Task[] }
export interface State { project: string; agents: Seat[]; features: Feature[]; awaiting_count: number }
export interface PactEvent { event_id: string; ts: string; agent_id: string; role: string; event_type: string; task_id: string; feature: string; payload: Record<string, unknown> }
export interface ProjectMeta { id: string; name: string; path: string; project: string; feature_count: number; awaiting_count: number }
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
