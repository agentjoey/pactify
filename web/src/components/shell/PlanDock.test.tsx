import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { PlanDock } from "./PlanDock";

const getPlanReview = vi.fn();
vi.mock("../../lib/api", () => ({
  getPlanReview: (...a: unknown[]) => getPlanReview(...a),
}));

describe("PlanDock", () => {
  it("renders the task breakdown read-only (no Apply button)", async () => {
    getPlanReview.mockResolvedValue({
      present: true, feature: "add-2fa", branch: "feat-2fa", valid: true,
      tasks: [{ id: "t1", owner: "w", reviewer: "r", spec: ".pact/tasks/t1.md", verify: "go test ./..." }],
    });
    render(<PlanDock project="p1" features={["add-2fa"]} />);
    await waitFor(() => expect(screen.getByTestId("plan-dock")).toBeInTheDocument());
    expect(screen.getByText("t1")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /apply/i })).toBeNull();
  });

  it("collapses when the header toggle is clicked", async () => {
    getPlanReview.mockResolvedValue({ present: true, feature: "add-2fa", branch: "feat-2fa", valid: true, tasks: [] });
    render(<PlanDock project="p1" features={["add-2fa"]} />);
    await waitFor(() => expect(screen.getByTestId("plan-dock")).toBeInTheDocument());
    fireEvent.click(screen.getByTestId("plan-dock-toggle"));
    expect(screen.queryByTestId("plan-dock-body")).toBeNull();
  });

  it("shows an empty state when there are no features", () => {
    render(<PlanDock project="p1" features={[]} />);
    expect(screen.getByTestId("plan-dock-empty")).toBeInTheDocument();
  });

  // tierui-render: PlanDock is narrower than the 312px DispatchPanel — it shows
  // the tier badge ONLY (same fixed-slot rule), never verify/dimension/role.
  it("renders the tier badge in a fixed slot but no verify / dimension / role", async () => {
    getPlanReview.mockResolvedValue({
      present: true, feature: "add-2fa", branch: "feat-2fa", valid: true,
      tasks: [{
        id: "t1", owner: "w", reviewer: "r", spec: ".pact/tasks/t1.md", verify: "go test ./...",
        tier: "L2", dimension: "correctness", role: "frontend",
      }, {
        id: "t2", owner: "w", reviewer: "r", spec: ".pact/tasks/t2.md", verify: "go test ./...",
        tier: "L1", tier_missing: true,
      }],
    });
    render(<PlanDock project="p1" features={["add-2fa"]} />);
    await waitFor(() => expect(screen.getAllByTestId("plan-dock-task")).toHaveLength(2));
    const badges = screen.getAllByTestId("tier-badge");
    expect(badges.map((b) => b.textContent)).toEqual(["L2", "NO TIER"]);
    expect(badges[0].parentElement).toHaveClass("w-[34px]", "shrink-0");
    expect(badges[1].parentElement).toHaveClass("w-auto", "shrink-0");
    expect(screen.queryByText(/verify/i)).toBeNull();
    expect(screen.queryByText(/correctness/i)).toBeNull();
    expect(screen.queryByText(/frontend/i)).toBeNull();
  });
});
