import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { Toolbar, type ToolbarProps } from "./Toolbar";

function props(over: Partial<ToolbarProps> = {}): ToolbarProps {
  return {
    author: true,
    comms: false,
    onToggleComms: vi.fn(),
    newFeatureOpen: false,
    onOpenNewFeature: vi.fn(),
    onCloseNewFeature: vi.fn(),
    nfId: "",
    setNfId: vi.fn(),
    nfBranch: "",
    setNfBranch: vi.fn(),
    nfValid: false,
    nfIdBad: false,
    nfBranchBad: false,
    onAddFeature: vi.fn(),
    onOpenNewTask: vi.fn(),
    newTaskDisabled: false,
    ...over,
  };
}

describe("canvas Toolbar (T7)", () => {
  it("comms pill toggles (testid comms-toggle, aria-pressed)", () => {
    const onToggleComms = vi.fn();
    const { rerender } = render(<Toolbar {...props({ onToggleComms })} />);
    const pill = screen.getByTestId("comms-toggle");
    expect(pill.getAttribute("aria-pressed")).toBe("false");
    fireEvent.click(pill);
    expect(onToggleComms).toHaveBeenCalled();
    rerender(<Toolbar {...props({ onToggleComms, comms: true })} />);
    expect(screen.getByTestId("comms-toggle").getAttribute("aria-pressed")).toBe("true");
  });

  it("author sees Feature + Task buttons; non-author sees only the comms pill", () => {
    const { rerender } = render(<Toolbar {...props()} />);
    expect(screen.getByRole("button", { name: /Feature/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^.?Task$/ })).toBeInTheDocument();

    rerender(<Toolbar {...props({ author: false })} />);
    expect(screen.queryByRole("button", { name: /Feature/ })).toBeNull();
    expect(screen.getByTestId("comms-toggle")).toBeInTheDocument();
  });

  it("Feature opens the inline new-feature form; Add is gated by nfValid", () => {
    const onOpenNewFeature = vi.fn();
    render(<Toolbar {...props({ onOpenNewFeature })} />);
    fireEvent.click(screen.getByRole("button", { name: /Feature/ }));
    expect(onOpenNewFeature).toHaveBeenCalled();

    // Open form, invalid → Add disabled.
    const { rerender } = render(<Toolbar {...props({ newFeatureOpen: true, nfValid: false })} />);
    expect(screen.getByText("Add")).toBeDisabled();
    rerender(<Toolbar {...props({ newFeatureOpen: true, nfValid: true })} />);
    expect(screen.getByText("Add")).not.toBeDisabled();
  });

  it("New task button is gated by newTaskDisabled", () => {
    const onOpenNewTask = vi.fn();
    render(<Toolbar {...props({ onOpenNewTask, newTaskDisabled: true })} />);
    const task = screen.getByRole("button", { name: /^.?Task$/ });
    expect(task).toBeDisabled();
  });

  // Task 4: Office mode reuses the Toolbar for authoring but hides the comms
  // lens pill (comms is a Plan-only display lens). showComms defaults true so
  // Plan-mode callers are unaffected; Office passes showComms={false}.
  it("showComms=false hides the comms pill (Office authoring chrome)", () => {
    const { rerender } = render(<Toolbar {...props({ showComms: false })} />);
    expect(screen.queryByTestId("comms-toggle")).toBeNull();
    // Feature/Task authoring buttons remain.
    expect(screen.getByRole("button", { name: /Feature/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^.?Task$/ })).toBeInTheDocument();
    // Default (omitted) → comms pill present.
    rerender(<Toolbar {...props()} />);
    expect(screen.getByTestId("comms-toggle")).toBeInTheDocument();
  });
});
