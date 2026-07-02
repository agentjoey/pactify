import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { fetchProjects, fetchState, subscribeEvents, subscribeAgentStream, browseFs, postRegister, setupApply, renameRegistry } from "./api";

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

  it("subscribeAgentStream forwards each line with its SSE lastEventId", () => {
    let lastES: { closed: boolean } | null = null;
    const streamListeners: Array<(ev: MessageEvent) => void> = [];

    class FakeES {
      closed = false;
      url: string;
      addEventListener(name: string, fn: (ev: MessageEvent) => void) {
        if (name === "stream") streamListeners.push(fn);
      }
      close() { this.closed = true; }
      constructor(url: string) {
        this.url = url;
        lastES = this;
      }
    }
    vi.stubGlobal("EventSource", FakeES);

    const got: Array<[string, string | undefined]> = [];
    const off = subscribeAgentStream("p", "t1", (line, id) => got.push([line, id]));

    streamListeners[0]({ data: "hello", lastEventId: "7" } as MessageEvent);
    streamListeners[0]({ data: "no-id", lastEventId: "" } as MessageEvent);
    expect(got).toEqual([["hello", "7"], ["no-id", ""]]);

    off();
    expect(lastES!.closed).toBe(true);
  });
});

describe("renameRegistry", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(JSON.stringify({ name: "fresh" }), { status: 200 })),
    );
  });
  afterEach(() => vi.unstubAllGlobals());

  it("PUTs new_name to /api/registry/{name}", async () => {
    await renameRegistry("old", "fresh");
    const f = fetch as unknown as ReturnType<typeof vi.fn>;
    const [url, init] = f.mock.calls[0];
    expect(url).toBe("/api/registry/old");
    expect(init.method).toBe("PUT");
    expect(JSON.parse(init.body)).toEqual({ new_name: "fresh" });
  });
});

import { generatePlan, getPlanGenStatus } from "./api";

describe("generatePlan / getPlanGenStatus", () => {
  it("POSTs goal+feature to plan/generate", async () => {
    vi.stubGlobal("fetch", vi.fn(async () =>
      new Response(JSON.stringify({ status_url: "/x", feature: "add-2fa" }), { status: 202 })));
    await generatePlan("p1", { goal: "add 2fa", feature: "add-2fa" });
    const f = fetch as unknown as ReturnType<typeof vi.fn>;
    const [url, init] = f.mock.calls[0];
    expect(url).toBe("/api/projects/p1/plan/generate");
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body)).toEqual({ goal: "add 2fa", feature: "add-2fa" });
    vi.unstubAllGlobals();
  });

  it("GETs the generation status", async () => {
    vi.stubGlobal("fetch", vi.fn(async () =>
      new Response(JSON.stringify({ state: "running", feature: "add-2fa" }), { status: 200 })));
    const st = await getPlanGenStatus("p1");
    expect(st.state).toBe("running");
    vi.unstubAllGlobals();
  });
});

import { getWorktrees } from "./api";
describe("worktrees api", () => {
  it("getWorktrees GETs the worktrees endpoint", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify([{ branch: "main", path: "/r", primary: true }]), { status: 200 })));
    const wts = await getWorktrees("p1");
    expect(wts[0].branch).toBe("main");
    vi.unstubAllGlobals();
  });
  it("fetchState passes ?wt= when given", async () => {
    const f = vi.fn(async () => new Response(JSON.stringify({ project: "p", agents: [], features: [], awaiting_count: 0 }), { status: 200 }));
    vi.stubGlobal("fetch", f);
    await fetchState("p1", "feat-x");
    const calls = f.mock.calls as unknown as [string, ...unknown[]][];
    expect(calls[0][0]).toContain("?wt=feat-x");
    vi.unstubAllGlobals();
  });
});

import { fetchEventsLog } from "./api";
describe("fetchEventsLog", () => {
  it("GETs the primary events log with no params", async () => {
    const f = vi.fn(async () => new Response(JSON.stringify([
      { event_id: "e1", ts: "t", agent_id: "a", role: "worker", event_type: "assign", task_id: "t1", feature: "f", payload: {} },
    ]), { status: 200 }));
    vi.stubGlobal("fetch", f);
    const evs = await fetchEventsLog("p1");
    expect((f.mock.calls as unknown as [string][])[0][0]).toBe("/api/projects/p1/events/log");
    expect(evs).toHaveLength(1);
    expect(evs[0].event_id).toBe("e1");
    vi.unstubAllGlobals();
  });

  it("passes wt and n as query params", async () => {
    const f = vi.fn(async () => new Response(JSON.stringify([]), { status: 200 }));
    vi.stubGlobal("fetch", f);
    await fetchEventsLog("p1", "feat-x", 100);
    expect((f.mock.calls as unknown as [string][])[0][0]).toBe("/api/projects/p1/events/log?wt=feat-x&n=100");
    vi.unstubAllGlobals();
  });

  it("throws on a non-2xx response", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("bad wt", { status: 400 })));
    await expect(fetchEventsLog("p1", "nope")).rejects.toThrow("400");
    vi.unstubAllGlobals();
  });
});
