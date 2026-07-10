import { describe, it, expect, vi, afterEach } from "vitest";
import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { LocalServeSource, DataSourceProvider, useDataSource } from "./datasource";
import * as api from "./api";
import type { State, ProjectMeta } from "./types";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("LocalServeSource", () => {
  it("exposes local-serve capabilities", () => {
    const src = new LocalServeSource();
    expect(src.capabilities).toEqual({
      canWrite: true,
      canOrchestrate: true,
      multiMachine: false,
      cockpit: true,
    });
  });

  it("listProjects delegates to fetchProjects", async () => {
    const projects: ProjectMeta[] = [
      { id: "p", name: "p", path: "/x", project: "p", feature_count: 0, awaiting_count: 0 },
    ];
    vi.spyOn(api, "fetchProjects").mockResolvedValue(projects);
    const src = new LocalServeSource();
    expect(await src.listProjects()).toEqual(projects);
    expect(api.fetchProjects).toHaveBeenCalled();
  });

  it("getState delegates to fetchState and passes wt", async () => {
    const state: State = { project: "p", agents: [], features: [], awaiting_count: 0 };
    vi.spyOn(api, "fetchState").mockResolvedValue(state);
    const src = new LocalServeSource();
    expect(await src.getState("p")).toEqual(state);
    expect(await src.getState("p", "feat-x")).toEqual(state);
    expect(api.fetchState).toHaveBeenNthCalledWith(1, "p", undefined);
    expect(api.fetchState).toHaveBeenNthCalledWith(2, "p", "feat-x");
  });

  it("getStats delegates to getStats", async () => {
    const stats: api.ProjectStats = { tasks: [], agents: [] };
    vi.spyOn(api, "getStats").mockResolvedValue(stats);
    const src = new LocalServeSource();
    expect(await src.getStats("p")).toEqual(stats);
    expect(api.getStats).toHaveBeenCalledWith("p");
  });

  it("subscribe wires SSE and fetches state on pact events, then unsubscribes", async () => {
    let lastES: FakeES | null = null;
    const pactListeners: Array<(ev: MessageEvent) => void> = [];
    class FakeES {
      closed = false;
      url: string;
      addEventListener(name: string, fn: (ev: MessageEvent) => void) {
        if (name === "pact") pactListeners.push(fn);
      }
      close() {
        this.closed = true;
      }
      constructor(url: string) {
        this.url = url;
        lastES = this;
      }
    }
    vi.stubGlobal("EventSource", FakeES);

    const state: State = { project: "p", agents: [], features: [], awaiting_count: 0 };
    vi.spyOn(api, "fetchState").mockResolvedValue(state);

    const src = new LocalServeSource();
    const onState = vi.fn();
    const off = src.subscribe("p", onState);

    expect(lastES).not.toBeNull();
    expect(lastES!.url).toBe("/api/projects/p/events");

    pactListeners[0]({ data: JSON.stringify({ event_id: "1" }) } as MessageEvent);

    await new Promise((r) => setTimeout(r, 0));
    expect(api.fetchState).toHaveBeenCalledWith("p");
    expect(onState).toHaveBeenCalledWith(state);

    off();
    expect(lastES!.closed).toBe(true);
  });

  it("verb delegates to postVerb", async () => {
    vi.spyOn(api, "postVerb").mockResolvedValue(undefined);
    const src = new LocalServeSource();
    await src.verb!("p", "assign", { task: "t1" });
    expect(api.postVerb).toHaveBeenCalledWith("p", "assign", { task: "t1" });
  });

  it("postTask delegates to api.postTask", async () => {
    vi.spyOn(api, "postTask").mockResolvedValue(undefined);
    const src = new LocalServeSource();
    await src.postTask!("p", { id: "t1", spec_md: "# spec" });
    expect(api.postTask).toHaveBeenCalledWith("p", { id: "t1", spec_md: "# spec" });
  });

  it("runOrchestrate delegates to api.runOrchestrate", async () => {
    vi.spyOn(api, "runOrchestrate").mockResolvedValue({ status_url: "/x" });
    const src = new LocalServeSource();
    const body: api.RunOrchestrateBody = { feature: "f1" };
    expect(await src.runOrchestrate!("p", body)).toEqual({ status_url: "/x" });
    expect(api.runOrchestrate).toHaveBeenCalledWith("p", body);
  });

  it("resumeOrchestrate delegates to api.resumeOrchestrate", async () => {
    vi.spyOn(api, "resumeOrchestrate").mockResolvedValue({ status_url: "/y" });
    const src = new LocalServeSource();
    expect(await src.resumeOrchestrate!("p", {})).toEqual({ status_url: "/y" });
    expect(api.resumeOrchestrate).toHaveBeenCalledWith("p", {});
  });

  it("shipFeature delegates to api.shipFeature", async () => {
    vi.spyOn(api, "shipFeature").mockResolvedValue({ pushed: true, pr_url: "http://pr" });
    const src = new LocalServeSource();
    const body: api.ShipBody = { pr: true };
    expect(await src.shipFeature!("p", body)).toEqual({ pushed: true, pr_url: "http://pr" });
    expect(api.shipFeature).toHaveBeenCalledWith("p", body);
  });

  it("generatePlan delegates to api.generatePlan", async () => {
    vi.spyOn(api, "generatePlan").mockResolvedValue({ status_url: "/z", feature: "f1" });
    const src = new LocalServeSource();
    const body = { goal: "g", feature: "f1" };
    expect(await src.generatePlan!("p", body)).toEqual({ status_url: "/z", feature: "f1" });
    expect(api.generatePlan).toHaveBeenCalledWith("p", body);
  });

  it("applyPlan delegates to api.applyPlan", async () => {
    vi.spyOn(api, "applyPlan").mockResolvedValue({ assigned: 2 });
    const src = new LocalServeSource();
    expect(await src.applyPlan!("p", "f1")).toEqual({ assigned: 2 });
    expect(api.applyPlan).toHaveBeenCalledWith("p", "f1");
  });

  it("cockpitSubscribe opens an EventSource on cockpitStreamUrl and forwards events", () => {
    let lastES: FakeES | null = null;
    class FakeES {
      onmessage: ((ev: MessageEvent) => void) | null = null;
      closed = false;
      url: string;
      constructor(url: string) {
        this.url = url;
        lastES = this;
      }
      close() {
        this.closed = true;
      }
    }
    vi.stubGlobal("EventSource", FakeES);
    vi.spyOn(api, "cockpitStreamUrl").mockReturnValue("/api/projects/p/cockpit/stream?seat=claude");

    const src = new LocalServeSource();
    const events: { kind: string; text?: string }[] = [];
    const off = src.cockpitSubscribe("p", "claude", (e) => events.push(e as { kind: string; text?: string }));

    expect(lastES).not.toBeNull();
    expect(lastES!.url).toBe("/api/projects/p/cockpit/stream?seat=claude");

    lastES!.onmessage?.({ data: JSON.stringify({ kind: "message", text: "hi" }) } as MessageEvent);
    expect(events).toEqual([{ kind: "message", text: "hi" }]);

    off();
    expect(lastES!.closed).toBe(true);
  });
});

describe("DataSourceProvider / useDataSource", () => {
  it("provides a LocalServeSource by default", () => {
    const wrapper = ({ children }: { children: ReactNode }) => (
      <DataSourceProvider>{children}</DataSourceProvider>
    );
    const { result } = renderHook(() => useDataSource(), { wrapper });
    expect(result.current).toBeInstanceOf(LocalServeSource);
  });

  it("throws when used outside provider", () => {
    expect(() => renderHook(() => useDataSource())).toThrow(
      "useDataSource must be used within DataSourceProvider",
    );
  });
});
