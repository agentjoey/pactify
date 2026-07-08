import { render, screen, fireEvent, within } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import type { State } from "../lib/types";
import { Board } from "./Board";
import { getStats } from "../lib/api";
import { DataSourceProvider } from "../lib/datasource";

// Board pulls per-task tokens from GET /stats (the event log never carries
// token counts); mock the api module so tests control the response.
vi.mock("../lib/api", () => ({
  getStats: vi.fn(),
  postVerb: vi.fn(),
}));
const getStatsMock = vi.mocked(getStats);

beforeEach(() => {
  getStatsMock.mockReset();
  getStatsMock.mockResolvedValue({ tasks: [], agents: [] });
});

const fixture: State = {
  project: "demo",
  awaiting_count: 1,
  agents: [
    { id: "alice", roles: ["orchestrator"] },
    { id: "bob", roles: ["worker"] },
  ],
  features: [
    {
      id: "F1",
      branch: "feat/f1",
      status: "active",
      tasks: [
        { id: "T1", owner: "bob", status: "awaiting_review", reviewer: "alice", spec: "", evidence: "" },
        { id: "T2", owner: "bob", status: "in_progress", reviewer: "alice", spec: "", evidence: "" },
      ],
    },
  ],
};

function renderBoard(ui: React.ReactElement) {
  return render(<DataSourceProvider>{ui}</DataSourceProvider>);
}

describe("Board — live pulse", () => {
  it("a task id in pulses gets the pulse class + status-colored --pulse-color", () => {
    renderBoard(<Board state={fixture} selected="" onSelect={() => {}} pulses={new Set(["T1"])} />);
    const pulsing = screen.getByTestId("board-pulse");
    expect(pulsing.className).toContain("pulse");
    // T1 is awaiting_review → the pact-state color (warn) drives the glow, so the
    // transition pulse reads in the same vocabulary as the StatusPill.
    expect(pulsing.getAttribute("style")).toContain("--pulse-color");
    expect(pulsing.getAttribute("style")).toContain("--color-warn");
    // T1 carries the data-testid; T2 (not pulsing) does not.
    expect(pulsing.textContent).toContain("T1");
  });

  it("no pulses → no pulse marker (first snapshot / quiescent)", () => {
    renderBoard(<Board state={fixture} selected="" onSelect={() => {}} />);
    expect(screen.queryByTestId("board-pulse")).toBeNull();
  });
});

describe("Board — accepted column recent + fold", () => {
  // 13 accepted tasks across two features, in log order t01..t13.
  const manyAccepted: State = {
    project: "demo", awaiting_count: 0,
    agents: [{ id: "a", roles: ["orchestrator"] }],
    features: [
      { id: "f1", branch: "feat-1", status: "merged",
        tasks: Array.from({ length: 8 }, (_, i) => ({
          id: `t${String(i + 1).padStart(2, "0")}`, owner: "a", status: "accepted", reviewer: "a", spec: "", evidence: "",
        })) },
      { id: "f2", branch: "feat-2", status: "active",
        tasks: Array.from({ length: 5 }, (_, i) => ({
          id: `t${String(i + 9).padStart(2, "0")}`, owner: "a", status: "accepted", reviewer: "a", spec: "", evidence: "",
        })) },
    ],
  };

  it("shows the 6 most-recent accepted cards and folds the rest behind a 'more' button", () => {
    renderBoard(<Board state={manyAccepted} selected="" onSelect={() => {}} />);
    // Most-recent-first: t13 (newest) is visible, t01 (oldest, 13th) is folded.
    expect(screen.getAllByText("t13").length).toBeGreaterThan(0);
    expect(screen.queryAllByText("t01")).toHaveLength(0);
    // 13 total − 6 shown = 7 folded.
    expect(screen.getByTestId("accepted-more")).toHaveTextContent("7 more accepted");
  });

  it("expands to show all accepted, then collapses back to recent 6", () => {
    renderBoard(<Board state={manyAccepted} selected="" onSelect={() => {}} />);
    fireEvent.click(screen.getByTestId("accepted-more"));
    expect(screen.getAllByText("t01").length).toBeGreaterThan(0); // folded one now visible
    fireEvent.click(screen.getByTestId("accepted-less"));
    expect(screen.queryAllByText("t01")).toHaveLength(0); // folded again
  });

  it("folds the shipped column to the recent 6 as well", () => {
    const manyShipped: State = {
      project: "demo", awaiting_count: 0,
      agents: [{ id: "a", roles: ["orchestrator"] }],
      features: [{ id: "g", branch: "feat-g", status: "shipped",
        tasks: Array.from({ length: 9 }, (_, i) => ({
          id: `s${String(i + 1).padStart(2, "0")}`, owner: "a", status: "accepted", reviewer: "a", spec: "", evidence: "",
        })) }],
    };
    renderBoard(<Board state={manyShipped} selected="" onSelect={() => {}} />);
    expect(screen.getAllByText("s09").length).toBeGreaterThan(0); // newest shown
    expect(screen.queryAllByText("s01")).toHaveLength(0); // oldest folded
    expect(screen.getByTestId("shipped-more")).toHaveTextContent("3 more shipped");
  });

  it("does not render a fold button when 6 or fewer accepted", () => {
    const few: State = {
      project: "demo", awaiting_count: 0,
      agents: [{ id: "a", roles: ["orchestrator"] }],
      features: [{ id: "f1", branch: "feat-1", status: "active", tasks: [
        { id: "only", owner: "a", status: "accepted", reviewer: "a", spec: "", evidence: "" },
      ]}],
    };
    renderBoard(<Board state={few} selected="" onSelect={() => {}} />);
    expect(screen.getAllByText("only").length).toBeGreaterThan(0);
    expect(screen.queryByTestId("accepted-more")).toBeNull();
  });
});

describe("Board — stats-fed TOK", () => {
  it("does not fetch stats without a project", () => {
    renderBoard(<Board state={fixture} selected="" onSelect={() => {}} />);
    expect(getStatsMock).not.toHaveBeenCalled();
  });

  it("fetches /stats for the project and renders per-task TOK + chip rollup", async () => {
    getStatsMock.mockResolvedValue({
      tasks: [
        { task_id: "T1", feature: "F1", owner: "bob", reviewer: "alice", status: "awaiting_review", duration_sec: 30, added: 0, deleted: 0, tokens: 12_400 },
        { task_id: "T2", feature: "F1", owner: "bob", reviewer: "alice", status: "in_progress", duration_sec: 5, added: 0, deleted: 0, tokens: 0 },
      ],
      agents: [],
    });
    renderBoard(<Board state={fixture} events={[]} selected="" onSelect={() => {}} project="demo" />);
    // T1's card TOK and the F1 feature chip rollup both show the stats value
    // once the debounced fetch lands.
    const hits = await screen.findAllByText("12.4k");
    expect(hits.length).toBeGreaterThanOrEqual(2);
    expect(getStatsMock).toHaveBeenCalledWith("demo");
    // T2 has tokens=0 (unknown) → its strip omits the TOK segment.
    expect(screen.queryByText("—")).toBeNull();
  });

  it("keeps rendering without TOK when the stats fetch fails", async () => {
    getStatsMock.mockRejectedValue(new Error("boom"));
    renderBoard(<Board state={fixture} events={[]} selected="" onSelect={() => {}} project="demo" />);
    // The board still renders; no TOK segments appear because no token data exists.
    expect(await screen.findAllByTestId("task-card")).toHaveLength(2);
    expect(screen.queryByText("—")).toBeNull();
  });
});

describe("Board — capability gating", () => {
  it("disables Accept/Changes when the source is read-only", () => {
    const readOnly = {
      capabilities: { canWrite: false, canOrchestrate: false, multiMachine: true, cockpit: false },
      listProjects: vi.fn(),
      getState: vi.fn(),
      getStats: vi.fn().mockResolvedValue({ tasks: [], agents: [] }),
      subscribe: vi.fn(),
      verb: vi.fn(),
    };
    render(
      <DataSourceProvider source={readOnly}>
        <Board state={fixture} events={[]} selected="" onSelect={() => {}} project="demo" author />
      </DataSourceProvider>,
    );
    const accept = screen.getByTestId("card-accept");
    const changes = screen.getByTestId("card-changes");
    expect(accept).toBeDisabled();
    expect(changes).toBeDisabled();
    expect(accept).toHaveAttribute("title", "Remote control needs U3");
  });
});

describe("Board — blocked dependency visibility", () => {
  const depsFixture: State = {
    project: "demo",
    awaiting_count: 0,
    agents: [{ id: "bob", roles: ["worker"] }],
    features: [
      {
        id: "F1",
        branch: "feat/f1",
        status: "active",
        tasks: [
          { id: "WAIT-1", owner: "bob", status: "assigned", reviewer: "alice", spec: "", evidence: "", deps: ["BLOCK-1"] },
          { id: "BLOCK-1", owner: "bob", status: "in_progress", reviewer: "alice", spec: "", evidence: "" },
          { id: "WAIT-MULTI", owner: "bob", status: "assigned", reviewer: "alice", spec: "", evidence: "", deps: ["BLOCK-1", "BLOCK-2"] },
          { id: "BLOCK-2", owner: "bob", status: "awaiting_review", reviewer: "alice", spec: "", evidence: "" },
          { id: "WAIT-DONE", owner: "bob", status: "assigned", reviewer: "alice", spec: "", evidence: "", deps: ["DONE"] },
          { id: "DONE", owner: "bob", status: "accepted", reviewer: "alice", spec: "", evidence: "" },
          { id: "NODEPS", owner: "bob", status: "in_progress", reviewer: "alice", spec: "", evidence: "" },
        ],
      },
    ],
  };

  function cardById(id: string): HTMLElement {
    return screen.getAllByTestId("task-card").find((card) => card.textContent?.includes(id)) as HTMLElement;
  }

  it("shows blocked badge for assigned/working tasks with unfinished deps", () => {
    renderBoard(<Board state={depsFixture} selected="" onSelect={() => {}} />);
    const wait1 = cardById("WAIT-1");
    const badge = within(wait1).getByTestId("blocked-badge");
    expect(badge.textContent).toContain("⧗ awaiting BLOCK-1");
  });

  it("hides the badge when the dependency is accepted", () => {
    renderBoard(<Board state={depsFixture} selected="" onSelect={() => {}} />);
    const waitDone = cardById("WAIT-DONE");
    expect(within(waitDone).queryByTestId("blocked-badge")).toBeNull();
  });

  it("renders +N and lists every unfinished dep in the badge title", () => {
    renderBoard(<Board state={depsFixture} selected="" onSelect={() => {}} />);
    const waitMulti = cardById("WAIT-MULTI");
    const badge = within(waitMulti).getByTestId("blocked-badge");
    expect(badge.textContent).toContain("⧗ awaiting BLOCK-1 +1");
    expect(badge).toHaveAttribute("title", "BLOCK-1, BLOCK-2");
  });

  it("leaves cards without deps unchanged (no badge)", () => {
    renderBoard(<Board state={depsFixture} selected="" onSelect={() => {}} />);
    const noDeps = cardById("NODEPS");
    expect(within(noDeps).queryByTestId("blocked-badge")).toBeNull();
  });
});
