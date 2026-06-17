import { describe, it, expect, vi, afterEach } from "vitest";
import { fetchProjects, fetchState, subscribeEvents, browseFs, postRegister, setupApply } from "./api";

afterEach(() => vi.restoreAllMocks());

describe("api", () => {
  it("fetchProjects GETs /api/projects and merges group from /api/registry", async () => {
    const data = [{ id: "p", name: "p", path: "/x", project: "p", feature_count: 0, awaiting_count: 0 }];
    const registry = [{ name: "p", path: "/x", group: "g1", status: { valid: true, seats: 0 } }];
    vi.stubGlobal("fetch", vi.fn(async (url: string) => {
      if (url === "/api/projects") return { ok: true, json: async () => data };
      if (url === "/api/registry") return { ok: true, json: async () => registry };
      return { ok: true, json: async () => ({}) };
    }));
    expect(await fetchProjects()).toEqual([{ ...data[0], group: "g1" }]);
    expect(fetch).toHaveBeenCalledWith("/api/projects");
    expect(fetch).toHaveBeenCalledWith("/api/registry");
  });

  it("fetchProjects survives a missing /api/registry", async () => {
    const data = [{ id: "p", name: "p", path: "/x", project: "p", feature_count: 0, awaiting_count: 0 }];
    vi.stubGlobal("fetch", vi.fn(async (url: string) => {
      if (url === "/api/projects") return { ok: true, json: async () => data };
      return { ok: false, status: 500, json: async () => ({ error: "nope" }) };
    }));
    expect(await fetchProjects()).toEqual(data);
  });

  it("browseFs GETs /api/fs/browse with encoded path", async () => {
    const resp = { path: "/home", parent: "/", entries: [{ name: "a", path: "/home/a", isGit: false, hasPact: false }] };
    vi.stubGlobal("fetch", vi.fn(async (url: string) => {
      if (url === "/api/fs/browse?path=%2Fhome") return { ok: true, json: async () => resp };
      return { ok: false, status: 404, json: async () => ({}) };
    }));
    expect(await browseFs("/home")).toEqual(resp);
  });

  it("postRegister sends name and group", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => ({ ok: true, json: async () => ({ name: "p" }) })));
    await postRegister("/x", { name: "n", group: "g" });
    const body = JSON.parse((fetch as ReturnType<typeof vi.fn>).mock.calls[0][1].body);
    expect(body).toEqual({ path: "/x", name: "n", group: "g" });
  });

  it("postRegister keeps backward-compatible string name", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => ({ ok: true, json: async () => ({ name: "p" }) })));
    await postRegister("/x", "n");
    const body = JSON.parse((fetch as ReturnType<typeof vi.fn>).mock.calls[0][1].body);
    expect(body).toEqual({ path: "/x", name: "n" });
  });

  it("setupApply sends group when provided", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => ({ ok: true, json: async () => ({ inited: true, wired: [], notes: [] }) })));
    await setupApply({ path: "/x", project: "p", seats: [], group: "g" });
    const body = JSON.parse((fetch as ReturnType<typeof vi.fn>).mock.calls[0][1].body);
    expect(body.group).toBe("g");
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
