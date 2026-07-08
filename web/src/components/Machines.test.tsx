import { render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { Machines } from "./Machines";
import { DataSourceProvider } from "../lib/datasource";
import type { DataSource } from "../lib/datasource";
import type { Machine } from "../lib/types";

function makeSource(
  getMachines?: DataSource["getMachines"],
  multiMachine = true,
): DataSource {
  return {
    capabilities: { canWrite: false, canOrchestrate: false, multiMachine, cockpit: false },
    listProjects: vi.fn(),
    getState: vi.fn(),
    getStats: vi.fn(),
    subscribe: vi.fn(),
    getMachines,
  } as unknown as DataSource;
}

function renderMachines(source: DataSource) {
  return render(
    <DataSourceProvider source={source}>
      <Machines />
    </DataSourceProvider>,
  );
}

describe("Machines", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("shows a local-mode message when source has no getMachines", () => {
    const source = makeSource(undefined, false);
    renderMachines(source);
    expect(screen.getByTestId("machines-local")).toHaveTextContent(/hosted mode/i);
  });

  it("shows loading state then renders online/offline rows with agent kinds", async () => {
    const now = 1_700_000_000_000;
    const machines: Machine[] = [
      {
        machineId: "machine-abc-123",
        host: "laptop.local",
        agentKinds: ["opencode", "claude-code"],
        online: true,
        lastSeenAt: now,
      },
      {
        machineId: "offline-one",
        agentKinds: ["gemini"],
        online: false,
        lastSeenAt: now - 3_600_000,
      },
    ];
    const source = makeSource(vi.fn().mockResolvedValue(machines));
    renderMachines(source);

    expect(screen.getByTestId("machines-loading")).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByTestId("machines-loading")).toBeNull());

    const rows = screen.getAllByTestId("machine-row");
    expect(rows).toHaveLength(2);
    expect(screen.getByText("laptop.local")).toBeInTheDocument();
    expect(screen.getByText("opencode")).toBeInTheDocument();
    expect(screen.getByText("claude-code")).toBeInTheDocument();
    expect(screen.getByText("offline-one".slice(0, 8))).toBeInTheDocument();
    expect(screen.getByText("gemini")).toBeInTheDocument();

    const indicators = screen.getAllByTestId("machine-online-indicator");
    expect(indicators[0]).toHaveAttribute("title", "online");
    expect(indicators[1]).toHaveAttribute("title", "offline");
  });

  it("falls back to machineId prefix when host is absent", async () => {
    const machines: Machine[] = [
      {
        machineId: "desktop-xyz",
        agentKinds: ["opencode"],
        online: true,
        lastSeenAt: 1_700_000_000_000,
      },
    ];
    const source = makeSource(vi.fn().mockResolvedValue(machines));
    renderMachines(source);
    await waitFor(() => expect(screen.getAllByTestId("machine-row")).toHaveLength(1));
    expect(screen.getByText("desktop-")).toBeInTheDocument();
  });

  it("shows empty state when no machines are returned", async () => {
    const source = makeSource(vi.fn().mockResolvedValue([]));
    renderMachines(source);
    await waitFor(() => expect(screen.getByTestId("machines-empty")).toBeInTheDocument());
  });

  it("shows error state when getMachines rejects", async () => {
    const source = makeSource(vi.fn().mockRejectedValue(new Error("relay down")));
    renderMachines(source);
    await waitFor(() => expect(screen.getByTestId("machines-error")).toHaveTextContent("relay down"));
  });
});
