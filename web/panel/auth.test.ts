import { afterEach, expect, it, vi } from "vitest";
import { initializeSession, panelFetch, streamLogs } from "./auth";

afterEach(() => { sessionStorage.clear(); vi.unstubAllGlobals(); document.querySelector('meta[name="pactify-session"]')?.remove(); history.replaceState(null, "", "/"); });

it("keeps the exchanged session across refresh and sends only a bearer header", async () => {
  const meta = document.createElement("meta");
  meta.name = "pactify-session"; meta.content = "session-secret";
  document.head.append(meta);
  history.replaceState(null, "", "/?token=launch-secret");
  initializeSession();
  expect(location.search).toBe("");
  expect(document.querySelector('meta[name="pactify-session"]')).toBeNull();
  initializeSession(); // Refreshed shell has no bootstrap metadata.
  const fetcher = vi.fn(async (_path: string, _options: RequestInit) => new Response("[]"));
  vi.stubGlobal("fetch", fetcher);
  await panelFetch("/api/jobs", new AbortController().signal);
  expect(fetcher.mock.calls[0][1]).toMatchObject({ headers: { Authorization: "Bearer session-secret" }, credentials: "omit" });
});

it("reads authenticated SSE through fetch across chunk boundaries and stops at done", async () => {
  sessionStorage.setItem("pactify-panel-session", "session-secret");
  const chunks = ['data: "hello 世', '界"\n\nevent: done\ndata: {}\n\n'];
  const bytes = new TextEncoder().encode(chunks.join(""));
  let canceled = false;
  const fetcher = vi.fn(async (_path: string, _options: RequestInit) => new Response(new ReadableStream({
    start(controller) { controller.enqueue(bytes.slice(0, 15)); controller.enqueue(bytes.slice(15, 17)); controller.enqueue(bytes.slice(17)); },
    cancel() { canceled = true; },
  })));
  vi.stubGlobal("fetch", fetcher);
  const lines: string[] = [], statuses: string[] = [];
  await streamLogs("/logs", new AbortController().signal, (line) => lines.push(line), (status) => statuses.push(status));
  expect(lines).toEqual(["hello 世界"]);
  expect(statuses.at(-1)).toBe("Finished");
  expect(canceled).toBe(true);
  expect(fetcher.mock.calls[0][1]).toMatchObject({ headers: { Authorization: "Bearer session-secret" }, credentials: "omit" });
});
