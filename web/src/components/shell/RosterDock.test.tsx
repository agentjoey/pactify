import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { RosterDock } from "./RosterDock";
import type { Seat } from "../../lib/types";

const seats: Seat[] = [
  { id: "opencode", roles: ["worker"], kind: "opencode" },
  { id: "claude", roles: ["orchestrator", "reviewer"], kind: "claude-code" },
  { id: "kimi", roles: ["worker"], kind: "kimi-cli" },
];

describe("RosterDock", () => {
  it("renders a single card with seats grouped by role (one logo per seat)", () => {
    render(<RosterDock seats={seats} onSeatSettings={() => {}} />);
    expect(screen.getByTestId("roster-dock")).toBeInTheDocument();
    expect(screen.getByTestId("roster-role-orchestrator")).toBeInTheDocument();
    expect(screen.getByTestId("roster-role-worker")).toBeInTheDocument();
    expect(screen.getByTestId("roster-logo-claude")).toBeInTheDocument();
    expect(screen.getByTestId("roster-logo-opencode")).toBeInTheDocument();
    expect(screen.getByTestId("roster-logo-kimi")).toBeInTheDocument();
  });

  it("places a multi-role seat under its highest-priority role only", () => {
    render(<RosterDock seats={seats} onSeatSettings={() => {}} />);
    // claude (orchestrator+reviewer) shows once, in the orchestrator row.
    expect(screen.getByTestId("roster-role-orchestrator")).toContainElement(
      screen.getByTestId("roster-logo-claude"),
    );
    expect(screen.queryByTestId("roster-role-reviewer")).toBeNull();
  });

  it("orders role rows orchestrator-first", () => {
    render(<RosterDock seats={seats} onSeatSettings={() => {}} />);
    const rows = screen.getAllByTestId(/^roster-role-/);
    expect(rows[0]).toHaveAttribute("data-testid", "roster-role-orchestrator");
  });

  it("calls onSeatSettings with the seat id when its logo is clicked", () => {
    const onSeatSettings = vi.fn();
    render(<RosterDock seats={seats} onSeatSettings={onSeatSettings} />);
    fireEvent.click(screen.getByTestId("roster-logo-kimi"));
    expect(onSeatSettings).toHaveBeenCalledWith("kimi");
  });

  it("renders nothing when there are no seats", () => {
    const { container } = render(<RosterDock seats={[]} onSeatSettings={() => {}} />);
    expect(container.firstChild).toBeNull();
  });
});
