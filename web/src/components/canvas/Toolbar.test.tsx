import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { Toolbar, type ToolbarProps } from "./Toolbar";

function props(over: Partial<ToolbarProps> = {}): ToolbarProps {
  return {
    author: true,
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

describe("canvas Toolbar", () => {
  it("author sees Feature + Task buttons; non-author sees nothing", () => {
    const { rerender } = render(<Toolbar {...props()} />);
    expect(screen.getByRole("button", { name: /Feature/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^.?Task$/ })).toBeInTheDocument();

    rerender(<Toolbar {...props({ author: false })} />);
    expect(screen.queryByRole("button", { name: /Feature/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /^.?Task$/ })).toBeNull();
  });

  it("Feature opens the inline new-feature form; Add is gated by nfValid", () => {
    const onOpenNewFeature = vi.fn();
    render(<Toolbar {...props({ onOpenNewFeature })} />);
    fireEvent.click(screen.getByRole("button", { name: /Feature/ }));
    expect(onOpenNewFeature).toHaveBeenCalled();

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
});
