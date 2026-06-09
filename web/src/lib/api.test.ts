import { describe, it, expect, vi, afterEach } from "vitest";
import { fetchProjects, fetchState } from "./api";

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
});
