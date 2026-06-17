import { render, screen, waitFor, fireEvent, within } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

const getSetupSuggest = vi.fn();
const setupApply = vi.fn();
vi.mock("../lib/api", () => ({
  getSetupSuggest: (...args: unknown[]) => getSetupSuggest(...args),
  setupApply: (...args: unknown[]) => setupApply(...args),
}));

import { Setup } from "./Setup";

describe("Setup view", () => {
  beforeEach(() => {
    getSetupSuggest.mockReset();
    setupApply.mockReset();
  });

  it("prompts to register agents when the roster is empty", async () => {
    getSetupSuggest.mockResolvedValue({ bindings: [], warnings: [] });
    render(<Setup />);
    await waitFor(() => {
      expect(screen.getByText("No agents registered yet")).toBeTruthy();
    });
  });

  it("renders the roster and the exact apply commands", async () => {
    getSetupSuggest.mockResolvedValue({
      bindings: [
        { seat: "claude", kind: "claude-code", roles: ["orchestrator", "reviewer"], drivable: true },
        { seat: "opencode", kind: "opencode", roles: ["worker"], drivable: true },
      ],
      warnings: [],
    });
    render(<Setup />);
    await waitFor(() => {
      expect(screen.getAllByTestId("setup-row")).toHaveLength(2);
    });
    const cmds = screen.getByTestId("setup-commands").textContent ?? "";
    // init carries the seat triples with the right entry file per kind
    expect(cmds).toContain('--seat "claude:orchestrator,reviewer:CLAUDE.md:claude-code"');
    expect(cmds).toContain('--seat "opencode:worker:AGENTS.md:opencode"');
    expect(cmds).toContain("pactify agent add opencode --id opencode --roles worker");
  });

  it("recomputes warnings live when roles are toggled off", async () => {
    getSetupSuggest.mockResolvedValue({
      bindings: [
        { seat: "claude", kind: "claude-code", roles: ["orchestrator", "reviewer"], drivable: true },
        { seat: "opencode", kind: "opencode", roles: ["worker"], drivable: true },
      ],
      warnings: [],
    });
    render(<Setup />);
    await waitFor(() => expect(screen.getAllByTestId("setup-row")).toHaveLength(2));
    // toggle the only worker off → a "No worker seat" warning appears
    fireEvent.click(screen.getByTestId("role-opencode-worker"));
    await waitFor(() => {
      expect(screen.getByText(/No worker seat/)).toBeTruthy();
    });
    // the apply command for opencode now has empty roles (no worker)
    const cmds = screen.getByTestId("setup-commands").textContent ?? "";
    expect(cmds).toContain("pactify agent add opencode --id opencode --roles ");
  });

  it("keeps Apply disabled until the roster roles are complete", async () => {
    getSetupSuggest.mockResolvedValue({
      bindings: [
        { seat: "claude", kind: "claude-code", roles: ["orchestrator"], drivable: true },
        { seat: "opencode", kind: "opencode", roles: ["worker"], drivable: true },
      ],
      warnings: [],
    });
    render(<Setup />);
    await waitFor(() => expect(screen.getAllByTestId("setup-row")).toHaveLength(2));
    const applyBtn = screen.getByRole("button", { name: /Apply/i });
    expect(applyBtn).toBeDisabled();

    fireEvent.click(screen.getByTestId("role-claude-reviewer"));
    await waitFor(() => expect(applyBtn).not.toBeDisabled());
  });

  it("sends the expected body when Apply is clicked", async () => {
    getSetupSuggest.mockResolvedValue({
      bindings: [
        { seat: "claude", kind: "claude-code", roles: ["orchestrator", "reviewer"], drivable: true },
        { seat: "opencode", kind: "opencode", roles: ["worker"], drivable: true },
      ],
      warnings: [],
    });
    setupApply.mockResolvedValue({ inited: true, wired: [], notes: [] });
    render(<Setup />);
    await waitFor(() => expect(screen.getAllByTestId("setup-row")).toHaveLength(2));

    fireEvent.change(screen.getByTestId("setup-path"), { target: { value: "/tmp/new-project" } });
    await waitFor(() => {
      expect(screen.getByTestId<HTMLInputElement>("setup-project").value).toBe("new-project");
    });

    fireEvent.click(screen.getByRole("button", { name: /Apply/i }));
    await waitFor(() => {
      expect(setupApply).toHaveBeenCalledTimes(1);
    });
    expect(setupApply).toHaveBeenLastCalledWith({
      path: "/tmp/new-project",
      project: "new-project",
      seats: [
        { id: "claude", roles: ["orchestrator", "reviewer"], entry: "CLAUDE.md", kind: "claude-code" },
        { id: "opencode", roles: ["worker"], entry: "AGENTS.md", kind: "opencode" },
      ],
    });
  });

  it("renders wired results, docOnly snippets and notes", async () => {
    getSetupSuggest.mockResolvedValue({
      bindings: [
        { seat: "claude", kind: "claude-code", roles: ["orchestrator", "reviewer"], drivable: true },
        { seat: "codex", kind: "codex-cli", roles: ["worker"], drivable: true },
      ],
      warnings: [],
    });
    setupApply.mockResolvedValue({
      inited: true,
      wired: [
        { kind: "claude-code", seat: "claude", wrote: true, path: "CLAUDE.md", docOnly: false },
        { kind: "codex-cli", seat: "codex", wrote: false, path: "codex.toml", docOnly: true, snippet: "[agent]\nseat = codex" },
      ],
      notes: ["wire codex-cli (codex): not installed"],
    });
    render(<Setup />);
    await waitFor(() => expect(screen.getAllByTestId("setup-row")).toHaveLength(2));

    fireEvent.change(screen.getByTestId("setup-path"), { target: { value: "/tmp/proj" } });
    fireEvent.click(screen.getByRole("button", { name: /Apply/i }));

    await waitFor(() => {
      expect(screen.getByTestId("setup-result")).toBeTruthy();
    });
    expect(screen.getByText(/Project initialized/)).toBeTruthy();
    const result = screen.getByTestId("setup-result");
    expect(within(result).getByText(/CLAUDE.md/)).toBeTruthy();
    expect(within(result).getByText(/codex.toml/)).toBeTruthy();
    expect(within(result).getByText(/copy snippet/i)).toBeTruthy();
    expect(screen.getByText(/wire codex-cli/)).toBeTruthy();
  });

  it("renders an error when setupApply rejects", async () => {
    getSetupSuggest.mockResolvedValue({
      bindings: [
        { seat: "claude", kind: "claude-code", roles: ["orchestrator", "reviewer"], drivable: true },
        { seat: "opencode", kind: "opencode", roles: ["worker"], drivable: true },
      ],
      warnings: [],
    });
    setupApply.mockRejectedValue(new Error("project already initialized (.pact exists)"));
    render(<Setup />);
    await waitFor(() => expect(screen.getAllByTestId("setup-row")).toHaveLength(2));

    fireEvent.change(screen.getByTestId("setup-path"), { target: { value: "/tmp/existing" } });
    fireEvent.click(screen.getByRole("button", { name: /Apply/i }));

    await waitFor(() => {
      expect(screen.getByText(/project already initialized/)).toBeTruthy();
    });
  });
});
