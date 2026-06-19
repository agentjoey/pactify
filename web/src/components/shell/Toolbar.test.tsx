import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { Toolbar } from "./Toolbar";

describe("Toolbar dark chrome", () => {
  it("applies handoff frosted bar styles", () => {
    render(
      <Toolbar
        projectName="demo"
        view="board"
        onView={() => {}}
        live={false}
        author={false}
        seat="kimi-worker"
        agents={[{ id: "kimi-worker", roles: ["worker"] }]}
        projects={[]}
        running={false}
        onSelectProject={() => {}}
        onRenameProject={() => {}}
        onDeleteProject={() => {}}
        onAddProject={() => {}}
        onOpenSettings={() => {}}
        onOpenDispatch={() => {}}
      />,
    );
    const bar = screen.getByTestId("toolbar");
    expect(bar.className).toContain("backdrop-blur-[12px]");
    expect(bar.className).toContain("bg-[color-mix(in_srgb,var(--color-bg-surface)_82%,var(--color-bg-page))]");
    expect(bar.className).toContain("min-h-[46px]");
  });

  it("uses raised fill for active lens segment", () => {
    render(
      <Toolbar
        projectName="demo"
        view="board"
        onView={() => {}}
        live={false}
        author={false}
        seat="kimi-worker"
        agents={[{ id: "kimi-worker", roles: ["worker"] }]}
        projects={[]}
        running={false}
        onSelectProject={() => {}}
        onRenameProject={() => {}}
        onDeleteProject={() => {}}
        onAddProject={() => {}}
        onOpenSettings={() => {}}
        onOpenDispatch={() => {}}
      />,
    );
    const active = screen.getByRole("button", { name: /Board/i });
    expect(active).toHaveAttribute("aria-pressed", "true");
    expect(active.style.background).toBe("var(--color-bg-raised)");
  });

  it("renders seat avatar with light inset ring", () => {
    render(
      <Toolbar
        projectName="demo"
        view="board"
        onView={() => {}}
        live={false}
        author
        seat="kimi-worker"
        agents={[{ id: "kimi-worker", roles: ["worker"] }]}
        projects={[]}
        running={false}
        onSelectProject={() => {}}
        onRenameProject={() => {}}
        onDeleteProject={() => {}}
        onAddProject={() => {}}
        onOpenSettings={() => {}}
        onOpenDispatch={() => {}}
      />,
    );
    const avatar = screen.getByTestId("seat-avatar");
    expect(avatar.style.boxShadow).toContain("rgba(255,255,255,.16)");
  });
});
