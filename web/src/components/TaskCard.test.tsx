import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { TaskCard } from "./TaskCard";
import type { Task } from "../lib/types";

const task = (over: Partial<Task> = {}): Task => ({
  id: "t2-cli",
  owner: "bob",
  status: "in_progress",
  reviewer: "alice",
  spec: "",
  evidence: "",
  ...over,
});

// owner bob → worker, reviewer alice → orchestrator (resolved by the caller).
const roleProps = { ownerRoles: ["worker"], reviewerRoles: ["orchestrator"] };

describe("TaskCard — genome", () => {
  it("renders id (mono), title, and feature chip", () => {
    render(
      <TaskCard task={task()} featureId="greet-cli" title="CLI 入口" {...roleProps} />,
    );
    const card = screen.getByTestId("task-card");
    expect(card.textContent).toContain("t2-cli");
    expect(card.textContent).toContain("CLI 入口");
    expect(card.textContent).toContain("greet-cli");
  });

  it("falls back to the task id as title when no title given", () => {
    render(<TaskCard task={task()} {...roleProps} />);
    expect(screen.getByTestId("task-card").textContent).toContain("t2-cli");
  });

  // Each status has its own medallion glyph + drives --st.
  const cases: Array<[string, string]> = [
    ["assigned", "◇"],
    ["in_progress", "⚡"],
    ["awaiting_review", "◉"],
    ["changes_requested", "↺"],
    ["accepted", "✓"],
  ];
  for (const [status, glyph] of cases) {
    it(`status ${status} → medallion ${glyph}`, () => {
      render(<TaskCard task={task({ status })} {...roleProps} />);
      const med = screen.getByTestId("task-card-med");
      expect(med.textContent).toBe(glyph);
    });
  }

  it("draws 4 lifecycle segments with done/current per stage (in_progress → 1 done +1 cur)", () => {
    const { container } = render(<TaskCard task={task({ status: "in_progress" })} {...roleProps} />);
    const bars = container.querySelectorAll('[data-testid="task-card"] .life i');
    expect(bars.length).toBe(4);
    expect(bars[0].className).toContain("done");
    expect(bars[1].className).toContain("cur");
    expect(bars[2].className).not.toContain("done");
  });

  it("accepted → all four segments done", () => {
    const { container } = render(<TaskCard task={task({ status: "accepted" })} {...roleProps} />);
    const bars = container.querySelectorAll('[data-testid="task-card"] .life i');
    expect([...bars].every((b) => b.className.includes("done"))).toBe(true);
  });

  it("awaiting_review shows owner→reviewer ant chips", () => {
    const { container } = render(
      <TaskCard task={task({ status: "awaiting_review" })} {...roleProps} />,
    );
    // two ant svgs (owner + reviewer) in the bottom row
    const ants = container.querySelectorAll('[data-testid="task-card"] .task-card-bot svg');
    expect(ants.length).toBe(2);
  });

  it("in_progress (working) gets the blue focus glow", () => {
    render(<TaskCard task={task({ status: "in_progress" })} {...roleProps} />);
    expect(screen.getByTestId("task-card").className).toContain("glow");
  });

  it("non-review status shows only the owner ant (no reviewer chip)", () => {
    const { container } = render(<TaskCard task={task({ status: "in_progress" })} {...roleProps} />);
    const ants = container.querySelectorAll('[data-testid="task-card"] .task-card-bot svg');
    expect(ants.length).toBe(1);
  });

  it("draft variant gets the dashed class", () => {
    render(<TaskCard task={task({ status: "assigned" })} draft {...roleProps} />);
    expect(screen.getByTestId("task-card").className).toContain("dashed");
  });

  it("stale → amber dot", () => {
    render(<TaskCard task={task()} stale {...roleProps} />);
    expect(screen.getByTestId("task-card-stale")).toBeInTheDocument();
  });

  it("selected adds the selected ring", () => {
    render(<TaskCard task={task()} selected {...roleProps} />);
    expect(screen.getByTestId("task-card").className).toContain("selected");
  });

  it("onClick fires on card click", () => {
    const onClick = vi.fn();
    render(<TaskCard task={task()} onClick={onClick} {...roleProps} />);
    fireEvent.click(screen.getByTestId("task-card"));
    expect(onClick).toHaveBeenCalled();
  });
});
