import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, within } from "@testing-library/react";
import { ProjectMenu } from "./ProjectMenu";
import type { ProjectMeta } from "../../lib/types";

const projects: ProjectMeta[] = [
  { name: "alpha", path: "/a", group: "team-x" } as ProjectMeta,
  { name: "beta", path: "/b" } as ProjectMeta,
];

describe("ProjectMenu", () => {
  it("shows the current project name and a running status light when running", () => {
    render(
      <ProjectMenu projects={projects} current="alpha" running={true}
        onSelect={() => {}} onRename={() => {}} onDelete={() => {}} onAdd={() => {}} />,
    );
    expect(screen.getByTestId("project-menu-trigger")).toHaveTextContent("alpha");
    expect(screen.getByTestId("project-status-light")).toHaveAttribute("data-running", "true");
  });

  it("opens the menu and selects another project", () => {
    const onSelect = vi.fn();
    render(
      <ProjectMenu projects={projects} current="alpha" running={false}
        onSelect={onSelect} onRename={() => {}} onDelete={() => {}} onAdd={() => {}} />,
    );
    fireEvent.click(screen.getByTestId("project-menu-trigger"));
    fireEvent.click(screen.getByText("beta"));
    expect(onSelect).toHaveBeenCalledWith("beta");
  });

  it("groups projects but never prints the word 'ungrouped'", () => {
    render(
      <ProjectMenu projects={projects} current="alpha" running={false}
        onSelect={() => {}} onRename={() => {}} onDelete={() => {}} onAdd={() => {}} />,
    );
    fireEvent.click(screen.getByTestId("project-menu-trigger"));
    const menu = screen.getByTestId("project-menu");
    expect(within(menu).getByText("team-x")).toBeInTheDocument();
    expect(within(menu).queryByText(/ungrouped/i)).toBeNull();
  });

  it("shows a per-row status light reflecting each project's running state", () => {
    render(
      <ProjectMenu projects={projects} current="alpha" running={true}
        runningByProject={{ alpha: true, beta: false }}
        onSelect={() => {}} onRename={() => {}} onDelete={() => {}} onAdd={() => {}} />,
    );
    fireEvent.click(screen.getByTestId("project-menu-trigger"));
    expect(screen.getByTestId("project-row-light-alpha")).toHaveAttribute("data-running", "true");
    expect(screen.getByTestId("project-row-light-beta")).toHaveAttribute("data-running", "false");
  });

  it("invokes onAdd from the footer add-project item", () => {
    const onAdd = vi.fn();
    render(
      <ProjectMenu projects={projects} current="alpha" running={false}
        onSelect={() => {}} onRename={() => {}} onDelete={() => {}} onAdd={onAdd} />,
    );
    fireEvent.click(screen.getByTestId("project-menu-trigger"));
    fireEvent.click(screen.getByTestId("project-menu-add"));
    expect(onAdd).toHaveBeenCalled();
  });

  it("nests worktrees under a project when there is more than one", () => {
    const onSelectWorktree = vi.fn();
    render(
      <ProjectMenu projects={projects} current="alpha" running={false}
        worktreesByProject={{ alpha: [{ branch: "main", path: "/a", primary: true }, { branch: "feat-x", path: "/a-fe", primary: false }] }}
        currentWorktree="" onSelectWorktree={onSelectWorktree}
        onSelect={() => {}} onRename={() => {}} onDelete={() => {}} onAdd={() => {}} />,
    );
    fireEvent.click(screen.getByTestId("project-menu-trigger"));
    fireEvent.click(screen.getByTestId("worktree-alpha-feat-x"));
    expect(onSelectWorktree).toHaveBeenCalledWith("alpha", "feat-x");
  });
});
