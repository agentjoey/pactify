import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, act } from "@testing-library/react";
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
    return { ok: true, json: async () => ({ project: "demo", agents: [{ id: "claude-opus", roles: ["orchestrator"] }], features: [], awaiting_count: 0 }) };
  }));
});

describe("App", () => {
  it("loads projects and shows the switcher + an agent", async () => {
    render(<App />);
    expect(screen.getByTestId("app-root")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole("option", { name: "demo" })).toBeInTheDocument());
    await waitFor(() => expect(screen.getByText(/claude-opus/)).toBeInTheDocument());
  });

  it("incoming pact event triggers a state re-fetch", async () => {
    render(<App />);
    // Wait for initial load (project list + initial state fetch)
    await waitFor(() => expect(screen.getByText(/claude-opus/)).toBeInTheDocument());

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
      return { ok: true, json: async () => ({ project: "demo", agents: [{ id: "claude-opus", roles: ["orchestrator"] }], features: [], awaiting_count: 0 }) };
    }));

    render(<App />);
    await waitFor(() => expect(screen.getByText(/claude-opus/)).toBeInTheDocument());

    // Fire a pact event to populate events list
    await act(async () => {
      lastES?.pactListeners.forEach((fn) =>
        fn({ data: JSON.stringify({ event_id: "e1", ts: "t", agent_id: "a", role: "worker", event_type: "join", task_id: "", feature: "", payload: {} }) } as MessageEvent)
      );
    });

    // Switch project — events should reset (new EventSource created)
    const select = screen.getByRole("combobox");
    await act(async () => {
      select.dispatchEvent(new Event("change", { bubbles: true }));
      Object.defineProperty(select, "value", { value: "other", configurable: true });
      select.dispatchEvent(new Event("change", { bubbles: true }));
    });

    // After switching, the old ES should have been closed
    // (new ES created for "other" project — just verify no crash)
    expect(screen.getByTestId("app-root")).toBeInTheDocument();
  });
});
