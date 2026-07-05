import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import type { State, Feature, OrchestrateStatus } from "../lib/types";
import { DataSourceProvider, type DataSource } from "../lib/datasource";

const getOrchestrateStatus = vi.fn();
const getParallelOrchestrate = vi.fn();
const runOrchestrate = vi.fn();
const resumeOrchestrate = vi.fn();
const shipFeature = vi.fn();
const getDiff = vi.fn();
const postVerb = vi.fn();
vi.mock("../lib/api", () => ({
  getOrchestrateStatus: (...args: unknown[]) => getOrchestrateStatus(...args),
  getParallelOrchestrate: (...args: unknown[]) => getParallelOrchestrate(...args),
  runOrchestrate: (...args: unknown[]) => runOrchestrate(...args),
  resumeOrchestrate: (...args: unknown[]) => resumeOrchestrate(...args),
  shipFeature: (...args: unknown[]) => shipFeature(...args),
  getDiff: (...args: unknown[]) => getDiff(...args),
  postVerb: (...args: unknown[]) => postVerb(...args),
  subscribeAgentStream: () => () => {},
}));

import { LiveOrchestrate } from "./LiveOrchestrate";

const FULL_ROSTER = [
  { id: "claude", roles: ["orchestrator", "reviewer"] },
  { id: "opencode-worker", roles: ["worker"] },
];

const task = (id: string, status: string, owner = "opencode-worker") => ({
  id, owner, status, reviewer: "claude", spec: "", evidence: "",
});
const st = (features: Feature[]): State => ({ project: "p1", agents: FULL_ROSTER, features, awaiting_count: 0 });
const EMPTY = st([]);

const status = (over: Partial<OrchestrateStatus>): OrchestrateStatus => ({
  feature: "feat-x", task: "t2", seat: "opencode-worker", action: "run_owner", phase: "owner",
  escalated: false, done: false, total: 3, accepted: 1, iter: 2, updated_at: "x", ...over,
});

function renderLive(props: Partial<React.ComponentProps<typeof LiveOrchestrate>> = {}) {
  return render(
    <DataSourceProvider>
      <LiveOrchestrate project="p1" state={EMPTY} refreshTick={0} author={true} agents={FULL_ROSTER} {...props} />
    </DataSourceProvider>,
  );
}

describe("LiveOrchestrate (lanes redesign)", () => {
  beforeEach(() => {
    getOrchestrateStatus.mockReset();
    getParallelOrchestrate.mockReset();
    runOrchestrate.mockReset();
    resumeOrchestrate.mockReset();
    shipFeature.mockReset();
    getDiff.mockReset();
    postVerb.mockReset();
    getParallelOrchestrate.mockResolvedValue({ present: false });
  });

  // P1-7 Review Gate: the five-action panel. Reject→rework and Approve merge are
  // backed by existing pact verbs (changes/accept/merge) via postVerb.
  describe("Review Gate decisions", () => {
    const gatedState = () =>
      st([{ id: "feat-x", branch: "feat/x", status: "in_progress", tasks: [
        task("t1", "accepted"), task("t2", "awaiting_review"),
      ] }]);

    async function renderGated(onNotify = vi.fn()) {
      getOrchestrateStatus.mockResolvedValue({ present: true, status: status({ escalated: true, action: "stuck", reason: "FAIL TestX" }) });
      postVerb.mockResolvedValue(undefined);
      renderLive({ onNotify, state: gatedState() });
      await waitFor(() => expect(screen.getByTestId("review-gate")).toBeTruthy());
      return onNotify;
    }

    it("Reject → rework requests changes on the awaiting task with the human's feedback", async () => {
      const onNotify = await renderGated();
      fireEvent.click(screen.getByRole("button", { name: "Reject → rework" }));
      fireEvent.change(screen.getByTestId("reject-feedback"), { target: { value: "handle the empty case" } });
      fireEvent.click(screen.getByRole("button", { name: "Send back for rework" }));
      await waitFor(() =>
        expect(postVerb).toHaveBeenCalledWith("p1", "changes", { task: "t2", reason: "handle the empty case" }),
      );
      await waitFor(() => expect(onNotify).toHaveBeenCalledWith(expect.stringContaining("rework")));
    });

    it("Approve merge overrides the gate: two-step confirm, then accept awaiting + merge", async () => {
      const onNotify = await renderGated();
      // First click = arm the override confirm (no verbs yet).
      fireEvent.click(screen.getByRole("button", { name: "Approve merge" }));
      expect(postVerb).not.toHaveBeenCalled();
      expect(screen.getByText(/overrides the failed hard gate/i)).toBeTruthy();
      // Confirm = accept the awaiting task then merge the feature, in order.
      fireEvent.click(screen.getByTestId("approve-merge-confirm"));
      await waitFor(() => expect(postVerb).toHaveBeenCalledWith("p1", "accept", { task: "t2" }));
      await waitFor(() => expect(postVerb).toHaveBeenCalledWith("p1", "merge", { feature: "feat-x" }));
      const order = postVerb.mock.calls.map((c) => c[1]);
      expect(order.indexOf("accept")).toBeLessThan(order.indexOf("merge"));
      await waitFor(() => expect(onNotify).toHaveBeenCalledWith(expect.stringContaining("overridden")));
    });

    it("Take over reveals the manual-drive commands without any backend call", async () => {
      await renderGated();
      fireEvent.click(screen.getByRole("button", { name: "Take over" }));
      expect(screen.getByText(/Drive this feature by hand/i)).toBeTruthy();
      expect(screen.getByText(/pactify merge <feature>/)).toBeTruthy();
      expect(postVerb).not.toHaveBeenCalled();
    });

    it("gate decisions are hidden in observe mode (author=false)", async () => {
      getOrchestrateStatus.mockResolvedValue({ present: true, status: status({ escalated: true, reason: "FAIL" }) });
      renderLive({ author: false, state: gatedState() });
      await waitFor(() => expect(screen.getByTestId("review-gate")).toBeTruthy());
      expect(screen.queryByRole("button", { name: "Reject → rework" })).toBeNull();
      expect(screen.queryByRole("button", { name: "Approve merge" })).toBeNull();
    });
  });

  it("renders empty state + Run when nothing has run and there are no features", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: false });
    renderLive();
    await waitFor(() => expect(screen.getByText("orchestrate hasn't run yet")).toBeTruthy());
    expect(screen.getByRole("button", { name: "Run" })).toBeTruthy();
  });

  it("Run is disabled when the roster is incomplete", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: false });
    renderLive({ agents: [{ id: "claude", roles: ["reviewer"] }] });
    await waitFor(() => expect(screen.getByRole("button", { name: "Run" })).toBeTruthy());
    expect((screen.getByRole("button", { name: "Run" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("Run is disabled in observe mode", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: false });
    renderLive({ author: false });
    await waitFor(() => expect(screen.getByRole("button", { name: "Run" })).toBeTruthy());
    expect((screen.getByRole("button", { name: "Run" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("renders a feature lane with task pipeline chips from state", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: true, status: status({}) });
    renderLive({
      state: st([{ id: "feat-x", branch: "feat/x", status: "in_progress", tasks: [
        task("t1", "accepted"), task("t2", "in_progress"), task("t3", "assigned"),
      ] }]),
    });
    await waitFor(() => expect(screen.getByTestId("feature-lane")).toBeTruthy());
    expect(screen.getByText("feat-x")).toBeTruthy();
    expect(screen.getByText("feat/x")).toBeTruthy();
    expect(screen.getByText("t1")).toBeTruthy();
    expect(screen.getByText("t2")).toBeTruthy();
    expect(screen.getByText("t3")).toBeTruthy();
    expect(screen.getByText("working")).toBeTruthy();
  });

  it("shows the review gate with reason + Resume/See diff when a gate fails", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: true, status: status({ escalated: true, action: "stuck", reason: "FAIL TestRetryCap" }) });
    resumeOrchestrate.mockResolvedValue({});
    getDiff.mockResolvedValue({ diff: "diff --git a/foo b/foo" });
    const onNotify = vi.fn();
    renderLive({
      onNotify,
      state: st([{ id: "feat-x", branch: "feat/x", status: "in_progress", tasks: [
        task("t1", "accepted"), task("t2", "in_progress"),
      ] }]),
    });
    await waitFor(() => expect(screen.getByTestId("review-gate")).toBeTruthy());
    expect(screen.getByText("Hard test gate failed — human decision required")).toBeTruthy();
    expect(screen.getByText("FAIL TestRetryCap")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Resume run ▸" }));
    await waitFor(() => expect(resumeOrchestrate).toHaveBeenCalledWith("p1", undefined));
    await waitFor(() => expect(onNotify).toHaveBeenCalledWith("Orchestrate resumed"));

    fireEvent.click(screen.getByRole("button", { name: "See diff" }));
    await waitFor(() => expect(getDiff).toHaveBeenCalledWith("p1"));
    await waitFor(() => expect(screen.getByText("diff --git a/foo b/foo")).toBeTruthy());
  });

  it("run control shows accepted/total progress", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: true, status: status({}) });
    renderLive({
      state: st([{ id: "feat-x", branch: "feat/x", status: "in_progress", tasks: [
        task("t1", "accepted"), task("t2", "in_progress"), task("t3", "assigned"),
      ] }]),
    });
    await waitFor(() => expect(screen.getAllByText("1 / 3 accepted").length).toBeGreaterThan(0));
    expect(screen.getByText("Orchestrating")).toBeTruthy();
  });

  it("Ship opens the PR form and posts ship when the run is done", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: true, status: status({ done: true, action: "done", task: "", accepted: 2, total: 2 }) });
    shipFeature.mockResolvedValue({ pushed: true, pr_url: "https://github.com/org/repo/pull/42" });
    const onNotify = vi.fn();
    renderLive({
      onNotify,
      state: st([{ id: "feat-x", branch: "feat/x", status: "in_progress", tasks: [task("t1", "accepted"), task("t2", "accepted")] }]),
    });
    await waitFor(() => expect(screen.getByRole("button", { name: "Ship" })).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "Ship" }));
    await waitFor(() => expect(screen.getByText("Ship feature")).toBeTruthy());
    fireEvent.change(screen.getByPlaceholderText("feat-xyz"), { target: { value: "feat-x" } });
    fireEvent.change(screen.getByPlaceholderText("feat: …"), { target: { value: "feat: x" } });
    fireEvent.click(screen.getByRole("button", { name: "Open PR" }));
    await waitFor(() =>
      expect(shipFeature).toHaveBeenCalledWith("p1", { pr: true, head: "feat-x", title: "feat: x", body: "" }),
    );
    await waitFor(() => expect(screen.getByText("https://github.com/org/repo/pull/42")).toBeTruthy());
  });

  it("shows the 修复中 indicator when the run phase is fixing", async () => {
    getOrchestrateStatus.mockResolvedValue({
      present: true,
      status: status({ phase: "fixing", fix_round: 2, fix_max: 3 }),
    });
    renderLive({
      state: st([{ id: "feat-x", branch: "feat/x", status: "in_progress", tasks: [
        task("t1", "accepted"), task("t2", "in_progress"),
      ] }]),
    });
    await waitFor(() => expect(screen.getByTestId("fixing-indicator")).toBeTruthy());
    expect(screen.getByTestId("fixing-indicator").textContent).toContain("修复中 2/3");
  });

  it("shows no fixing indicator in a normal (owner) phase", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: true, status: status({}) });
    renderLive({
      state: st([{ id: "feat-x", branch: "feat/x", status: "in_progress", tasks: [
        task("t1", "accepted"), task("t2", "in_progress"),
      ] }]),
    });
    await waitFor(() => expect(screen.getByTestId("feature-lane")).toBeTruthy());
    expect(screen.queryByTestId("fixing-indicator")).toBeNull();
  });

  it("always renders the event stream pane", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: false });
    renderLive();
    await waitFor(() => expect(screen.getByTestId("event-stream")).toBeTruthy());
  });

  it("expands the first working lane's agent terminal by default", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: true, status: status({}) });
    renderLive({
      state: st([{ id: "feat-x", branch: "feat/x", status: "in_progress", tasks: [
        task("t1", "accepted"), task("t2", "in_progress"),
      ] }]),
    });
    await waitFor(() => expect(screen.getByTestId("agent-terminal")).toBeTruthy());
  });
});

describe("LiveOrchestrate — capability gating", () => {
  it("disables Run/Resume/Ship when the source is read-only", async () => {
    const readOnly = {
      capabilities: { canWrite: false, canOrchestrate: false, multiMachine: true },
      listProjects: vi.fn(),
      getState: vi.fn(),
      getStats: vi.fn().mockResolvedValue({ tasks: [], agents: [] }),
      subscribe: vi.fn(),
      getOrchestrateStatus: vi.fn().mockResolvedValue({ present: true, status: status({ done: true, action: "done", task: "", accepted: 2, total: 2 }) }),
      getParallelOrchestrate: vi.fn().mockResolvedValue({ present: false }),
      runOrchestrate: vi.fn(),
      resumeOrchestrate: vi.fn(),
      shipFeature: vi.fn(),
      getDiff: vi.fn(),
    };
    render(
      <DataSourceProvider source={readOnly}>
        <LiveOrchestrate
          project="p1"
          state={st([{ id: "feat-x", branch: "feat/x", status: "in_progress", tasks: [task("t1", "accepted"), task("t2", "accepted")] }])}
          refreshTick={0}
          author={true}
          agents={FULL_ROSTER}
        />
      </DataSourceProvider>,
    );
    await waitFor(() => expect(screen.getByRole("button", { name: "Ship" })).toBeTruthy());
    expect(screen.getByRole("button", { name: "Ship" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Ship" })).toHaveAttribute("title", "Remote control needs U3");
  });
});

// A hosted-like source: RelaySource can drive orchestrate (canOrchestrate) but
// OMITS the orchestrate read/action methods (status/parallel/diff/ship) that read
// or act on the machine's live driver + git tree. The Live view must guard on
// method presence and degrade gracefully instead of throwing a non-null-assertion.
function hostedSource(over: Partial<DataSource> = {}): DataSource {
  return {
    capabilities: { canWrite: true, canOrchestrate: true, multiMachine: true },
    listProjects: vi.fn().mockResolvedValue([]),
    getState: vi.fn().mockResolvedValue(EMPTY),
    getStats: vi.fn().mockResolvedValue({ tasks: [], agents: [] }),
    subscribe: vi.fn().mockReturnValue(() => {}),
    runOrchestrate: vi.fn().mockResolvedValue({ status_url: "" }),
    resumeOrchestrate: vi.fn().mockResolvedValue({ status_url: "" }),
    // deliberately no getOrchestrateStatus / getParallelOrchestrate / getDiff / shipFeature
    ...over,
  } as unknown as DataSource;
}

describe("LiveOrchestrate — hosted method-presence guards", () => {
  const laneState = st([
    { id: "feat-x", branch: "feat/x", status: "in_progress", tasks: [task("t1", "accepted"), task("t2", "in_progress")] },
  ]);

  it("renders (no throw) with a hosted note + live event stream when status methods are absent", async () => {
    render(
      <DataSourceProvider source={hostedSource()}>
        <LiveOrchestrate project="p1" state={laneState} refreshTick={0} author={true} agents={FULL_ROSTER} />
      </DataSourceProvider>,
    );
    // event stream + lanes still render; a note explains the missing run status.
    expect(screen.getByTestId("event-stream")).toBeTruthy();
    expect(screen.getByTestId("hosted-status-note")).toBeTruthy();
    await waitFor(() => expect(screen.getByTestId("feature-lane")).toBeTruthy());
    // status-driven controls never fabricate (no getOrchestrateStatus to poll).
    expect(screen.queryByRole("button", { name: "Ship" })).toBeNull();
    expect(screen.queryByRole("button", { name: "See diff" })).toBeNull();
  });

  it("hides the Ship button when the source lacks shipFeature (even when done)", async () => {
    const src = hostedSource({
      getOrchestrateStatus: vi.fn().mockResolvedValue({ present: true, status: status({ done: true, action: "done", task: "", accepted: 2, total: 2 }) }),
      getParallelOrchestrate: vi.fn().mockResolvedValue({ present: false }),
    });
    render(
      <DataSourceProvider source={src}>
        <LiveOrchestrate
          project="p1"
          state={st([{ id: "feat-x", branch: "feat/x", status: "in_progress", tasks: [task("t1", "accepted"), task("t2", "accepted")] }])}
          refreshTick={0}
          author={true}
          agents={FULL_ROSTER}
        />
      </DataSourceProvider>,
    );
    await waitFor(() => expect(screen.getByText("Delivered")).toBeTruthy());
    // status IS queryable here → no hosted note, but Ship stays hidden (no method).
    expect(screen.queryByTestId("hosted-status-note")).toBeNull();
    expect(screen.queryByRole("button", { name: "Ship" })).toBeNull();
  });

  it("hides the review-gate See-diff button when the source lacks getDiff (Resume still shown)", async () => {
    const src = hostedSource({
      getOrchestrateStatus: vi.fn().mockResolvedValue({ present: true, status: status({ escalated: true, action: "stuck", reason: "FAIL TestX" }) }),
      getParallelOrchestrate: vi.fn().mockResolvedValue({ present: false }),
    });
    render(
      <DataSourceProvider source={src}>
        <LiveOrchestrate project="p1" state={laneState} refreshTick={0} author={true} agents={FULL_ROSTER} />
      </DataSourceProvider>,
    );
    await waitFor(() => expect(screen.getByTestId("review-gate")).toBeTruthy());
    expect(screen.queryByRole("button", { name: "See diff" })).toBeNull();
    // Resume remains (resumeOrchestrate is present on the hosted source).
    expect(screen.getByRole("button", { name: "Resume run ▸" })).toBeTruthy();
  });
});
