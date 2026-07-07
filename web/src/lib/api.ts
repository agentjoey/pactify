import type {
  ProjectMeta,
  State,
  PactEvent,
  RegistryEntry,
  WiringStatus,
  SeatInfo,
  WireResult,
  Timeline,
  OrchestrateStatusResponse,
  ParallelStatusResponse,
  PlanReviewResponse,
  SetupSuggestResponse,
  RecipeItem,
  ExpandedTaskItem,
  FsBrowseResponse,
} from "./types";

async function getJSON<T>(url: string): Promise<T> {
  const r = await fetch(url);
  if (!r.ok) throw new Error(`${url}: ${r.status}`);
  return (await r.json()) as T;
}

// extractErrorMessage surfaces the server's {"error":msg} message verbatim
// so the UI can show it as-is. Falls back to a status line when the body isn't
// the expected error envelope.
async function extractErrorMessage(r: Response): Promise<string> {
  try {
    const j = (await r.json()) as { error?: string };
    if (j && typeof j.error === "string") return j.error;
  } catch { /* non-JSON body; keep status line */ }
  return `${r.status}`;
}

// writeJSON sends a JSON body and, on non-2xx, throws with the server's
// readable error message.
async function writeJSON(url: string, method: string, body: unknown): Promise<Response> {
  const r = await fetch(url, {
    method,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!r.ok) {
    throw new Error(`${url}: ${await extractErrorMessage(r)}`);
  }
  return r;
}

// normalizeState repairs the State the backend may emit with `null` instead of
// `[]` for empty lists. Go marshals a nil slice as JSON `null`, so a freshly
// registered repo (agents present, zero features) arrives as { features: null }.
// The State type promises non-null arrays and every consumer relies on .map /
// .forEach / .find, so an un-coerced null crashes the whole canvas (no seats
// render, "+ New task" stays disabled, dispatch is unreachable). Coercing once
// at the fetch boundary keeps that contract true for all downstream code.
type WireFeature = Omit<State["features"][number], "tasks"> & {
  tasks: State["features"][number]["tasks"] | null;
};
type WireState = Omit<State, "agents" | "features"> & {
  agents: State["agents"] | null;
  features: WireFeature[] | null;
};
function normalizeState(s: WireState): State {
  return {
    ...s,
    agents: s.agents ?? [],
    features: (s.features ?? []).map((f) => ({ ...f, tasks: f.tasks ?? [] })),
  };
}

export async function fetchProjects(): Promise<ProjectMeta[]> {
  const [projects, registry] = await Promise.all([
    getJSON<ProjectMeta[]>("/api/projects"),
    getJSON<RegistryEntry[]>("/api/registry").catch(() => [] as RegistryEntry[]),
  ]);
  const groupFor = new Map<string, string>();
  for (const r of registry) {
    if (r.group) {
      groupFor.set(r.path, r.group);
      groupFor.set(r.name, r.group);
    }
  }
  return projects.map((p) => ({
    ...p,
    group: p.group ?? groupFor.get(p.path) ?? groupFor.get(p.name),
  }));
}
export const fetchState = (id: string, wt?: string) =>
  getJSON<State>(`/api/projects/${id}/state${wt ? `?wt=${encodeURIComponent(wt)}` : ""}`).then(normalizeState);

export const getActingSeat = () => getJSON<{ seat: string }>("/api/acting-seat");

// --- Work stats (D1) ---
// Per-task + per-agent duration (LOC / tokens best-effort, 0 until later slices).
export type TaskStat = {
  task_id: string;
  feature: string;
  owner: string;
  reviewer: string;
  status: string;
  duration_sec: number;
  added: number;
  deleted: number;
  tokens: number;
};
export type AgentStat = {
  seat: string;
  tasks: number;
  duration_sec: number;
  added: number;
  deleted: number;
  tokens: number;
  accepted: number;
  reworked: number;
};
export type ProjectStats = { tasks: TaskStat[]; agents: AgentStat[] };
export const getStats = (id: string) => getJSON<ProjectStats>(`/api/projects/${id}/stats`);

// fmtDuration renders seconds as a compact "2h 5m" / "14m" / "45s".
export function fmtDuration(sec: number): string {
  if (sec <= 0) return "—";
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  if (h > 0) return m > 0 ? `${h}h ${m}m` : `${h}h`;
  if (m > 0) return `${m}m`;
  return `${sec}s`;
}

// --- Replay (M3.3b) ---
//
// getTimeline reads the lightweight log index once on entering replay.
// getStateAt folds the first `at` events into a state snapshot (at=0 → empty,
// at≥total → clamps to full). Both are read-only; status-line errors on non-2xx.
export const getTimeline = (id: string) =>
  getJSON<Timeline>(`/api/projects/${id}/timeline`);

export const getStateAt = (id: string, at: number) =>
  getJSON<State>(`/api/projects/${id}/state?at=${at}`).then(normalizeState);

export async function postTask(
  project: string,
  body: { id: string; spec_md: string },
): Promise<void> {
  await writeJSON(`/api/projects/${project}/tasks`, "POST", body);
}

export async function postVerb(
  project: string,
  verb: "assign" | "accept" | "changes" | "merge",
  body: Record<string, unknown>,
): Promise<void> {
  await writeJSON(`/api/projects/${project}/verbs/${verb}`, "POST", body);
}

// --- Ops view (M3.3a) ---
//
// Registry/wiring/seats reads use getJSON (status-line errors on non-2xx);
// mutations use writeJSON so the server's {"error":msg} surfaces verbatim.

export interface Worktree { branch: string; path: string; primary: boolean }
export const getWorktrees = (project: string) =>
  getJSON<Worktree[]>(`/api/projects/${project}/worktrees`);

export const getRegistry = () => getJSON<RegistryEntry[]>("/api/registry");

export async function postRegister(
  path: string,
  opts?: string | { name?: string; group?: string },
): Promise<{ name: string }> {
  const body: { path: string; name?: string; group?: string } = { path };
  if (typeof opts === "string") {
    body.name = opts;
  } else if (opts) {
    if (opts.name) body.name = opts.name;
    if (opts.group) body.group = opts.group;
  }
  const r = await writeJSON("/api/registry", "POST", body);
  return (await r.json()) as { name: string };
}

export async function deleteRegistry(name: string): Promise<void> {
  await writeJSON(`/api/registry/${encodeURIComponent(name)}`, "DELETE", undefined);
}

export async function renameRegistry(name: string, newName: string): Promise<{ name: string }> {
  const r = await writeJSON(
    `/api/registry/${encodeURIComponent(name)}`,
    "PUT",
    { new_name: newName },
  );
  return (await r.json()) as { name: string };
}

export const getWiring = (project: string) =>
  getJSON<WiringStatus[]>(`/api/projects/${project}/wiring`);

export async function postWire(
  project: string,
  kind: string,
  body: { seat: string; roles: string },
): Promise<WireResult> {
  const r = await writeJSON(`/api/projects/${project}/wiring/${kind}`, "POST", body);
  return (await r.json()) as WireResult;
}

export const getSeats = (project: string) =>
  getJSON<SeatInfo[]>(`/api/projects/${project}/seats`);

export interface AgentRow {
  kind: string;
  installed: boolean;
  detail: string;
  registered: boolean;
  label?: string;
}

export const getAgents = () => getJSON<AgentRow[]>("/api/agents");

// Permission audit log — one captured tool call per record (read-only lens).
export interface AuditRecord {
  ts: string;
  project?: string;
  repo?: string;
  seat: string;
  task: string;
  kind?: string;
  session?: string;
  tool: string;
  summary: string;
  risk: string;
  decision: string;
}
export const getAudit = (project: string, params: Record<string, string> = {}) => {
  const qs = new URLSearchParams(params).toString();
  return getJSON<AuditRecord[]>(`/api/projects/${project}/audit${qs ? `?${qs}` : ""}`);
};

// Per-agent launch config (#10 model / #9 posture / #4 scoped tools).
export interface AgentConfig {
  kind: string;
  registered: boolean;
  drivable: boolean;
  model: string;
  allowed_tools: string[] | null;
  restricted: boolean;
  effective_model: string;
  effective_scoped: boolean;
  // Curated model IDs for this kind, driving the model dropdown. Null/empty →
  // the UI falls back to a free-text model field. Always offers a custom escape.
  candidate_models: string[] | null;
}

export const getAgentConfig = (kind: string) =>
  getJSON<AgentConfig>(`/api/agents/${kind}/config`);

export const setAgentConfig = (
  kind: string,
  body: { model: string; allowed_tools: string[]; restricted: boolean },
) =>
  writeJSON(`/api/agents/${kind}/config`, "POST", body).then(
    (r) => r.json() as Promise<AgentConfig>,
  );

export async function registerAgent(kind: string, label?: string): Promise<void> {
  const body: { label?: string } = {};
  if (label) body.label = label;
  await writeJSON(`/api/agents/${encodeURIComponent(kind)}/register`, "POST", body);
}

export async function unregisterAgent(kind: string): Promise<void> {
  await writeJSON(`/api/agents/${encodeURIComponent(kind)}/register`, "DELETE", undefined);
}

export const getOrchestrateStatus = (project: string) =>
  getJSON<OrchestrateStatusResponse>(`/api/projects/${project}/orchestrate/status`);

export const getParallelOrchestrate = (project: string) =>
  getJSON<ParallelStatusResponse>(`/api/projects/${project}/orchestrate/parallel`);

export const getSetupSuggest = () => getJSON<SetupSuggestResponse>(`/api/setup/suggest`);

export interface SetupApplySeat {
  id: string;
  roles: string[];
  entry: string;
  kind: string;
}

export interface SetupApplyResponse {
  inited: boolean;
  wired: {
    kind: string;
    seat: string;
    wrote: boolean;
    path: string;
    docOnly: boolean;
    snippet?: string;
  }[];
  notes: string[];
}

export async function setupApply(body: {
  path: string;
  project: string;
  seats: SetupApplySeat[];
  group?: string;
}): Promise<SetupApplyResponse> {
  const r = await writeJSON("/api/setup/apply", "POST", body);
  return (await r.json()) as SetupApplyResponse;
}

export const getRecipes = () => getJSON<RecipeItem[]>(`/api/recipes`);

export const expandRecipe = (name: string, goal: string) =>
  writeJSON(`/api/recipes/${name}/expand`, "POST", { goal }).then(
    (r) => r.json() as Promise<ExpandedTaskItem[]>,
  );

export const getPlanReview = (project: string, feature: string) =>
  getJSON<PlanReviewResponse>(`/api/projects/${project}/plan/${feature}`);

export async function applyPlan(
  project: string,
  feature: string,
): Promise<{ assigned: number }> {
  const r = await writeJSON(
    `/api/projects/${project}/plan/${encodeURIComponent(feature)}/apply`,
    "POST",
    {},
  );
  return (await r.json()) as { assigned: number };
}

export interface PlanGenStatus {
  state: "idle" | "running" | "done" | "error";
  feature?: string;
  error?: string;
}

export async function generatePlan(
  project: string,
  body: { goal: string; feature: string; planner_kind?: string },
): Promise<{ status_url: string; feature: string }> {
  const r = await writeJSON(`/api/projects/${project}/plan/generate`, "POST", body);
  return (await r.json()) as { status_url: string; feature: string };
}

export const getPlanGenStatus = (project: string) =>
  getJSON<PlanGenStatus>(`/api/projects/${project}/plan/generate/status`);

// --- Orchestrate drive (SP2) ---

export type RunOrchestrateBody = {
  feature?: string;
  max_concurrency?: number;
  seat_kinds?: Record<string, string>;
};

export async function runOrchestrate(
  project: string,
  body: RunOrchestrateBody = {},
): Promise<{ status_url: string }> {
  const r = await writeJSON(`/api/projects/${project}/orchestrate/run`, "POST", body);
  return (await r.json()) as { status_url: string };
}

export async function resumeOrchestrate(
  project: string,
  body: RunOrchestrateBody = {},
): Promise<{ status_url: string }> {
  const r = await writeJSON(`/api/projects/${project}/orchestrate/resume`, "POST", body);
  return (await r.json()) as { status_url: string };
}

export type ShipBody = {
  remote?: string;
  branch?: string;
  pr?: boolean;
  head?: string;
  title?: string;
  body?: string;
};

export async function shipFeature(
  project: string,
  body: ShipBody,
): Promise<{ pushed: boolean; pr_url?: string }> {
  const r = await writeJSON(`/api/projects/${project}/orchestrate/ship`, "POST", body);
  return (await r.json()) as { pushed: boolean; pr_url?: string };
}

export async function getDiff(project: string): Promise<{ diff: string }> {
  return getJSON<{ diff: string }>(`/api/projects/${project}/orchestrate/diff`);
}

export async function pruneSessions(
  kind: string,
): Promise<{ kind: string; skipped: boolean; output: string }> {
  const r = await writeJSON(
    `/api/agents/${encodeURIComponent(kind)}/sessions/prune`,
    "POST",
    {},
  );
  return (await r.json()) as { kind: string; skipped: boolean; output: string };
}

// --- Custom-agent manifests (Phase D) ---
export interface ManifestRow { kind: string; binary: string; drivable: boolean }
export const listManifests = () => getJSON<ManifestRow[]>("/api/manifests");
export const createManifest = async (toml: string): Promise<{ kind: string }> => {
  const r = await fetch("/api/manifests", {
    method: "POST",
    headers: { "Content-Type": "text/plain" },
    body: toml,
  });
  if (!r.ok) {
    throw new Error(`/api/manifests: ${await extractErrorMessage(r)}`);
  }
  return r.json() as Promise<{ kind: string }>;
};
export const deleteManifest = async (kind: string): Promise<void> => {
  const url = `/api/manifests/${encodeURIComponent(kind)}`;
  const r = await fetch(url, { method: "DELETE" });
  if (!r.ok) {
    throw new Error(`${url}: ${await extractErrorMessage(r)}`);
  }
};

export const browseFs = (path?: string) => {
  const qs = path ? `?path=${encodeURIComponent(path)}` : "";
  return getJSON<FsBrowseResponse>(`/api/fs/browse${qs}`);
};

// subscribeAgentStream opens an SSE stream of a task's raw agent output lines.
// Returns an unsubscribe fn. Each `stream` event's data is one raw output line;
// lastEventId is the server's 1-based line ordinal (empty if the server sent no
// id) so callers can drop lines re-delivered by EventSource auto-reconnect.
export function subscribeAgentStream(
  project: string,
  task: string,
  onLine: (line: string, lastEventId?: string) => void,
): () => void {
  const es = new EventSource(`/api/projects/${project}/orchestrate/stream/${task}`);
  es.addEventListener("stream", (e) => {
    const me = e as MessageEvent;
    onLine(me.data, me.lastEventId);
  });
  return () => es.close();
}

// fetchEventsLog reads the last `n` events straight from a project's log via
// REST — byte-identical objects to the SSE `pact` frames, so PactEvent applies.
// Non-primary worktree views have no SSE stream to accumulate from; they poll
// this alongside state so task metrics stay live. No `wt` → primary; server
// defaults n=500 (cap 2000).
export const fetchEventsLog = (id: string, wt?: string, n?: number) => {
  const qs = new URLSearchParams();
  if (wt) qs.set("wt", wt);
  if (n !== undefined) qs.set("n", String(n));
  const s = qs.toString();
  return getJSON<PactEvent[]>(`/api/projects/${id}/events/log${s ? `?${s}` : ""}`);
};

// subscribeEvents opens an SSE stream; returns an unsubscribe fn.
// onLive (optional) reports connection state: true on open, false on error/drop.
export function subscribeEvents(
  id: string,
  onEvent: (e: PactEvent) => void,
  onLive?: (live: boolean) => void,
): () => void {
  const es = new EventSource(`/api/projects/${id}/events`);
  es.onopen = () => onLive?.(true);
  es.onerror = () => onLive?.(false);
  es.addEventListener("pact", (ev) => {
    try { onEvent(JSON.parse((ev as MessageEvent).data) as PactEvent); } catch { /* ignore malformed */ }
  });
  return () => es.close();
}
