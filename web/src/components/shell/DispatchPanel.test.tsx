import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor, within } from "@testing-library/react";
import { DispatchPanel } from "./DispatchPanel";
import type { Seat, PlanTaskReview } from "../../lib/types";
import { DataSourceProvider } from "../../lib/datasource";

const generatePlan = vi.fn();
const getPlanGenStatus = vi.fn();
const getPlanReview = vi.fn();
const applyPlan = vi.fn();
const runOrchestrate = vi.fn();
vi.mock("../../lib/api", () => ({
  generatePlan: (...a: unknown[]) => generatePlan(...a),
  getPlanGenStatus: (...a: unknown[]) => getPlanGenStatus(...a),
  getPlanReview: (...a: unknown[]) => getPlanReview(...a),
  applyPlan: (...a: unknown[]) => applyPlan(...a),
  runOrchestrate: (...a: unknown[]) => runOrchestrate(...a),
}));

const roster: Seat[] = [
  { id: "claude", roles: ["orchestrator", "reviewer"], kind: "claude-code" },
  { id: "kimi", roles: ["worker"], kind: "kimi-cli" },
];

beforeEach(() => {
  generatePlan.mockReset(); getPlanGenStatus.mockReset(); getPlanReview.mockReset();
  applyPlan.mockReset(); runOrchestrate.mockReset();
});

function open() {
  return render(
    <DataSourceProvider>
      <DispatchPanel project="p1" roster={roster} open onClose={() => {}} onGoLive={() => {}} />
    </DataSourceProvider>,
  );
}

describe("DispatchPanel", () => {
  it("suggests a feature slug from the goal", () => {
    open();
    fireEvent.change(screen.getByTestId("dispatch-goal"), { target: { value: "Add 2FA Login" } });
    expect(screen.getByTestId("dispatch-feature")).toHaveValue("add-2fa-login");
  });

  it("disables Generate when roster is empty", () => {
    render(
      <DataSourceProvider>
        <DispatchPanel project="p1" roster={[]} open onClose={() => {}} onGoLive={() => {}} />
      </DataSourceProvider>,
    );
    fireEvent.change(screen.getByTestId("dispatch-goal"), { target: { value: "x" } });
    expect(screen.getByTestId("dispatch-generate")).toBeDisabled();
  });

  it("runs the full flow: generate → poll done → review → dispatch", async () => {
    generatePlan.mockResolvedValue({ status_url: "/x", feature: "add-2fa-login" });
    getPlanGenStatus.mockResolvedValue({ state: "done", feature: "add-2fa-login" });
    getPlanReview.mockResolvedValue({
      present: true, feature: "add-2fa-login", valid: true,
      tasks: [{ id: "add-2fa-otp", owner: "kimi", reviewer: "claude", spec: ".pact/x.md", verify: "go test" }],
    });
    applyPlan.mockResolvedValue({ assigned: 1 });
    runOrchestrate.mockResolvedValue({ status_url: "/y" });
    open();
    fireEvent.change(screen.getByTestId("dispatch-goal"), { target: { value: "Add 2FA Login" } });
    fireEvent.click(screen.getByTestId("dispatch-generate"));
    await waitFor(() => expect(screen.getByTestId("dispatch-review")).toBeInTheDocument());
    expect(screen.getByText("add-2fa-otp")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("dispatch-confirm"));
    await waitFor(() => expect(applyPlan).toHaveBeenCalledWith("p1", "add-2fa-login"));
    expect(runOrchestrate).toHaveBeenCalledWith("p1", { feature: "add-2fa-login" });
    await waitFor(() => expect(screen.getByTestId("dispatch-done")).toBeInTheDocument());
  });

  it("blocks dispatch on an invalid plan", async () => {
    generatePlan.mockResolvedValue({ status_url: "/x", feature: "add-2fa" });
    getPlanGenStatus.mockResolvedValue({ state: "done", feature: "add-2fa" });
    getPlanReview.mockResolvedValue({ present: true, feature: "add-2fa", valid: false, error: "owner \"ghost\" not in roster", tasks: [] });
    open();
    fireEvent.change(screen.getByTestId("dispatch-goal"), { target: { value: "add 2fa" } });
    fireEvent.click(screen.getByTestId("dispatch-generate"));
    await waitFor(() => expect(screen.getByTestId("dispatch-review")).toBeInTheDocument());
    expect(screen.getByTestId("dispatch-confirm")).toBeDisabled();
    expect(screen.getByText(/not in roster/)).toBeInTheDocument();
  });

  it("surfaces a generation error", async () => {
    generatePlan.mockResolvedValue({ status_url: "/x", feature: "add-2fa" });
    getPlanGenStatus.mockResolvedValue({ state: "error", feature: "add-2fa", error: "planner failed" });
    open();
    fireEvent.change(screen.getByTestId("dispatch-goal"), { target: { value: "add 2fa" } });
    fireEvent.click(screen.getByTestId("dispatch-generate"));
    await waitFor(() => expect(screen.getByTestId("dispatch-error")).toHaveTextContent("planner failed"));
  });
});

describe("DispatchPanel — hosted (relay) one-shot", () => {
  it("generates and confirms without polling status, review, or apply", async () => {
    const generatePlanFn = vi.fn().mockResolvedValue({ status_url: "", feature: "add-2fa-login" });
    const getPlanReviewFn = vi.fn();
    const applyPlanFn = vi.fn();
    const runOrchestrateFn = vi.fn();
    // A relay-style source: canOrchestrate=true but NO getPlanGenStatus/getPlanReview.
    const hostedSrc = {
      capabilities: { canWrite: true, canOrchestrate: true, multiMachine: true, cockpit: false },
      listProjects: vi.fn(),
      getState: vi.fn(),
      getStats: vi.fn(),
      subscribe: vi.fn(),
      generatePlan: generatePlanFn,
      applyPlan: applyPlanFn,
      runOrchestrate: runOrchestrateFn,
      getPlanReview: getPlanReviewFn,
    };
    render(
      <DataSourceProvider source={hostedSrc}>
        <DispatchPanel project="p1" roster={roster} open onClose={() => {}} onGoLive={() => {}} />
      </DataSourceProvider>,
    );
    fireEvent.change(screen.getByTestId("dispatch-goal"), { target: { value: "Add 2FA Login" } });
    fireEvent.click(screen.getByTestId("dispatch-generate"));
    await waitFor(() => expect(screen.getByTestId("dispatch-done")).toBeInTheDocument());
    expect(generatePlanFn).toHaveBeenCalledWith("p1", { goal: "Add 2FA Login", feature: "add-2fa-login" });
    expect(screen.getByTestId("dispatch-done")).toHaveTextContent("Watch the board");
    // One-shot: no review step, no separate apply, no explicit orchestrate call.
    expect(getPlanReviewFn).not.toHaveBeenCalled();
    expect(applyPlanFn).not.toHaveBeenCalled();
    expect(runOrchestrateFn).not.toHaveBeenCalled();
    expect(screen.queryByTestId("dispatch-review")).not.toBeInTheDocument();
  });
});

describe("DispatchPanel — capability gating", () => {
  it("disables Generate and Dispatch when the source is read-only", () => {
    const readOnly = {
      capabilities: { canWrite: false, canOrchestrate: false, multiMachine: true, cockpit: false },
      listProjects: vi.fn(),
      getState: vi.fn(),
      getStats: vi.fn(),
      subscribe: vi.fn(),
      generatePlan: vi.fn(),
      getPlanGenStatus: vi.fn(),
      getPlanReview: vi.fn().mockResolvedValue({
        present: true, feature: "add-2fa", valid: true,
        tasks: [{ id: "t1", owner: "kimi", reviewer: "claude", spec: "", verify: "" }],
      }),
      applyPlan: vi.fn(),
      runOrchestrate: vi.fn(),
    };
    render(
      <DataSourceProvider source={readOnly}>
        <DispatchPanel project="p1" roster={roster} open onClose={() => {}} onGoLive={() => {}} />
      </DataSourceProvider>,
    );
    fireEvent.change(screen.getByTestId("dispatch-goal"), { target: { value: "add 2fa" } });
    expect(screen.getByTestId("dispatch-generate")).toBeDisabled();
    expect(screen.getByTestId("dispatch-generate")).toHaveAttribute("title", "Remote control needs U3");
  });
});

// tierui-render: exec tiering in plan review. Design constraints under test:
// same muted color for all four tiers (text only), NO TIER is the only warn,
// badge sits in a FIXED-width slot as the row's first element (scannable
// column — never floating with id length), dimension·role is a muted text
// line (not badges) omitted entirely when both are empty.
describe("DispatchPanel — tier rendering (exec-tiering-ui)", () => {
  const task = (over: Partial<PlanTaskReview>): PlanTaskReview => ({
    id: "t1", owner: "kimi", reviewer: "claude", spec: ".pact/tasks/t1.md", verify: "", ...over,
  });

  async function openReview(tasks: PlanTaskReview[]) {
    generatePlan.mockResolvedValue({ status_url: "/x", feature: "f1" });
    getPlanGenStatus.mockResolvedValue({ state: "done", feature: "f1" });
    getPlanReview.mockResolvedValue({ present: true, feature: "f1", valid: true, tasks });
    open();
    fireEvent.change(screen.getByTestId("dispatch-goal"), { target: { value: "goal" } });
    fireEvent.click(screen.getByTestId("dispatch-generate"));
    await waitFor(() => expect(screen.getByTestId("dispatch-review")).toBeInTheDocument());
  }

  it("renders all four tiers + NO TIER, all muted except the NO TIER warn", async () => {
    await openReview([
      task({ id: "a", tier: "L0" }),
      task({ id: "b", tier: "L1" }),
      task({ id: "c", tier: "L2" }),
      task({ id: "d", tier: "L3" }),
      task({ id: "e", tier: "L1", tier_missing: true }),
    ]);
    const badges = screen.getAllByTestId("tier-badge");
    expect(badges.map((b) => b.textContent)).toEqual(["L0", "L1", "L2", "L3", "NO TIER"]);
    const noTier = badges[4];
    expect(noTier).toHaveAttribute("role", "img");
    expect(noTier).toHaveAttribute("aria-label", "未标注 tier —— 引擎将按 L1 运行");
    // warn for the anomaly; the four healthy tiers all use the muted text-2 token.
    expect(noTier.getAttribute("style")).toContain("var(--color-warn)");
    // Tier rows sit in the fixed 34px slot; the wider NO TIER row's slot grows
    // (w-auto) so the badge never overlaps the id — the L-column is unaffected.
    for (const b of badges.slice(0, 4)) {
      expect(b.getAttribute("style")).toContain("var(--color-text-2)");
      expect(b).not.toHaveAttribute("role");
      expect(b.parentElement).toHaveClass("w-[34px]", "shrink-0");
    }
    expect(noTier.parentElement).toHaveClass("w-auto", "shrink-0");
  });

  it("keeps the badge's fixed-width slot regardless of id length (scannable column)", async () => {
    await openReview([
      task({ id: "x", tier: "L1" }),
      task({ id: "a-very-long-task-id-that-would-push-a-floating-badge-way-out", tier: "L3" }),
    ]);
    for (const badge of screen.getAllByTestId("tier-badge")) {
      expect(badge.parentElement).toHaveClass("w-[34px]", "shrink-0");
    }
    const rows = screen.getAllByTestId("tier-badge").map((b) => b.closest("li")!);
    for (const row of rows) {
      const firstLine = row.querySelector("div")!;
      // Badge slot is the first element of the row's first line; id truncates.
      expect(firstLine.firstElementChild).toHaveClass("w-[34px]", "shrink-0");
      expect(firstLine.lastElementChild).toHaveClass("min-w-0", "truncate");
    }
  });

  it("shows a visible legend (badge titles are mouse-only)", async () => {
    await openReview([task({ tier: "L1" })]);
    expect(screen.getByTestId("tier-legend")).toHaveTextContent("L0 便宜 · L1 默认 · L2 复杂 · L3 高风险");
  });

  it("every tier badge carries a title; a conflict note wins over the tier meaning", async () => {
    await openReview([
      task({ id: "plain", tier: "L2" }),
      task({ id: "conflict", tier: "L0", tier_conflict: "manifest says L3, spec file says L0 — the engine will use L0" }),
    ]);
    const badges = screen.getAllByTestId("tier-badge");
    expect(badges[0].getAttribute("title")).toContain("L2");
    expect(badges[1]).toHaveAttribute("title", "manifest says L3, spec file says L0 — the engine will use L0");
  });

  it("a conflict note is ALSO the accessible name (badges are tabIndex=-1 — title alone is mouse-only)", async () => {
    await openReview([
      task({ id: "plain", tier: "L2" }),
      task({ id: "conflict", tier: "L0", tier_conflict: "manifest says L3, spec file says L0 — the engine will use L0" }),
    ]);
    const badges = screen.getAllByTestId("tier-badge");
    expect(badges[1]).toHaveAttribute("role", "img");
    expect(badges[1]).toHaveAttribute("aria-label", "manifest says L3, spec file says L0 — the engine will use L0");
    // No conflict → no role/aria-label tacked on a plain badge.
    expect(badges[0]).not.toHaveAttribute("role");
    expect(badges[0]).not.toHaveAttribute("aria-label");
  });

  it("an unrecognized spec tier (tier_raw) is named in the badge title — not byte-identical to explicit L1", async () => {
    await openReview([
      task({ id: "explicit", tier: "L1" }),
      task({ id: "typo", tier: "L1", tier_raw: "L9" }),
    ]);
    const badges = screen.getAllByTestId("tier-badge");
    expect(badges[1]).toHaveAttribute("title", 'spec 写的是 "L9"，无法识别 —— 引擎将按 L1 运行');
    expect(badges[0].getAttribute("title")).not.toContain("无法识别");
  });

  it("renders verify as scope evidence (truncated, full text in title)", async () => {
    await openReview([task({ tier: "L1", verify: "go test ./internal/foo/ -run TestBar" })]);
    const line = screen.getByText("verify: go test ./internal/foo/ -run TestBar");
    expect(line).toHaveClass("truncate");
    expect(line).toHaveAttribute("title", "go test ./internal/foo/ -run TestBar");
  });

  it("renders dimension · role as one muted text line; omits the line (no stray ·) when both are empty", async () => {
    await openReview([
      task({ id: "labeled", tier: "L1", dimension: "correctness", role: "frontend" }),
      task({ id: "bare", tier: "L1" }),
    ]);
    expect(screen.getByText("correctness · frontend")).toBeInTheDocument();
    const bareRow = screen.getByText("bare").closest("li")!;
    expect(within(bareRow).queryByText(/·/)).toBeNull();
  });
});
