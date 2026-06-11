import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { ContextMenu, type MenuTarget } from "./ContextMenu";

function setup(target: MenuTarget) {
  const handlers = {
    onClose: vi.fn(),
    onNewFeature: vi.fn(),
    onNewTask: vi.fn(),
    onDispatch: vi.fn(),
    onEdit: vi.fn(),
    onViewSpec: vi.fn(),
    onDelete: vi.fn(),
  };
  render(<ContextMenu target={target} {...handlers} />);
  return handlers;
}

describe("ContextMenu (T8)", () => {
  it("pane target → New feature / New task", () => {
    const h = setup({ kind: "pane", x: 10, y: 10, flow: { x: 0, y: 0 } });
    expect(screen.getByText("New feature")).toBeInTheDocument();
    expect(screen.getByText("New task")).toBeInTheDocument();
    expect(screen.queryByText("Dispatch")).toBeNull();
    fireEvent.click(screen.getByText("New feature"));
    expect(h.onNewFeature).toHaveBeenCalled();
    expect(h.onClose).toHaveBeenCalled();
  });

  it("draft node → Dispatch / Edit / Delete (drafts only)", () => {
    const h = setup({ kind: "node", x: 10, y: 10, id: "d1", draft: true });
    expect(screen.getByText("Dispatch")).toBeInTheDocument();
    expect(screen.getByText("Edit task")).toBeInTheDocument();
    expect(screen.getByText("Delete draft")).toBeInTheDocument();
    expect(screen.queryByText("View spec")).toBeNull();
    fireEvent.click(screen.getByText("Dispatch"));
    expect(h.onDispatch).toHaveBeenCalledWith("d1");
    fireEvent.click(screen.getByText("Delete draft"));
    expect(h.onDelete).toHaveBeenCalledWith("d1");
  });

  it("committed node → View spec only", () => {
    const h = setup({ kind: "node", x: 10, y: 10, id: "t2", draft: false });
    expect(screen.getByText("View spec")).toBeInTheDocument();
    expect(screen.queryByText("Dispatch")).toBeNull();
    expect(screen.queryByText("Delete draft")).toBeNull();
    fireEvent.click(screen.getByText("View spec"));
    expect(h.onViewSpec).toHaveBeenCalledWith("t2");
  });

  it("Esc closes the menu", () => {
    const h = setup({ kind: "pane", x: 0, y: 0, flow: { x: 0, y: 0 } });
    fireEvent.keyDown(window, { key: "Escape" });
    expect(h.onClose).toHaveBeenCalled();
  });

  it("outside pointerdown closes the menu", () => {
    const h = setup({ kind: "pane", x: 0, y: 0, flow: { x: 0, y: 0 } });
    fireEvent.pointerDown(document.body);
    expect(h.onClose).toHaveBeenCalled();
  });
});
