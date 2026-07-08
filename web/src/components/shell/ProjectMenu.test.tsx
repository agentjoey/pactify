import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, within } from "@testing-library/react";
import { ProjectMenu } from "./ProjectMenu";
import type { ProjectMeta } from "../../lib/types";

const projects: ProjectMeta[] = [
  { id: "acct-alpha", name: "alpha", path: "/a", group: "team-x" } as ProjectMeta,
  { id: "acct-beta", name: "beta", path: "/b" } as ProjectMeta,
];

const mainWorktree = { branch: "main", path: "/a", primary: true };
const featWorktree = { branch: "feat-x", path: "/a-fe", primary: false };

describe("ProjectMenu", () => {
  it("shows the current project name and a running status light when running", () => {
    render(
      <ProjectMenu projects={projects} current="acct-alpha" running={true}
        onSelect={() => {}} onRename={() => {}} onDelete={() => {}} onAdd={() => {}} />,
    );
    expect(screen.getByTestId("project-menu-trigger")).toHaveTextContent("alpha");
    expect(screen.getByTestId("project-status-light")).toHaveAttribute("data-running", "true");
  });

  it("opens the menu and selects another project", () => {
    const onSelect = vi.fn();
    render(
      <ProjectMenu projects={projects} current="acct-alpha" running={false}
        onSelect={onSelect} onRename={() => {}} onDelete={() => {}} onAdd={() => {}} />,
    );
    fireEvent.click(screen.getByTestId("project-menu-trigger"));
    fireEvent.click(screen.getByText("beta"));
    expect(onSelect).toHaveBeenCalledWith("acct-beta");
  });

  it("groups projects but never prints the word 'ungrouped'", () => {
    render(
      <ProjectMenu projects={projects} current="acct-alpha" running={false}
        onSelect={() => {}} onRename={() => {}} onDelete={() => {}} onAdd={() => {}} />,
    );
    fireEvent.click(screen.getByTestId("project-menu-trigger"));
    const menu = screen.getByTestId("project-menu");
    expect(within(menu).getByText("team-x")).toBeInTheDocument();
    expect(within(menu).queryByText(/ungrouped/i)).toBeNull();
  });

  it("shows a per-row status light reflecting each project's running state", () => {
    render(
      <ProjectMenu projects={projects} current="acct-alpha" running={true}
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
      <ProjectMenu projects={projects} current="acct-alpha" running={false}
        onSelect={() => {}} onRename={() => {}} onDelete={() => {}} onAdd={onAdd} />,
    );
    fireEvent.click(screen.getByTestId("project-menu-trigger"));
    fireEvent.click(screen.getByTestId("project-menu-add"));
    expect(onAdd).toHaveBeenCalled();
  });

  it("collapses worktrees by default and toggles them", () => {
    const onSelectWorktree = vi.fn();
    render(
      <ProjectMenu projects={projects} current="acct-alpha" running={false}
        worktreesByProject={{ alpha: [mainWorktree, featWorktree] }}
        currentWorktree="" onSelectWorktree={onSelectWorktree}
        onSelect={() => {}} onRename={() => {}} onDelete={() => {}} onAdd={() => {}} />,
    );
    fireEvent.click(screen.getByTestId("project-menu-trigger"));

    const toggle = screen.getByTestId("worktree-toggle-alpha");
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    expect(toggle).toHaveTextContent("▸ 2");
    expect(screen.queryByTestId("worktree-alpha-feat-x")).toBeNull();

    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "true");
    expect(toggle).toHaveTextContent("▾ 2");
    expect(screen.getByTestId("worktree-alpha-feat-x")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("worktree-alpha-feat-x"));
    expect(onSelectWorktree).toHaveBeenCalledWith("acct-alpha", "feat-x");

    fireEvent.click(toggle);
    expect(screen.queryByTestId("worktree-alpha-feat-x")).toBeNull();
  });

  it("expands worktrees initially when currentWorktree matches a branch", () => {
    render(
      <ProjectMenu projects={projects} current="acct-alpha" running={false}
        worktreesByProject={{ alpha: [mainWorktree, featWorktree] }}
        currentWorktree="feat-x" onSelectWorktree={() => {}}
        onSelect={() => {}} onRename={() => {}} onDelete={() => {}} onAdd={() => {}} />,
    );
    fireEvent.click(screen.getByTestId("project-menu-trigger"));

    const toggle = screen.getByTestId("worktree-toggle-alpha");
    expect(toggle).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByTestId("worktree-alpha-feat-x")).toBeInTheDocument();
  });

  it("does not render a worktree toggle for a single-worktree project", () => {
    render(
      <ProjectMenu projects={projects} current="acct-alpha" running={false}
        worktreesByProject={{ alpha: [mainWorktree] }}
        currentWorktree="" onSelectWorktree={() => {}}
        onSelect={() => {}} onRename={() => {}} onDelete={() => {}} onAdd={() => {}} />,
    );
    fireEvent.click(screen.getByTestId("project-menu-trigger"));

    expect(screen.queryByTestId("worktree-toggle-alpha")).toBeNull();
    expect(screen.queryByTestId("worktree-alpha-main")).toBeNull();
  });
});
