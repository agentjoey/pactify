import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

const getSetupSuggest = vi.fn();
vi.mock("../lib/api", () => ({
  getSetupSuggest: (...args: unknown[]) => getSetupSuggest(...args),
}));

import { Setup } from "./Setup";

describe("Setup view", () => {
  beforeEach(() => getSetupSuggest.mockReset());

  it("prompts to register agents when the roster is empty", async () => {
    getSetupSuggest.mockResolvedValue({ bindings: [], warnings: [] });
    render(<Setup />);
    await waitFor(() => {
      expect(screen.getByText("还没有注册的 agent")).toBeTruthy();
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
    // toggle the only worker off → a "缺 worker" warning appears
    fireEvent.click(screen.getByTestId("role-opencode-worker"));
    await waitFor(() => {
      expect(screen.getByText(/缺 worker 座席/)).toBeTruthy();
    });
    // the apply command for opencode now has empty roles (no worker)
    const cmds = screen.getByTestId("setup-commands").textContent ?? "";
    expect(cmds).toContain("pactify agent add opencode --id opencode --roles ");
  });
});
