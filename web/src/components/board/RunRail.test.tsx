import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import type { State, Feature, OrchestrateStatus } from "../../lib/types";
import { DataSourceProvider, type DataSource } from "../../lib/datasource";

const getOrchestrateStatus = vi.fn();
const getParallelOrchestrate = vi.fn();
const resumeOrchestrate = vi.fn();
const shipFeature = vi.fn();
const getDiff = vi.fn();
const postVerb = vi.fn();
vi.mock("../../lib/api", () => ({
  getOrchestrateStatus: (...args: unknown[]) => getOrchestrateStatus(...args),
  getParallelOrchestrate: (...args: unknown[]) => getParallelOrchestrate(...args),
  resumeOrchestrate: (...args: unknown[]) => resumeOrchestrate(...args),
  shipFeature: (...args: unknown[]) => shipFeature(...args),
  getDiff: (...args: unknown[]) => getDiff(...args),
  postVerb: (...args: unknown[]) => postVerb(...args),
  runOrchestrate: vi.fn(),
  subscribeAgentStream: () => () => {},
}));

import { RunRail } from "./RunRail";

const task = (id: string, status: string, owner = "opencode-worker") => ({
  id, owner, status, reviewer: "claude", spec: "", evidence: "",
});
const st = (features: Feature[]): State => ({
  project: "p1",
  agents: [
    { id: "claude", roles: ["orchestrator", "reviewer"] },
    { id: "opencode-worker", roles: ["worker"] },
  ],
  features,
  awaiting_count: 0,
});
const EMPTY = st([]);

const status = (over: Partial<OrchestrateStatus>): OrchestrateStatus => ({
  feature: "feat-x", task: "t2", seat: "opencode-worker", action: "run_owner", phase: "owner",
  escalated: false, done: false, total: 3, accepted: 1, iter: 2, updated_at: "x", ...over,
});

function renderRail(props: Partial<React.ComponentProps<typeof RunRail>> = {}) {
  return render(
    <DataSourceProvider>
      <RunRail project="p1" state={EMPTY} refreshTick={0} author={true} {...props} />
    </DataSourceProvider>,
  );
}

describe("RunRail (Board run banner — the former Live lanes)", () => {
  beforeEach(() => {
    getOrchestrateStatus.mockReset();
    getParallelOrchestrate.mockReset();
    resumeOrchestrate.mockReset();
    shipFeature.mockReset();
    getDiff.mockReset();
    postVerb.mockReset();
    getParallelOrchestrate.mockResolvedValue({ present: false });
  });

  // The rail is a banner, not a view: with no orchestrate activity it renders
  // NOTHING — the Board columns already tell the whole story.
  it("renders nothing when orchestrate has no activity", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: false });
    renderRail();
    await waitFor(() => expect(getOrchestrateStatus).toHaveBeenCalled());
    expect(screen.queryByTestId("run-rail")).toBeNull();
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
      renderRail({ onNotify, state: gatedState() });
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
      fireEvent.click(screen.getByRole("button", { name: "Approve merge" }));
      expect(postVerb).not.toHaveBeenCalled();
      expect(screen.getByText(/overrides the failed hard gate/i)).toBeTruthy();
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
      renderRail({ author: false, state: gatedState() });
      await waitFor(() => expect(screen.getByTestId("review-gate")).toBeTruthy());
      expect(screen.queryByRole("button", { name: "Reject → rework" })).toBeNull();
      expect(screen.queryByRole("button", { name: "Approve merge" })).toBeNull();
    });
  });

  it("renders a feature lane with task pipeline chips from state", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: true, status: status({}) });
    renderRail({
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

  it("shows only driver-touched features as lanes (Board columns own the rest)", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: true, status: status({ feature: "feat-x" }) });
    renderRail({
      state: st([
        { id: "feat-x", branch: "feat/x", status: "in_progress", tasks: [task("t1", "in_progress")] },
        { id: "feat-idle", branch: "feat/idle", status: "in_progress", tasks: [task("t9", "assigned")] },
      ]),
    });
    await waitFor(() => expect(screen.getByTestId("feature-lane")).toBeTruthy());
    expect(screen.getAllByTestId("feature-lane")).toHaveLength(1);
    expect(screen.queryByText("feat-idle")).toBeNull();
  });

  it("shows the review gate with reason + Resume/See diff when a gate fails", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: true, status: status({ escalated: true, action: "stuck", reason: "FAIL TestRetryCap" }) });
    resumeOrchestrate.mockResolvedValue({});
    getDiff.mockResolvedValue({ diff: "diff --git a/foo b/foo" });
    const onNotify = vi.fn();
    renderRail({
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
    renderRail({
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
    renderRail({
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
    renderRail({
      state: st([{ id: "feat-x", branch: "feat/x", status: "in_progress", tasks: [
        task("t1", "accepted"), task("t2", "in_progress"),
      ] }]),
    });
    await waitFor(() => expect(screen.getByTestId("fixing-indicator")).toBeTruthy());
    expect(screen.getByTestId("fixing-indicator").textContent).toContain("修复中 2/3");
  });

  it("shows no fixing indicator in a normal (owner) phase", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: true, status: status({}) });
    renderRail({
      state: st([{ id: "feat-x", branch: "feat/x", status: "in_progress", tasks: [
        task("t1", "accepted"), task("t2", "in_progress"),
      ] }]),
    });
    await waitFor(() => expect(screen.getByTestId("feature-lane")).toBeTruthy());
    expect(screen.queryByTestId("fixing-indicator")).toBeNull();
  });

  it("expands the first working lane's agent terminal by default", async () => {
    getOrchestrateStatus.mockResolvedValue({ present: true, status: status({}) });
    renderRail({
      state: st([{ id: "feat-x", branch: "feat/x", status: "in_progress", tasks: [
        task("t1", "accepted"), task("t2", "in_progress"),
      ] }]),
    });
    await waitFor(() => expect(screen.getByTestId("agent-terminal")).toBeTruthy());
  });
});

describe("RunRail — capability gating", () => {
  it("disables Ship when the source is read-only", async () => {
    const readOnly = {
      capabilities: { canWrite: false, canOrchestrate: false, multiMachine: true },
      listProjects: vi.fn(),
      getState: vi.fn(),
      getStats: vi.fn().mockResolvedValue({ tasks: [], agents: [] }),
      subscribe: vi.fn(),
      getOrchestrateStatus: vi.fn().mockResolvedValue({ present: true, status: { feature: "feat-x", task: "", seat: "", action: "done", phase: "done", escalated: false, done: true, total: 2, accepted: 2, iter: 3, updated_at: "x" } }),
      getParallelOrchestrate: vi.fn().mockResolvedValue({ present: false }),
      runOrchestrate: vi.fn(),
      resumeOrchestrate: vi.fn(),
      shipFeature: vi.fn(),
      getDiff: vi.fn(),
    };
    render(
      <DataSourceProvider source={readOnly as unknown as DataSource}>
        <RunRail
          project="p1"
          state={st([{ id: "feat-x", branch: "feat/x", status: "in_progress", tasks: [task("t1", "accepted"), task("t2", "accepted")] }])}
          refreshTick={0}
          author={true}
        />
      </DataSourceProvider>,
    );
    await waitFor(() => expect(screen.getByRole("button", { name: "Ship" })).toBeTruthy());
    expect(screen.getByRole("button", { name: "Ship" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Ship" })).toHaveAttribute("title", "Remote control needs U3");
  });
});

// A hosted-like source omits the orchestrate read methods entirely: the rail
// must render NOTHING (no throw, no note) — hosted boards stay clean and the
// event drawer/board columns carry the story.
describe("RunRail — hosted method-presence guard", () => {
  it("renders nothing when status methods are absent", () => {
    const hosted = {
      capabilities: { canWrite: true, canOrchestrate: true, multiMachine: true },
      listProjects: vi.fn().mockResolvedValue([]),
      getState: vi.fn(),
      getStats: vi.fn().mockResolvedValue({ tasks: [], agents: [] }),
      subscribe: vi.fn().mockReturnValue(() => {}),
      runOrchestrate: vi.fn(),
      resumeOrchestrate: vi.fn(),
      // deliberately no getOrchestrateStatus / getParallelOrchestrate
    };
    render(
      <DataSourceProvider source={hosted as unknown as DataSource}>
        <RunRail
          project="p1"
          state={st([{ id: "feat-x", branch: "feat/x", status: "in_progress", tasks: [task("t1", "accepted"), task("t2", "in_progress")] }])}
          refreshTick={0}
          author={true}
        />
      </DataSourceProvider>,
    );
    expect(screen.queryByTestId("run-rail")).toBeNull();
  });

  it("hides the Ship button when the source lacks shipFeature (even when done)", async () => {
    const src = {
      capabilities: { canWrite: true, canOrchestrate: true, multiMachine: true },
      listProjects: vi.fn().mockResolvedValue([]),
      getState: vi.fn(),
      getStats: vi.fn().mockResolvedValue({ tasks: [], agents: [] }),
      subscribe: vi.fn().mockReturnValue(() => {}),
      runOrchestrate: vi.fn(),
      resumeOrchestrate: vi.fn(),
      getOrchestrateStatus: vi.fn().mockResolvedValue({ present: true, status: { feature: "feat-x", task: "", seat: "", action: "done", phase: "done", escalated: false, done: true, total: 2, accepted: 2, iter: 3, updated_at: "x" } }),
      getParallelOrchestrate: vi.fn().mockResolvedValue({ present: false }),
      // deliberately no shipFeature / getDiff
    };
    render(
      <DataSourceProvider source={src as unknown as DataSource}>
        <RunRail
          project="p1"
          state={st([{ id: "feat-x", branch: "feat/x", status: "in_progress", tasks: [task("t1", "accepted"), task("t2", "accepted")] }])}
          refreshTick={0}
          author={true}
        />
      </DataSourceProvider>,
    );
    await waitFor(() => expect(screen.getByText("Delivered")).toBeTruthy());
    expect(screen.queryByRole("button", { name: "Ship" })).toBeNull();
  });
});
