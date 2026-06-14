import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

const getAgents = vi.fn();
const registerAgent = vi.fn();
const unregisterAgent = vi.fn();
vi.mock("../lib/api", () => ({
  getAgents: (...args: unknown[]) => getAgents(...args),
  registerAgent: (...args: unknown[]) => registerAgent(...args),
  unregisterAgent: (...args: unknown[]) => unregisterAgent(...args),
}));

import { Agents } from "./Agents";

describe("Agents panel", () => {
  beforeEach(() => {
    getAgents.mockReset();
    registerAgent.mockReset();
    unregisterAgent.mockReset();
  });

  it("renders onboarding when no agents registered", async () => {
    getAgents.mockResolvedValue([
      { kind: "opencode", installed: true, detail: "/usr/local/bin/opencode", registered: false },
      { kind: "gemini", installed: true, detail: "/usr/local/bin/gemini", registered: false },
    ]);
    render(<Agents author={true} onChanged={() => {}} />);
    await waitFor(() => {
      expect(screen.getByText("扫描到这些 agent，选择注册以开始")).toBeTruthy();
    });
    expect(screen.getByText("opencode")).toBeTruthy();
    expect(screen.getByText("gemini")).toBeTruthy();
  });

  it("hides entirely once any agent is registered (onboarding-only bar)", async () => {
    // The global bar is a first-run nudge; with an agent already registered,
    // management lives in the Setup view + Ops panel, so the bar renders nothing.
    getAgents.mockResolvedValue([
      { kind: "opencode", installed: true, detail: "/usr/local/bin/opencode", registered: true, label: "My Code" },
      { kind: "gemini", installed: false, detail: "not found", registered: false },
    ]);
    const { container } = render(<Agents author={true} onChanged={() => {}} />);
    await waitFor(() => {
      expect(getAgents).toHaveBeenCalled();
    });
    expect(screen.queryByText("扫描到这些 agent，选择注册以开始")).toBeNull();
    expect(container).toBeEmptyDOMElement();
  });

  it("clicking register calls registerAgent and the onboarding bar then disappears", async () => {
    getAgents.mockResolvedValue([
      { kind: "opencode", installed: true, detail: "/usr/local/bin/opencode", registered: false },
    ]);
    registerAgent.mockResolvedValue(undefined);
    render(<Agents author={true} onChanged={() => {}} />);
    await waitFor(() => {
      expect(screen.getByText("注册")).toBeTruthy();
    });
    fireEvent.click(screen.getByText("注册"));
    expect(registerAgent).toHaveBeenCalledWith("opencode");
    // optimistic toggle → hasRegistered → the onboarding bar removes itself.
    await waitFor(() => {
      expect(screen.queryByText("扫描到这些 agent，选择注册以开始")).toBeNull();
    });
  });

  it("not-installed rows are grayed and toggle disabled", async () => {
    getAgents.mockResolvedValue([
      { kind: "gemini", installed: false, detail: "not found", registered: false },
    ]);
    render(<Agents author={true} onChanged={() => {}} />);
    await waitFor(() => {
      expect(screen.getByText("gemini")).toBeTruthy();
    });
    const btn = screen.getByRole("button", { name: "注册" }) as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
  });

  it("observe mode disables toggle for installed agents", async () => {
    getAgents.mockResolvedValue([
      { kind: "opencode", installed: true, detail: "/usr/local/bin/opencode", registered: false },
    ]);
    render(<Agents author={false} onChanged={() => {}} />);
    await waitFor(() => {
      expect(screen.getByText("注册")).toBeTruthy();
    });
    const btn = screen.getByRole("button", { name: "注册" }) as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
  });

  it("handles fetch error gracefully", async () => {
    getAgents.mockRejectedValue(new Error("network"));
    render(<Agents author={true} onChanged={() => {}} />);
    await waitFor(() => {
      expect(screen.queryByText("扫描到这些 agent")).toBeFalsy();
    });
  });
});
