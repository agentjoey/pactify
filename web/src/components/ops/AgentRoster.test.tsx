import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

const getAgents = vi.fn();
const registerAgent = vi.fn();
const unregisterAgent = vi.fn();
const pruneSessions = vi.fn();
vi.mock("../../lib/api", () => ({
  getAgents: (...a: unknown[]) => getAgents(...a),
  registerAgent: (...a: unknown[]) => registerAgent(...a),
  unregisterAgent: (...a: unknown[]) => unregisterAgent(...a),
  pruneSessions: (...a: unknown[]) => pruneSessions(...a),
}));

import { AgentRoster } from "./AgentRoster";

describe("AgentRoster panel", () => {
  beforeEach(() => {
    getAgents.mockReset();
    registerAgent.mockReset();
    unregisterAgent.mockReset();
    pruneSessions.mockReset();
  });

  it("lists installed agents and registers an undetected-but-supported one", async () => {
    getAgents.mockResolvedValue([
      { kind: "opencode", installed: true, detail: "/usr/local/bin/opencode", registered: false },
      { kind: "kimi-cli", installed: false, detail: "not found", registered: false },
    ]);
    registerAgent.mockResolvedValue(undefined);
    render(<AgentRoster author={true} onChanged={() => {}} />);

    // installed row is shown directly with a Register action.
    await waitFor(() => expect(screen.getByTestId("roster-opencode")).toBeTruthy());
    expect(screen.getByText("1 installed on this machine")).toBeTruthy();
    // undetected kind is hidden behind the manual-add disclosure.
    expect(screen.queryByTestId("roster-kimi-cli")).toBeNull();

    fireEvent.click(screen.getByTestId("manual-add-toggle"));
    await waitFor(() => expect(screen.getByTestId("roster-kimi-cli")).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "Register anyway" }));
    expect(registerAgent).toHaveBeenCalledWith("kimi-cli");
  });

  it("Scan re-fetches the agent list", async () => {
    getAgents.mockResolvedValue([
      { kind: "opencode", installed: true, detail: "", registered: true, label: "My Code" },
    ]);
    render(<AgentRoster author={true} onChanged={() => {}} />);
    await waitFor(() => expect(getAgents).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByTestId("agent-scan"));
    await waitFor(() => expect(getAgents).toHaveBeenCalledTimes(2));
  });

  it("registers an installed-but-unregistered agent", async () => {
    getAgents.mockResolvedValue([
      { kind: "opencode", installed: true, detail: "/usr/local/bin/opencode", registered: false },
    ]);
    registerAgent.mockResolvedValue(undefined);
    render(<AgentRoster author={true} onChanged={() => {}} />);
    await waitFor(() => expect(screen.getByRole("button", { name: "Register" })).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "Register" }));
    expect(registerAgent).toHaveBeenCalledWith("opencode");
  });

  it("observe mode (author=false) disables register", async () => {
    getAgents.mockResolvedValue([
      { kind: "opencode", installed: true, detail: "", registered: false },
    ]);
    render(<AgentRoster author={false} onChanged={() => {}} />);
    await waitFor(() => expect(screen.getByRole("button", { name: "Register" })).toBeTruthy());
    const btn = screen.getByRole("button", { name: "Register" }) as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
  });

  it("shows an empty state when nothing is detected", async () => {
    getAgents.mockResolvedValue([]);
    render(<AgentRoster author={true} onChanged={() => {}} />);
    await waitFor(() => expect(screen.getByText("No supported agents detected")).toBeTruthy());
  });

  it("prunes sessions for an installed agent", async () => {
    getAgents.mockResolvedValue([
      { kind: "opencode", installed: true, detail: "/usr/local/bin/opencode", registered: true },
    ]);
    pruneSessions.mockResolvedValue({ kind: "opencode", skipped: false, output: "closed 3 sessions" });
    render(<AgentRoster author={true} onChanged={() => {}} />);

    await waitFor(() => expect(screen.getByTestId("roster-opencode")).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "Prune sessions" }));
    await waitFor(() => expect(pruneSessions).toHaveBeenCalledWith("opencode"));
    await waitFor(() => expect(screen.getByText("closed 3 sessions")).toBeTruthy());
  });

  it("shows 'Nothing to prune' when prune is skipped", async () => {
    getAgents.mockResolvedValue([
      { kind: "claude", installed: true, detail: "/usr/local/bin/claude", registered: true },
    ]);
    pruneSessions.mockResolvedValue({ kind: "claude", skipped: true, output: "" });
    render(<AgentRoster author={true} onChanged={() => {}} />);

    await waitFor(() => expect(screen.getByTestId("roster-claude")).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "Prune sessions" }));
    await waitFor(() => expect(screen.getByText("Nothing to prune")).toBeTruthy());
  });
});
