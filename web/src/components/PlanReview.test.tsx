import { render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

const getPlanReview = vi.fn();
vi.mock("../lib/api", () => ({
  getPlanReview: (...args: unknown[]) => getPlanReview(...args),
}));

import { PlanReview } from "./PlanReview";

describe("PlanReview panel", () => {
  beforeEach(() => getPlanReview.mockReset());

  it("prompts to generate a plan when none exists for the feature", async () => {
    getPlanReview.mockResolvedValue({ present: false, valid: false });
    render(<PlanReview project="p1" features={["relay"]} />);
    await waitFor(() => {
      expect(screen.getByText(/No plan for/)).toBeTruthy();
    });
    expect(getPlanReview).toHaveBeenCalledWith("p1", "relay");
  });

  it("renders the task graph with owner→reviewer, deps and verify", async () => {
    getPlanReview.mockResolvedValue({
      present: true,
      feature: "relay",
      branch: "feat-relay",
      valid: true,
      tasks: [
        { id: "t1", owner: "w", reviewer: "r", spec: ".pact/tasks/t1.md", verify: "go test ./..." },
        { id: "t2", owner: "w", reviewer: "r", spec: ".pact/tasks/t2.md", verify: "go test ./...", deps: ["t1"] },
      ],
    });
    render(<PlanReview project="p1" features={["relay"]} />);
    await waitFor(() => {
      expect(screen.getAllByTestId("plan-task")).toHaveLength(2);
    });
    expect(screen.getByText("valid")).toBeTruthy();
    expect(screen.getByText(/deps: t1/)).toBeTruthy();
    expect(screen.getByText(/pactify plan apply relay/)).toBeTruthy();
  });

  it("shows a validation warning for an invalid plan", async () => {
    getPlanReview.mockResolvedValue({
      present: true,
      feature: "relay",
      valid: false,
      error: "task t2 references unknown dep t9",
      tasks: [{ id: "t2", owner: "w", reviewer: "r", spec: "", verify: "", deps: ["t9"] }],
    });
    render(<PlanReview project="p1" features={["relay"]} />);
    await waitFor(() => {
      expect(screen.getByText("invalid")).toBeTruthy();
    });
    expect(screen.getByText(/references unknown dep/)).toBeTruthy();
  });

  it("shows an empty state when the project has no features", async () => {
    render(<PlanReview project="p1" features={[]} />);
    await waitFor(() => {
      expect(screen.getByText("No features yet")).toBeTruthy();
    });
    expect(getPlanReview).not.toHaveBeenCalled();
  });
});
