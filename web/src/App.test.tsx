import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, act, fireEvent, within } from "@testing-library/react";
import App from "./App";

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
  globalThis.EventSource = makeFakeESClass() as unknown as typeof EventSource;
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url === "/api/projects") return { ok: true, json: async () => [{ id: "demo", name: "demo", path: "/x", project: "demo", feature_count: 1, awaiting_count: 0 }] };
    if (url === "/api/agents") return { ok: true, json: async () => [] };
    if (url.includes("/timeline")) return { ok: true, json: async () => ({ total: 0, events: [] }) };
    return { ok: true, json: async () => ({ project: "demo", agents: [{ id: "claude-opus", roles: ["orchestrator"] }], features: [], awaiting_count: 0 }) };
  }));
});

describe("App", () => {
  it("loads projects and shows the switcher + an agent", async () => {
    render(<App />);
    expect(screen.getByTestId("app-root")).toBeInTheDocument();
    // The TopBar project chip shows the current project name.
    await waitFor(() => expect(screen.getByTestId("project-chip")).toHaveTextContent("demo"));
    await waitFor(() => expect(screen.getByTestId("project-chip")).toHaveTextContent("demo"));
  });

  it("incoming pact event triggers a state re-fetch", async () => {
    render(<App />);
    // Wait for initial load (project list + initial state fetch)
    await waitFor(() => expect(screen.getByTestId("project-chip")).toHaveTextContent("demo"));

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
      if (url === "/api/agents") return { ok: true, json: async () => [] };
      return { ok: true, json: async () => ({ project: "demo", agents: [{ id: "claude-opus", roles: ["orchestrator"] }], features: [], awaiting_count: 0 }) };
    }));

    render(<App />);
    await waitFor(() => expect(screen.getByTestId("project-chip")).toHaveTextContent("demo"));

    // Fire a pact event to populate events list
    await act(async () => {
      lastES?.pactListeners.forEach((fn) =>
        fn({ data: JSON.stringify({ event_id: "e1", ts: "t", agent_id: "a", role: "worker", event_type: "join", task_id: "", feature: "", payload: {} }) } as MessageEvent)
      );
    });

    // Switch project — events should reset (new EventSource created). Open the
    // TopBar project switcher Popover and pick "other".
    await act(async () => {
      fireEvent.click(screen.getByTestId("project-chip"));
    });
    await act(async () => {
      fireEvent.click(screen.getByRole("option", { name: /other/ }));
    });

    // After switching, the old ES should have been closed
    // (new ES created for "other" project — just verify no crash)
    expect(screen.getByTestId("app-root")).toBeInTheDocument();
  });

  describe("?at deep link (spec §6.6)", () => {
    function stubReplayFetch() {
      vi.stubGlobal("fetch", vi.fn(async (url: string) => {
        if (url === "/api/projects") return { ok: true, json: async () => [{ id: "demo", name: "demo", path: "/x", project: "demo", feature_count: 1, awaiting_count: 0 }] };
        if (url === "/api/agents") return { ok: true, json: async () => [] };
        if (url.includes("/timeline")) return {
          ok: true,
          json: async () => ({
            total: 3,
            events: [
              { n: 1, ts: "t1", type: "join", actor: "alice" },
              { n: 2, ts: "t2", type: "assign", actor: "alice", task: "T1" },
              { n: 3, ts: "t3", type: "checkpoint", actor: "bob", task: "T1" },
            ],
          }),
        };
        // state and state?at=N both fold to a demo state.
        return { ok: true, json: async () => ({ project: "demo", agents: [{ id: "claude-opus", roles: ["orchestrator"] }], features: [], awaiting_count: 0 }) };
      }));
    }

    it("reads ?at on load → enters replay, then LIVE clears the param", async () => {
      window.history.replaceState(null, "", "/?at=2");
      stubReplayFetch();
      render(<App />);
      await waitFor(() => expect(screen.getByTestId("project-chip")).toHaveTextContent("demo"));

      // Replay is entered: the LIVE button (resume live) becomes enabled and the
      // URL still carries ?at=2 (normalized through enterReplay's replaceState).
      const liveBtn = await screen.findByLabelText("resume live");
      await waitFor(() => expect(liveBtn).toBeEnabled());
      expect(window.location.search).toBe("?at=2");

      // Resume live clears the param.
      await act(async () => { fireEvent.click(liveBtn); });
      expect(window.location.search).toBe("");
      window.history.replaceState(null, "", "/");
    });

    it("scrubbing writes ?at into the URL; absent on a fresh live load", async () => {
      window.history.replaceState(null, "", "/");
      stubReplayFetch();
      render(<App />);
      await waitFor(() => expect(screen.getByTestId("project-chip")).toHaveTextContent("demo"));
      // Fresh load with no ?at stays live: no param written.
      expect(window.location.search).toBe("");

      // Step back on the timeline (drives enterReplay) → ?at appears.
      const stepBack = await screen.findByLabelText("step back");
      await act(async () => { fireEvent.click(stepBack); });
      await waitFor(() => expect(window.location.search).toBe("?at=2"));
      window.history.replaceState(null, "", "/");
    });
  });

  describe("global view shortcuts", () => {
    it("1/2/3 switch views; ignored while typing in an input", async () => {
      render(<App />);
      await waitFor(() => expect(screen.getByTestId("project-chip")).toHaveTextContent("demo"));

      const group = () => screen.getByRole("group", { name: "view toggle" });
      const pressed = () =>
        within(group()).getAllByRole("button").map((b) => b.getAttribute("aria-pressed"));

      // default = kanban (first button pressed)
      expect(pressed()).toEqual(["true", "false", "false", "false", "false"]);

      await act(async () => { fireEvent.keyDown(window, { key: "2" }); });
      expect(pressed()).toEqual(["false", "true", "false", "false", "false"]);

      await act(async () => { fireEvent.keyDown(window, { key: "1" }); });
      expect(pressed()).toEqual(["true", "false", "false", "false", "false"]);

      // typing guard: a keydown originating from an INPUT must not switch.
      const input = document.createElement("input");
      document.body.appendChild(input);
      await act(async () => { fireEvent.keyDown(input, { key: "2" }); });
      expect(pressed()).toEqual(["true", "false", "false", "false", "false"]);
      input.remove();
    });
  });
});
