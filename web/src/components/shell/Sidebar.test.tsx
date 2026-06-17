import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";

const deleteRegistry = vi.fn();
vi.mock("../../lib/api", () => ({
  deleteRegistry: (...args: unknown[]) => deleteRegistry(...args),
}));
vi.mock("./AddProjectWizard", () => ({
  AddProjectWizard: ({ open }: { open: boolean }) => open ? <div data-testid="add-project-wizard" /> : null,
}));

import { Sidebar } from "./Sidebar";

const PROJECTS = [
  { id: "a", name: "alpha", path: "/x/a", project: "a", feature_count: 0, awaiting_count: 0 },
  { id: "b", name: "beta", path: "/x/b", project: "b", feature_count: 0, awaiting_count: 0, group: "g1" },
  { id: "c", name: "gamma", path: "/x/c", project: "c", feature_count: 0, awaiting_count: 0, group: "g1" },
];

describe("Sidebar", () => {
  beforeEach(() => deleteRegistry.mockReset());

  it("renders ungrouped projects flat", () => {
    render(<Sidebar projects={[PROJECTS[0]]} current="" onSelect={() => {}} view="canvas" onView={() => {}} collapsed={false} onToggleCollapse={() => {}} />);
    expect(screen.getByTestId("sidebar-project-a")).toBeTruthy();
  });

  it("groups projects under expandable nodes", () => {
    render(<Sidebar projects={PROJECTS} current="" onSelect={() => {}} view="canvas" onView={() => {}} collapsed={false} onToggleCollapse={() => {}} />);
    expect(screen.getByTestId("sidebar-group-g1")).toBeTruthy();
    expect(screen.queryByTestId("sidebar-project-b")).toBeNull();
    fireEvent.click(screen.getByText("g1"));
    expect(screen.getByTestId("sidebar-project-b")).toBeTruthy();
    expect(screen.getByTestId("sidebar-project-c")).toBeTruthy();
  });

  it("opens the add-project wizard", () => {
    render(<Sidebar projects={PROJECTS} current="" onSelect={() => {}} view="canvas" onView={() => {}} collapsed={false} onToggleCollapse={() => {}} />);
    fireEvent.click(screen.getByTestId("sidebar-add-project"));
    expect(screen.getByTestId("add-project-wizard")).toBeTruthy();
  });

  it("calls onChanged after deleting a project", async () => {
    deleteRegistry.mockResolvedValue(undefined);
    const onChanged = vi.fn();
    render(<Sidebar projects={PROJECTS} current="" onSelect={() => {}} view="canvas" onView={() => {}} collapsed={false} onToggleCollapse={() => {}} onChanged={onChanged} />);
    const row = screen.getByTestId("sidebar-project-a");
    fireEvent.mouseEnter(row);
    fireEvent.click(screen.getByTestId("sidebar-delete-a"));
    await waitFor(() => expect(screen.getByTestId("delete-project-modal")).toBeTruthy());
    fireEvent.click(screen.getByTestId("delete-project-confirm"));
    await waitFor(() => expect(deleteRegistry).toHaveBeenCalledWith("a"));
    await waitFor(() => expect(onChanged).toHaveBeenCalled());
  });

  it("closes the delete modal on cancel", async () => {
    render(<Sidebar projects={PROJECTS} current="" onSelect={() => {}} view="canvas" onView={() => {}} collapsed={false} onToggleCollapse={() => {}} />);
    fireEvent.mouseEnter(screen.getByTestId("sidebar-project-a"));
    fireEvent.click(screen.getByTestId("sidebar-delete-a"));
    await waitFor(() => expect(screen.getByTestId("delete-project-modal")).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: /Cancel/i }));
    await waitFor(() => expect(screen.queryByTestId("delete-project-modal")).toBeNull());
  });
});
