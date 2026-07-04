import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { DispatchPanel } from "./DispatchPanel";
import type { Seat } from "../../lib/types";
import { DataSourceProvider } from "../../lib/datasource";

const generatePlan = vi.fn();
const getPlanGenStatus = vi.fn();
const getPlanReview = vi.fn();
const applyPlan = vi.fn();
const runOrchestrate = vi.fn();
vi.mock("../../lib/api", () => ({
  generatePlan: (...a: unknown[]) => generatePlan(...a),
  getPlanGenStatus: (...a: unknown[]) => getPlanGenStatus(...a),
  getPlanReview: (...a: unknown[]) => getPlanReview(...a),
  applyPlan: (...a: unknown[]) => applyPlan(...a),
  runOrchestrate: (...a: unknown[]) => runOrchestrate(...a),
}));

const roster: Seat[] = [
  { id: "claude", roles: ["orchestrator", "reviewer"], kind: "claude-code" },
  { id: "kimi", roles: ["worker"], kind: "kimi-cli" },
];

beforeEach(() => {
  generatePlan.mockReset(); getPlanGenStatus.mockReset(); getPlanReview.mockReset();
  applyPlan.mockReset(); runOrchestrate.mockReset();
});

function open() {
  return render(
    <DataSourceProvider>
      <DispatchPanel project="p1" roster={roster} open onClose={() => {}} onGoLive={() => {}} />
    </DataSourceProvider>,
  );
}

describe("DispatchPanel", () => {
  it("suggests a feature slug from the goal", () => {
    open();
    fireEvent.change(screen.getByTestId("dispatch-goal"), { target: { value: "Add 2FA Login" } });
    expect(screen.getByTestId("dispatch-feature")).toHaveValue("add-2fa-login");
  });

  it("disables Generate when roster is empty", () => {
    render(
      <DataSourceProvider>
        <DispatchPanel project="p1" roster={[]} open onClose={() => {}} onGoLive={() => {}} />
      </DataSourceProvider>,
    );
    fireEvent.change(screen.getByTestId("dispatch-goal"), { target: { value: "x" } });
    expect(screen.getByTestId("dispatch-generate")).toBeDisabled();
  });

  it("runs the full flow: generate → poll done → review → dispatch", async () => {
    generatePlan.mockResolvedValue({ status_url: "/x", feature: "add-2fa-login" });
    getPlanGenStatus.mockResolvedValue({ state: "done", feature: "add-2fa-login" });
    getPlanReview.mockResolvedValue({
      present: true, feature: "add-2fa-login", valid: true,
      tasks: [{ id: "add-2fa-otp", owner: "kimi", reviewer: "claude", spec: ".pact/x.md", verify: "go test" }],
    });
    applyPlan.mockResolvedValue({ assigned: 1 });
    runOrchestrate.mockResolvedValue({ status_url: "/y" });
    open();
    fireEvent.change(screen.getByTestId("dispatch-goal"), { target: { value: "Add 2FA Login" } });
    fireEvent.click(screen.getByTestId("dispatch-generate"));
    await waitFor(() => expect(screen.getByTestId("dispatch-review")).toBeInTheDocument());
    expect(screen.getByText("add-2fa-otp")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("dispatch-confirm"));
    await waitFor(() => expect(applyPlan).toHaveBeenCalledWith("p1", "add-2fa-login"));
    expect(runOrchestrate).toHaveBeenCalledWith("p1", { feature: "add-2fa-login" });
    await waitFor(() => expect(screen.getByTestId("dispatch-done")).toBeInTheDocument());
  });

  it("blocks dispatch on an invalid plan", async () => {
    generatePlan.mockResolvedValue({ status_url: "/x", feature: "add-2fa" });
    getPlanGenStatus.mockResolvedValue({ state: "done", feature: "add-2fa" });
    getPlanReview.mockResolvedValue({ present: true, feature: "add-2fa", valid: false, error: "owner \"ghost\" not in roster", tasks: [] });
    open();
    fireEvent.change(screen.getByTestId("dispatch-goal"), { target: { value: "add 2fa" } });
    fireEvent.click(screen.getByTestId("dispatch-generate"));
    await waitFor(() => expect(screen.getByTestId("dispatch-review")).toBeInTheDocument());
    expect(screen.getByTestId("dispatch-confirm")).toBeDisabled();
    expect(screen.getByText(/not in roster/)).toBeInTheDocument();
  });

  it("surfaces a generation error", async () => {
    generatePlan.mockResolvedValue({ status_url: "/x", feature: "add-2fa" });
    getPlanGenStatus.mockResolvedValue({ state: "error", feature: "add-2fa", error: "planner failed" });
    open();
    fireEvent.change(screen.getByTestId("dispatch-goal"), { target: { value: "add 2fa" } });
    fireEvent.click(screen.getByTestId("dispatch-generate"));
    await waitFor(() => expect(screen.getByTestId("dispatch-error")).toHaveTextContent("planner failed"));
  });
});

describe("DispatchPanel — capability gating", () => {
  it("disables Generate and Dispatch when the source is read-only", () => {
    const readOnly = {
      capabilities: { canWrite: false, canOrchestrate: false, multiMachine: true },
      listProjects: vi.fn(),
      getState: vi.fn(),
      getStats: vi.fn(),
      subscribe: vi.fn(),
      generatePlan: vi.fn(),
      getPlanGenStatus: vi.fn(),
      getPlanReview: vi.fn().mockResolvedValue({
        present: true, feature: "add-2fa", valid: true,
        tasks: [{ id: "t1", owner: "kimi", reviewer: "claude", spec: "", verify: "" }],
      }),
      applyPlan: vi.fn(),
      runOrchestrate: vi.fn(),
    };
    render(
      <DataSourceProvider source={readOnly}>
        <DispatchPanel project="p1" roster={roster} open onClose={() => {}} onGoLive={() => {}} />
      </DataSourceProvider>,
    );
    fireEvent.change(screen.getByTestId("dispatch-goal"), { target: { value: "add 2fa" } });
    expect(screen.getByTestId("dispatch-generate")).toBeDisabled();
    expect(screen.getByTestId("dispatch-generate")).toHaveAttribute("title", "Remote control needs U3");
  });
});
