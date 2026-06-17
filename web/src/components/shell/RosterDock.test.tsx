import { describe, it, expect, vi } from "vitest";
import { render, screen, within, fireEvent } from "@testing-library/react";
import { RosterDock } from "./RosterDock";
import type { Seat } from "../../lib/types";

const seats: Seat[] = [
  { id: "opencode", roles: ["worker"], kind: "opencode" },
  { id: "claude", roles: ["orchestrator", "reviewer"], kind: "claude-code" },
  { id: "kimi", roles: ["worker"], kind: "kimi-cli" },
];

describe("RosterDock", () => {
  it("renders one card per seat with name + roles", () => {
    render(<RosterDock seats={seats} onSeatSettings={() => {}} />);
    const cards = screen.getAllByTestId("roster-card");
    expect(cards).toHaveLength(3);
    expect(within(cards[0]).getByText("claude")).toBeInTheDocument();
    expect(within(cards[0]).getByText(/orchestrator/)).toBeInTheDocument();
  });

  it("puts the orchestrator seat first regardless of input order", () => {
    render(<RosterDock seats={seats} onSeatSettings={() => {}} />);
    const cards = screen.getAllByTestId("roster-card");
    expect(within(cards[0]).getByText("claude")).toBeInTheDocument();
  });

  it("calls onSeatSettings with the seat id when its gear is clicked", () => {
    const onSeatSettings = vi.fn();
    render(<RosterDock seats={seats} onSeatSettings={onSeatSettings} />);
    const cards = screen.getAllByTestId("roster-card");
    fireEvent.click(within(cards[0]).getByTestId("roster-card-settings"));
    expect(onSeatSettings).toHaveBeenCalledWith("claude");
  });
});
