import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { CommandDock } from "./CommandDock";

describe("CommandDock", () => {
  it("calls onRun with the typed goal and selected concurrency", () => {
    const onRun = vi.fn();
    render(<CommandDock onRun={onRun} />);
    fireEvent.change(screen.getByTestId("command-dock-input"), { target: { value: "add 2fa" } });
    // pick concurrency 4 (third segment)
    fireEvent.click(screen.getAllByTestId("command-dock-conc")[2]);
    fireEvent.click(screen.getByTestId("command-dock-run"));
    expect(onRun).toHaveBeenCalledWith("add 2fa", { concurrency: 4 });
  });

  it("ignores Run with an empty goal", () => {
    const onRun = vi.fn();
    render(<CommandDock onRun={onRun} />);
    fireEvent.click(screen.getByTestId("command-dock-run"));
    expect(onRun).not.toHaveBeenCalled();
  });

  it("a recipe chip fills the goal input", () => {
    const onRun = vi.fn();
    render(<CommandDock onRun={onRun} />);
    fireEvent.click(screen.getAllByTestId("command-dock-recipe")[0]);
    expect((screen.getByTestId("command-dock-input") as HTMLInputElement).value).toContain("review-harden");
  });
});
