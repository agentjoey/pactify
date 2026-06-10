import type { ProjectMeta, State, PactEvent } from "./types";
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

export const fetchProjects = () => getJSON<ProjectMeta[]>("/api/projects");
export const fetchState = (id: string) => getJSON<State>(`/api/projects/${id}/state`);

export const getActingSeat = () => getJSON<{ seat: string }>("/api/acting-seat");

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
