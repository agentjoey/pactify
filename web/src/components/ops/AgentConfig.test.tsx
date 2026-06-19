import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

const getAgents = vi.fn();
const getAgentConfig = vi.fn();
const setAgentConfig = vi.fn();
vi.mock("../../lib/api", () => ({
  getAgents: (...a: unknown[]) => getAgents(...a),
  getAgentConfig: (...a: unknown[]) => getAgentConfig(...a),
  setAgentConfig: (...a: unknown[]) => setAgentConfig(...a),
}));

import { AgentConfig } from "./AgentConfig";

const cfg = (over: Record<string, unknown> = {}) => ({
  kind: "opencode",
  registered: true,
  drivable: true,
  model: "",
  allowed_tools: null,
  restricted: false,
  effective_model: "deepseek/deepseek-v4-pro",
  effective_scoped: false,
  ...over,
});

describe("AgentConfig panel", () => {
  beforeEach(() => {
    getAgents.mockReset();
    getAgentConfig.mockReset();
    setAgentConfig.mockReset();
  });

  it("only lists registered agents and shows their effective model", async () => {
    getAgents.mockResolvedValue([
      { kind: "opencode", installed: true, detail: "", registered: true },
      { kind: "gemini-cli", installed: true, detail: "", registered: false },
    ]);
    getAgentConfig.mockResolvedValue(cfg());
    render(<AgentConfig />);
    await waitFor(() => {
      expect(screen.getByTestId("agent-config-opencode")).toBeTruthy();
    });
    expect(screen.queryByTestId("agent-config-gemini-cli")).toBeNull();
    // effective model comes from the row's own async getAgentConfig — wait for it
    // (was a flaky sync assertion racing the fetch).
    await waitFor(() => {
      expect(screen.getByText(/deepseek\/deepseek-v4-pro/)).toBeTruthy();
    });
  });

  it("saves model + scoped posture with allowed tools", async () => {
    getAgents.mockResolvedValue([{ kind: "opencode", installed: true, detail: "", registered: true }]);
    getAgentConfig.mockResolvedValue(cfg());
    setAgentConfig.mockResolvedValue(cfg({ model: "deepseek/custom", restricted: true, allowed_tools: ["Read", "Edit"], effective_model: "deepseek/custom", effective_scoped: true }));
    render(<AgentConfig />);
    await waitFor(() => expect(screen.getByTestId("model-opencode")).toBeTruthy());

    fireEvent.change(screen.getByTestId("model-opencode"), { target: { value: "deepseek/custom" } });
    fireEvent.click(screen.getByTestId("scoped-opencode")); // blanket → scoped
    await waitFor(() => expect(screen.getByTestId("tools-opencode")).toBeTruthy());
    fireEvent.change(screen.getByTestId("tools-opencode"), { target: { value: "Read, Edit" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(setAgentConfig).toHaveBeenCalledWith("opencode", {
        model: "deepseek/custom",
        restricted: true,
        allowed_tools: ["Read", "Edit"],
      });
    });
  });

  it("renders a model dropdown from candidate_models and saves the picked model", async () => {
    getAgents.mockResolvedValue([{ kind: "claude-code", installed: true, detail: "", registered: true }]);
    getAgentConfig.mockResolvedValue(
      cfg({
        kind: "claude-code",
        model: "",
        effective_model: "claude-opus-4-8",
        candidate_models: ["claude-opus-4-8", "claude-sonnet-4-6", "claude-haiku-4-5"],
      }),
    );
    setAgentConfig.mockResolvedValue(
      cfg({ kind: "claude-code", model: "claude-sonnet-4-6", candidate_models: ["claude-opus-4-8", "claude-sonnet-4-6", "claude-haiku-4-5"] }),
    );
    render(<AgentConfig />);

    // dropdown is shown (not the free-text field) when candidates exist.
    await waitFor(() => expect(screen.getByTestId("model-select-claude-code")).toBeTruthy());
    expect(screen.queryByTestId("model-claude-code")).toBeNull();
    expect(screen.getByRole("option", { name: "claude-sonnet-4-6" })).toBeTruthy();

    fireEvent.change(screen.getByTestId("model-select-claude-code"), { target: { value: "claude-sonnet-4-6" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => {
      expect(setAgentConfig).toHaveBeenCalledWith("claude-code", {
        model: "claude-sonnet-4-6",
        restricted: false,
        allowed_tools: [],
      });
    });
  });

  it("reveals a free-text field when the model dropdown switches to custom…", async () => {
    getAgents.mockResolvedValue([{ kind: "claude-code", installed: true, detail: "", registered: true }]);
    getAgentConfig.mockResolvedValue(
      cfg({ kind: "claude-code", model: "", candidate_models: ["claude-opus-4-8", "claude-sonnet-4-6"] }),
    );
    render(<AgentConfig />);
    await waitFor(() => expect(screen.getByTestId("model-select-claude-code")).toBeTruthy());
    expect(screen.queryByTestId("model-claude-code")).toBeNull();
    fireEvent.change(screen.getByTestId("model-select-claude-code"), { target: { value: "__custom__" } });
    await waitFor(() => expect(screen.getByTestId("model-claude-code")).toBeTruthy());
  });

  it("shows an empty state when no agents are registered", async () => {
    getAgents.mockResolvedValue([{ kind: "opencode", installed: true, detail: "", registered: false }]);
    render(<AgentConfig />);
    await waitFor(() => {
      expect(screen.getByText("No agents registered")).toBeTruthy();
    });
  });

  it("renders the machine scope banner", async () => {
    getAgents.mockResolvedValue([]);
    render(<AgentConfig />);
    await waitFor(() => {
      expect(screen.getByTestId("agent-config-scope-banner")).toHaveTextContent("MACHINE · all projects");
    });
  });

  it("renders Blanket/Scoped segmented posture and allowed-tools chips", async () => {
    getAgents.mockResolvedValue([{ kind: "claude-code", installed: true, detail: "", registered: true }]);
    getAgentConfig.mockResolvedValue(
      cfg({ kind: "claude-code", restricted: true, allowed_tools: ["Read", "Edit", "Bash"] }),
    );
    render(<AgentConfig />);
    await waitFor(() => expect(screen.getByTestId("posture-scoped-claude-code")).toBeTruthy());
    expect(screen.getByTestId("posture-blanket-claude-code")).toBeTruthy();
    expect(screen.getByTestId("posture-scoped-claude-code")).toHaveAttribute("aria-pressed", "true");
    expect(screen.getAllByTestId("allowed-tool-chip").length).toBe(3);
  });

  it("dims manual (non-drivable) agents", async () => {
    getAgents.mockResolvedValue([{ kind: "antigravity", installed: true, detail: "", registered: true }]);
    getAgentConfig.mockResolvedValue(cfg({ kind: "antigravity", drivable: false, model: "", restricted: false }));
    render(<AgentConfig />);
    await waitFor(() => expect(screen.getByText("manual")).toBeTruthy());
    expect(screen.getByTestId("agent-config-antigravity")).toHaveClass("opacity-[.62]");
  });
});
