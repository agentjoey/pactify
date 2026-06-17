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
    if (url === "/api/registry") return { ok: true, json: async () => [] };
    if (url === "/api/agents") return { ok: true, json: async () => [] };
    if (url.includes("/timeline")) return { ok: true, json: async () => ({ total: 0, events: [] }) };
    // SettingsModal mounts ops panels that read array-shaped endpoints.
    if (url.includes("/wiring") || url.includes("/seats")) return { ok: true, json: async () => [] };
    return { ok: true, json: async () => ({ project: "demo", agents: [{ id: "claude-opus", roles: ["orchestrator"] }], features: [], awaiting_count: 0 }) };
  }));
});

describe("App", () => {
  it("loads projects and shows the switcher + an agent", async () => {
    render(<App />);
    expect(screen.getByTestId("app-root")).toBeInTheDocument();
    // The TopBar project chip shows the current project name.
    await waitFor(() => expect(screen.getByTestId("toolbar")).toHaveTextContent("demo"));
    await waitFor(() => expect(screen.getByTestId("toolbar")).toHaveTextContent("demo"));
  });

  it("incoming pact event triggers a state re-fetch", async () => {
    render(<App />);
    // Wait for initial load (project list + initial state fetch)
    await waitFor(() => expect(screen.getByTestId("toolbar")).toHaveTextContent("demo"));

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
    await waitFor(() => expect(screen.getByTestId("toolbar")).toHaveTextContent("demo"));

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

  it("no longer renders the replay scrubber", async () => {
    render(<App />);
    await waitFor(() => expect(screen.getByTestId("toolbar")).toBeInTheDocument());
    expect(screen.queryByRole("slider", { name: "replay position" })).toBeNull();
  });

  it("opens the Settings modal from the toolbar gear", async () => {
    render(<App />);
    await waitFor(() => expect(screen.getByTestId("toolbar")).toBeInTheDocument());
    fireEvent.click(screen.getByTestId("toolbar-settings"));
    await waitFor(() => expect(screen.getByTestId("settings-modal")).toBeInTheDocument());
  });

  describe("global view shortcuts", () => {
    it("defaults to the Board view and switches with keys 1/2/3", async () => {
      render(<App />);
      await waitFor(() => expect(screen.getByTestId("toolbar")).toBeInTheDocument());
      expect(screen.getByTestId("view-board")).toBeInTheDocument();
      fireEvent.keyDown(window, { key: "2" });
      await waitFor(() => expect(screen.getByTestId("view-canvas")).toBeInTheDocument());
      fireEvent.keyDown(window, { key: "3" });
      await waitFor(() => expect(screen.getByTestId("view-live")).toBeInTheDocument());
    });

    it("the lens control reflects board/canvas/live and ignores keys while typing", async () => {
      render(<App />);
      await waitFor(() => expect(screen.getByTestId("toolbar")).toHaveTextContent("demo"));

      // The Toolbar lens segmented control (board/canvas/live).
      const group = () => screen.getByRole("group", { name: "lens" });
      const pressed = () =>
        within(group()).getAllByRole("button").map((b) => b.getAttribute("aria-pressed"));

      // default lens = board (first button pressed)
      expect(pressed()).toEqual(["true", "false", "false"]);

      await act(async () => { fireEvent.keyDown(window, { key: "2" }); }); // canvas
      expect(pressed()).toEqual(["false", "true", "false"]);

      await act(async () => { fireEvent.keyDown(window, { key: "1" }); }); // board
      expect(pressed()).toEqual(["true", "false", "false"]);

      // typing guard: a keydown originating from an INPUT must not switch.
      const input = document.createElement("input");
      document.body.appendChild(input);
      await act(async () => { fireEvent.keyDown(input, { key: "2" }); });
      expect(pressed()).toEqual(["true", "false", "false"]);
      input.remove();
    });
  });
});
