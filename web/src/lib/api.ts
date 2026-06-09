import type { ProjectMeta, State, PactEvent } from "./types";

async function getJSON<T>(url: string): Promise<T> {
  const r = await fetch(url);
  if (!r.ok) throw new Error(`${url}: ${r.status}`);
  return (await r.json()) as T;
}

export const fetchProjects = () => getJSON<ProjectMeta[]>("/api/projects");
export const fetchState = (id: string) => getJSON<State>(`/api/projects/${id}/state`);

// subscribeEvents opens an SSE stream; returns an unsubscribe fn.
export function subscribeEvents(id: string, onEvent: (e: PactEvent) => void): () => void {
  const es = new EventSource(`/api/projects/${id}/events`);
  es.addEventListener("pact", (ev) => {
    try { onEvent(JSON.parse((ev as MessageEvent).data) as PactEvent); } catch { /* ignore malformed */ }
  });
  return () => es.close();
}
