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
} from "./types";
import type { LayoutJSON } from "./canvas";

async function getJSON<T>(url: string): Promise<T> {
  const r = await fetch(url);
  if (!r.ok) throw new Error(`${url}: ${r.status}`);
  return (await r.json()) as T;
}

// writeJSON sends a JSON body and, on non-2xx, surfaces the server's
// {"error":msg} message verbatim so the UI can show it as-is. Falls back to a
// status line when the body isn't the expected error envelope.
async function writeJSON(url: string, method: string, body: unknown): Promise<Response> {
  const r = await fetch(url, {
    method,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!r.ok) {
    let msg = `${url}: ${r.status}`;
    try {
      const j = (await r.json()) as { error?: string };
      if (j && typeof j.error === "string") msg = j.error;
    } catch { /* non-JSON body; keep status line */ }
    throw new Error(msg);
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

export const fetchProjects = () => getJSON<ProjectMeta[]>("/api/projects");
export const fetchState = (id: string) =>
  getJSON<State>(`/api/projects/${id}/state`).then(normalizeState);

export const getActingSeat = () => getJSON<{ seat: string }>("/api/acting-seat");

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

export const getLayout = (project: string) =>
  getJSON<LayoutJSON>(`/api/projects/${project}/squad/layout`);

export async function putLayout(project: string, layout: LayoutJSON): Promise<void> {
  await writeJSON(`/api/projects/${project}/squad/layout`, "PUT", layout);
}

// --- Ops view (M3.3a) ---
//
// Registry/wiring/seats reads use getJSON (status-line errors on non-2xx);
// mutations use writeJSON so the server's {"error":msg} surfaces verbatim.

export const getRegistry = () => getJSON<RegistryEntry[]>("/api/registry");

export async function postRegister(path: string, name?: string): Promise<{ name: string }> {
  const body: { path: string; name?: string } = { path };
  if (name) body.name = name;
  const r = await writeJSON("/api/registry", "POST", body);
  return (await r.json()) as { name: string };
}

export async function deleteRegistry(name: string): Promise<void> {
  await writeJSON(`/api/registry/${encodeURIComponent(name)}`, "DELETE", undefined);
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

export const getRecipes = () => getJSON<RecipeItem[]>(`/api/recipes`);

export const expandRecipe = (name: string, goal: string) =>
  writeJSON(`/api/recipes/${name}/expand`, "POST", { goal }).then(
    (r) => r.json() as Promise<ExpandedTaskItem[]>,
  );

export const getPlanReview = (project: string, feature: string) =>
  getJSON<PlanReviewResponse>(`/api/projects/${project}/plan/${feature}`);

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
