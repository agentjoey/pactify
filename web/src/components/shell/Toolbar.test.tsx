import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { Toolbar } from "./Toolbar";

describe("Toolbar dark chrome", () => {
  const baseProps = {
    projectName: "demo",
    live: false,
    author: true,
    seat: "kimi-worker",
    agents: [{ id: "kimi-worker", roles: ["worker"] }],
    projects: [],
    running: false,
    onSelectProject: () => {},
    onRenameProject: () => {},
    onDeleteProject: () => {},
    onAddProject: () => {},
    onOpenDispatch: () => {},
    onLensChange: () => {},
    lens: "board" as const,
  };

  it("applies handoff frosted bar styles", () => {
    render(<Toolbar {...baseProps} />);
    const bar = screen.getByTestId("toolbar");
    expect(bar.className).toContain("backdrop-blur-[12px]");
    expect(bar.className).toContain("bg-[rgba(12,17,25,0.82)]");
    expect(bar.className).toContain("min-h-[48px]");
    expect(bar.className).toContain("border-b");
  });

  it("renders 3-stroke wordmark + pactify label", () => {
    render(<Toolbar {...baseProps} />);
    expect(screen.getByText("pactify")).toBeInTheDocument();
    const svg = screen.getByTestId("toolbar").querySelector("svg");
    expect(svg).toBeTruthy();
    expect(svg?.querySelectorAll("path")).toHaveLength(3);
  });

  it("renders the lens segments with Board active by default", () => {
    render(<Toolbar {...baseProps} />);
    const group = screen.getByRole("group", { name: "lens" });
    expect(group).toBeInTheDocument();
    expect(screen.getByTestId("lens-dashboard")).toHaveAttribute("aria-pressed", "false");
    expect(screen.getByTestId("lens-board")).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByTestId("lens-cockpit")).toHaveAttribute("aria-pressed", "false");
  });

  it("calls onLensChange when a segment is clicked", () => {
    const onLensChange = vi.fn();
    render(<Toolbar {...baseProps} onLensChange={onLensChange} />);
    fireEvent.click(screen.getByTestId("lens-cockpit"));
    expect(onLensChange).toHaveBeenCalledWith("cockpit");
  });

  it("shows the Cockpit pending badge when count > 0", () => {
    render(<Toolbar {...baseProps} cockpitPending={3} />);
    const cockpit = screen.getByTestId("lens-cockpit");
    expect(cockpit).toHaveTextContent("3");
  });

  it("hides the Cockpit pending badge when count is 0", () => {
    render(<Toolbar {...baseProps} cockpitPending={0} />);
    const cockpit = screen.getByTestId("lens-cockpit");
    expect(cockpit).not.toHaveTextContent("0");
  });

  it("renders user avatar tile with monogram", () => {
    render(<Toolbar {...baseProps} />);
    const tile = screen.getByTestId("user-avatar-tile");
    expect(tile).toHaveTextContent("cl");
    expect(tile.style.background).toContain("linear-gradient");
  });

  it("uses the first two letters of the hosted email for the avatar tile", () => {
    render(<Toolbar {...baseProps} profileEmail="Ada@example.com" />);
    expect(screen.getByTestId("user-avatar-tile")).toHaveTextContent("ad");
  });
});
