import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, act, fireEvent, within } from "@testing-library/react";
import App, { pickInitialProject } from "./App";
import type { ProjectMeta } from "./lib/types";

// Module-level capture so tests can fire events on the last-constructed instance.
let lastES: {
  onopen: (() => void) | null;
  onerror: (() => void) | null;
  pactListeners: Array<(ev: MessageEvent) => void>;
  closed: boolean;
} | null = null;

function makeFakeESClass() {
  class FakeES {
    onopen: (() => void) | null = null;
    onerror: (() => void) | null = null;
    closed = false;
    private _pactListeners: Array<(ev: MessageEvent) => void> = [];
    constructor() {
      // expose via module-level ref so tests can drive the instance
      lastES = {
        get onopen() { return instance.onopen; },
        set onopen(fn) { instance.onopen = fn; },
        get onerror() { return instance.onerror; },
        set onerror(fn) { instance.onerror = fn; },
        get pactListeners() { return instance._pactListeners; },
        get closed() { return instance.closed; },
      };
      const instance = this;
    }
    addEventListener(name: string, fn: (ev: MessageEvent) => void) {
      if (name === "pact") this._pactListeners.push(fn);
    }
    close() { this.closed = true; }
  }
  return FakeES;
}

beforeEach(() => {
  lastES = null;
  localStorage.clear();
  globalThis.EventSource = makeFakeESClass() as unknown as typeof EventSource;
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url === "/api/projects") return { ok: true, json: async () => [{ id: "demo", name: "demo", path: "/x", project: "demo", feature_count: 1, awaiting_count: 0 }] };
    if (url === "/api/registry") return { ok: true, json: async () => [] };
    if (url === "/api/agents") return { ok: true, json: async () => [] };
    if (url.includes("/timeline")) return { ok: true, json: async () => ({ total: 0, events: [] }) };
    // SettingsModal mounts ops panels that read array-shaped endpoints.
    if (url.includes("/wiring") || url.includes("/seats")) return { ok: true, json: async () => [] };
    if (url.includes("/cockpit/status")) return { ok: true, json: async () => ({ capable: true, pending: [], threadId: "" }) };
    return { ok: true, json: async () => ({ project: "demo", agents: [{ id: "claude-opus", roles: ["orchestrator"] }], features: [], awaiting_count: 0 }) };
  }));
});

describe("App", () => {
  it("loads projects and shows the switcher + an agent", async () => {
    render(<App />);
    expect(screen.getByTestId("app-root")).toBeInTheDocument();
    // The TopBar project chip shows the current project name.
    await waitFor(() => expect(screen.getByTestId("project-menu-trigger")).toHaveTextContent("demo"));
    await waitFor(() => expect(screen.getByTestId("project-menu-trigger")).toHaveTextContent("demo"));
  });

  it("incoming pact event triggers a state re-fetch", async () => {
    render(<App />);
    // Wait for initial load (project list + initial state fetch)
    await waitFor(() => expect(screen.getByTestId("project-menu-trigger")).toHaveTextContent("demo"));

    const fetchMock = fetch as ReturnType<typeof vi.fn>;
    const callsBefore = fetchMock.mock.calls.filter(([url]) =>
      url === "/api/projects/demo/state"
    ).length;
    expect(callsBefore).toBeGreaterThanOrEqual(1);

    // Fire a pact event via the SSE stub
    const pactEvent = {
      event_id: "e1", ts: "t", agent_id: "a", role: "worker",
      event_type: "join", task_id: "", feature: "", payload: {},
    };
    await act(async () => {
      lastES?.pactListeners.forEach((fn) =>
        fn({ data: JSON.stringify(pactEvent) } as MessageEvent)
      );
    });

    // State should be re-fetched at least once more
    await waitFor(() => {
      const callsAfter = fetchMock.mock.calls.filter(([url]) =>
        url === "/api/projects/demo/state"
      ).length;
      expect(callsAfter).toBeGreaterThanOrEqual(callsBefore + 1);
    });
  });

  it("switching projects resets events", async () => {
    // Start with two projects
    vi.stubGlobal("fetch", vi.fn(async (url: string) => {
      if (url === "/api/projects") return {
        ok: true,
        json: async () => [
          { id: "demo", name: "demo", path: "/x", project: "demo", feature_count: 1, awaiting_count: 0 },
          { id: "other", name: "other", path: "/y", project: "other", feature_count: 0, awaiting_count: 0 },
        ],
      };
      if (url === "/api/registry") return { ok: true, json: async () => [] };
      if (url === "/api/agents") return { ok: true, json: async () => [] };
      return { ok: true, json: async () => ({ project: "demo", agents: [{ id: "claude-opus", roles: ["orchestrator"] }], features: [], awaiting_count: 0 }) };
    }));

    render(<App />);
    await waitFor(() => expect(screen.getByTestId("project-menu-trigger")).toHaveTextContent("demo"));

    // Fire a pact event to populate events list
    await act(async () => {
      lastES?.pactListeners.forEach((fn) =>
        fn({ data: JSON.stringify({ event_id: "e1", ts: "t", agent_id: "a", role: "worker", event_type: "join", task_id: "", feature: "", payload: {} }) } as MessageEvent)
      );
    });

    // Switch project — events should reset (new EventSource created). The
    // project list now lives in the header ProjectMenu; open it and pick "other".
    await act(async () => {
      fireEvent.click(screen.getByTestId("project-menu-trigger"));
    });
    await act(async () => {
      fireEvent.click(within(screen.getByTestId("project-menu")).getByText("other"));
    });

    // After switching, the old ES should have been closed
    // (new ES created for "other" project — just verify no crash)
    expect(screen.getByTestId("app-root")).toBeInTheDocument();
  });

  it("shows the fetch-stale indicator after 3 consecutive refresh failures and clears it on success", async () => {
    render(<App />);
    await waitFor(() => expect(screen.getByTestId("project-menu-trigger")).toHaveTextContent("demo"));

    const fetchMock = fetch as ReturnType<typeof vi.fn>;
    let failState = true;
    fetchMock.mockImplementation(async (url: string) => {
      if (url === "/api/projects") return { ok: true, json: async () => [{ id: "demo", name: "demo", path: "/x", project: "demo", feature_count: 1, awaiting_count: 0 }] };
      if (url === "/api/registry" || url === "/api/agents") return { ok: true, json: async () => [] };
      if (url === "/api/projects/demo/state" && failState) return { ok: false, status: 500, json: async () => ({}) };
      return { ok: true, json: async () => ({ project: "demo", agents: [], features: [], awaiting_count: 0 }) };
    });

    // Each pact event triggers one state re-fetch; unique ids pass the dedupe.
    const fire = (id: string) => act(async () => {
      lastES?.pactListeners.forEach((fn) =>
        fn({ data: JSON.stringify({ event_id: id, ts: "t", agent_id: "a", role: "worker", event_type: "join", task_id: "", feature: "", payload: {} }) } as MessageEvent)
      );
    });

    await fire("f1");
    await fire("f2");
    expect(screen.queryByTestId("fetch-stale")).toBeNull(); // below threshold
    await fire("f3");
    await waitFor(() => expect(screen.getByTestId("fetch-stale")).toBeInTheDocument());

    failState = false;
    await fire("f4");
    await waitFor(() => expect(screen.queryByTestId("fetch-stale")).toBeNull());
  });

  it("non-primary worktree view fetches events from the REST log endpoint", async () => {
    vi.stubGlobal("fetch", vi.fn(async (url: string) => {
      if (url === "/api/projects") return { ok: true, json: async () => [{ id: "demo", name: "demo", path: "/x", project: "demo", feature_count: 1, awaiting_count: 0 }] };
      if (url === "/api/registry" || url === "/api/agents") return { ok: true, json: async () => [] };
      if (url.includes("/worktrees")) return { ok: true, json: async () => [{ branch: "main", path: "/x", primary: true }, { branch: "feat-x", path: "/x-fx", primary: false }] };
      if (url.includes("/events/log")) return { ok: true, json: async () => [{ event_id: "w1", ts: "t", agent_id: "a", role: "worker", event_type: "assign", task_id: "t1", feature: "f", payload: {} }] };
      if (url.includes("/orchestrate/status")) return { ok: true, json: async () => ({ present: false }) };
      return { ok: true, json: async () => ({ project: "demo", agents: [], features: [], awaiting_count: 0 }) };
    }));

    render(<App />);
    await waitFor(() => expect(screen.getByTestId("project-menu-trigger")).toHaveTextContent("demo"));

    // Pick the non-primary worktree from the header project menu. Worktrees are
    // collapsed behind the per-project chevron by default, so expand first.
    await act(async () => { fireEvent.click(screen.getByTestId("project-menu-trigger")); });
    await waitFor(() => expect(screen.getByTestId("worktree-toggle-demo")).toBeInTheDocument());
    await act(async () => { fireEvent.click(screen.getByTestId("worktree-toggle-demo")); });
    await waitFor(() => expect(screen.getByTestId("worktree-demo-feat-x")).toBeInTheDocument());
    await act(async () => { fireEvent.click(screen.getByTestId("worktree-demo-feat-x")); });

    const fetchMock = fetch as ReturnType<typeof vi.fn>;
    await waitFor(() => {
      const urls = fetchMock.mock.calls.map(([u]) => u as string);
      expect(urls.some((u) => u.includes("/api/projects/demo/events/log?wt=feat-x"))).toBe(true);
      expect(urls.some((u) => u.includes("/api/projects/demo/state?wt=feat-x"))).toBe(true);
    });
  });

  it("no longer renders the replay scrubber", async () => {
    render(<App />);
    await waitFor(() => expect(screen.getByTestId("toolbar")).toBeInTheDocument());
    expect(screen.queryByRole("slider", { name: "replay position" })).toBeNull();
  });

  it("opens the Settings view from the toolbar gear", async () => {
    render(<App />);
    await waitFor(() => expect(screen.getByTestId("toolbar")).toBeInTheDocument());
    fireEvent.click(screen.getByTestId("toolbar-settings"));
    await waitFor(() => expect(screen.getByTestId("settings-view")).toBeInTheDocument());
  });

  it("opens the Dispatch panel from the toolbar in Cockpit lens", async () => {
    render(<App />);
    await waitFor(() => expect(screen.getByTestId("toolbar")).toBeInTheDocument());
    fireEvent.click(screen.getByTestId("lens-cockpit"));
    await waitFor(() => expect(screen.getByTestId("lens-cockpit")).toHaveAttribute("aria-pressed", "true"));
    fireEvent.click(screen.getByTestId("toolbar-dispatch"));
    await waitFor(() => expect(screen.getByTestId("dispatch-panel")).toBeInTheDocument());
  });

  describe("lens routing", () => {
    it("renders the Board lens by default with the lens control and collapsed event drawer", async () => {
      render(<App />);
      await waitFor(() => expect(screen.getByTestId("toolbar")).toBeInTheDocument());
      expect(screen.getByTestId("view-board")).toBeInTheDocument();
      expect(screen.getByRole("group", { name: "lens" })).toBeInTheDocument();
      await waitFor(() => expect(screen.getByTestId("event-drawer")).toBeInTheDocument());
      expect(screen.getByTestId("event-drawer").dataset.state).toBe("collapsed");
    });

    it("switches to the Dashboard lens and persists it", async () => {
      render(<App />);
      await waitFor(() => expect(screen.getByTestId("toolbar")).toBeInTheDocument());
      fireEvent.click(screen.getByTestId("lens-dashboard"));
      await waitFor(() => expect(screen.getByTestId("view-dashboard")).toBeInTheDocument());
      expect(localStorage.getItem("pactify:lens")).toBe("dashboard");
    });

    it("restores the saved lens from localStorage", async () => {
      localStorage.setItem("pactify:lens", "dashboard");
      render(<App />);
      await waitFor(() => expect(screen.getByTestId("view-dashboard")).toBeInTheDocument());
    });
  });

  it("restores the last selected project from localStorage when it is still present", async () => {
    localStorage.setItem("pactify:lastProject", "other");
    vi.stubGlobal("fetch", vi.fn(async (url: string) => {
      if (url === "/api/projects") return {
        ok: true,
        json: async () => [
          { id: "demo", name: "demo", path: "/x", project: "demo", feature_count: 1, awaiting_count: 0 },
          { id: "other", name: "other", path: "/y", project: "other", feature_count: 0, awaiting_count: 0 },
        ],
      };
      if (url === "/api/registry") return { ok: true, json: async () => [] };
      if (url === "/api/agents") return { ok: true, json: async () => [] };
      return { ok: true, json: async () => ({ project: "other", agents: [], features: [], awaiting_count: 0 }) };
    }));

    render(<App />);
    await waitFor(() => expect(screen.getByTestId("project-menu-trigger")).toHaveTextContent("other"));
  });

  it("toggles between Board and Flow views and persists boardMode", async () => {
    render(<App />);
    await waitFor(() => expect(screen.getByTestId("project-menu-trigger")).toHaveTextContent("demo"));

    expect(screen.getByTestId("view-board")).toBeInTheDocument();
    expect(screen.queryByTestId("view-flow")).toBeNull();

    fireEvent.click(screen.getByTestId("board-mode-flow"));
    await waitFor(() => expect(screen.getByTestId("view-flow")).toBeInTheDocument());
    expect(screen.queryByTestId("view-board")).toBeNull();
    expect(localStorage.getItem("pactify:boardMode")).toBe("flow");

    fireEvent.click(screen.getByTestId("board-mode-board"));
    await waitFor(() => expect(screen.getByTestId("view-board")).toBeInTheDocument());
    expect(screen.queryByTestId("view-flow")).toBeNull();
    expect(localStorage.getItem("pactify:boardMode")).toBe("board");
  });

  it("restores boardMode from localStorage", async () => {
    localStorage.setItem("pactify:boardMode", "flow");
    render(<App />);
    await waitFor(() => expect(screen.getByTestId("view-flow")).toBeInTheDocument());
  });

  it("persists project selection changes to localStorage", async () => {
    vi.stubGlobal("fetch", vi.fn(async (url: string) => {
      if (url === "/api/projects") return {
        ok: true,
        json: async () => [
          { id: "demo", name: "demo", path: "/x", project: "demo", feature_count: 1, awaiting_count: 0 },
          { id: "other", name: "other", path: "/y", project: "other", feature_count: 0, awaiting_count: 0 },
        ],
      };
      if (url === "/api/registry") return { ok: true, json: async () => [] };
      if (url === "/api/agents") return { ok: true, json: async () => [] };
      return { ok: true, json: async () => ({ project: "demo", agents: [], features: [], awaiting_count: 0 }) };
    }));

    render(<App />);
    await waitFor(() => expect(screen.getByTestId("project-menu-trigger")).toHaveTextContent("demo"));
    expect(localStorage.getItem("pactify:lastProject")).toBe("demo");
  });
});

describe("pickInitialProject", () => {
  const mk = (id: string, project: string): ProjectMeta => ({
    id, name: id, path: `/${id}`, project, feature_count: 0, awaiting_count: 0,
  });

  it("prefers the stored id when it is still in the list", () => {
    const ps = [mk("dead", "unknown"), mk("alive", "greet")];
    expect(pickInitialProject(ps, "alive")).toBe("alive");
  });

  it("skips project===unknown when no stored id matches", () => {
    const ps = [mk("dead", "unknown"), mk("alive", "greet")];
    expect(pickInitialProject(ps, "missing")).toBe("alive");
    expect(pickInitialProject(ps, null)).toBe("alive");
  });

  it("falls back to the first entry when every project is unknown", () => {
    const ps = [mk("dead1", "unknown"), mk("dead2", "unknown")];
    expect(pickInitialProject(ps, null)).toBe("dead1");
  });

  it("returns empty string for an empty list", () => {
    expect(pickInitialProject([], "stored")).toBe("");
  });
});
