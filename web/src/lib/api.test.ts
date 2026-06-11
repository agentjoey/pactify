import { describe, it, expect, vi, afterEach } from "vitest";
import { fetchProjects, fetchState, subscribeEvents } from "./api";

afterEach(() => vi.restoreAllMocks());

describe("api", () => {
  it("fetchProjects GETs /api/projects", async () => {
    const data = [{ id: "p", name: "p", path: "/x", project: "p", feature_count: 0, awaiting_count: 0 }];
    vi.stubGlobal("fetch", vi.fn(async () => ({ ok: true, json: async () => data })));
    expect(await fetchProjects()).toEqual(data);
    expect(fetch).toHaveBeenCalledWith("/api/projects");
  });
  it("fetchState GETs the project state", async () => {
    const st = { project: "p", agents: [], features: [], awaiting_count: 0 };
    vi.stubGlobal("fetch", vi.fn(async () => ({ ok: true, json: async () => st })));
    expect(await fetchState("p")).toEqual(st);
    expect(fetch).toHaveBeenCalledWith("/api/projects/p/state");
  });

  it("fetchState coerces a null features/agents list to [] (Go nil-slice → JSON null)", async () => {
    // The Go backend marshals an empty Features (or Agents) slice as JSON `null`,
    // not `[]`. A freshly-registered repo with agents but no features yet (the
    // dogfood acceptance case) thus arrives as { features: null }. Every consumer
    // does state.features.map/forEach/find, so an un-coerced null crashes the
    // whole canvas (no seats, "+ New task" disabled, dispatch unreachable). The
    // fetch boundary normalizes so the State type contract holds downstream.
    const raw = { project: "greet", agents: [{ id: "claude-opus", roles: ["orchestrator"] }], features: null, awaiting_count: 0 };
    vi.stubGlobal("fetch", vi.fn(async () => ({ ok: true, json: async () => raw })));
    const s = await fetchState("p");
    expect(s.features).toEqual([]);
    expect(Array.isArray(s.features)).toBe(true);
    expect(s.agents).toHaveLength(1);
  });

  it("subscribeEvents parses pact events, reports live state, ignores malformed", () => {
    let lastES: FakeES | null = null;
    const pactListeners: Array<(ev: MessageEvent) => void> = [];

    class FakeES {
      onopen: (() => void) | null = null;
      onerror: (() => void) | null = null;
      closed = false;
      addEventListener(name: string, fn: (ev: MessageEvent) => void) {
        if (name === "pact") pactListeners.push(fn);
      }
      close() { this.closed = true; }
      constructor() { lastES = this; }
    }
    vi.stubGlobal("EventSource", FakeES);

    const events: unknown[] = [];
    const liveStates: boolean[] = [];
    const off = subscribeEvents("p", (e) => events.push(e), (v) => liveStates.push(v));

    // Trigger open → onLive(true)
    lastES!.onopen?.();
    expect(liveStates).toEqual([true]);

    // Trigger error → onLive(false)
    lastES!.onerror?.();
    expect(liveStates).toEqual([true, false]);

    // Valid pact event forwarded
    pactListeners[0]({ data: JSON.stringify({
      event_id: "1", ts: "t", agent_id: "a", role: "worker",
      event_type: "join", task_id: "", feature: "", payload: {},
    }) } as MessageEvent);
    expect(events).toHaveLength(1);
    expect((events[0] as { event_type: string }).event_type).toBe("join");

    // Malformed JSON swallowed (no throw, not forwarded)
    expect(() => pactListeners[0]({ data: "not json{" } as MessageEvent)).not.toThrow();
    expect(events).toHaveLength(1);

    // Unsubscribe closes the EventSource
    off();
    expect(lastES!.closed).toBe(true);
  });
});
