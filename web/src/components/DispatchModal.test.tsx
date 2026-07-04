import { render, screen } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { DataSourceProvider, LocalServeSource, type DataSource } from "../lib/datasource";
import { DispatchModal } from "./DispatchModal";
import type { Draft } from "../lib/canvas";

const draft: Draft = { id: "d1", specMd: "# spec", feature: "F1", deps: [] };

function renderModal(source: DataSource) {
  return render(
    <DataSourceProvider source={source}>
      <DispatchModal
        project="demo"
        draft={draft}
        owner="bob"
        roster={["bob", "alice"]}
        branch="feat/x"
        onDispatched={() => {}}
        onClose={() => {}}
      />
    </DataSourceProvider>,
  );
}

// A hosted-like source without postTask (task authoring writes a spec_md FILE on
// the machine, which the zero-knowledge relay can't do yet — a pact.task rpc is
// in flight). The modal must disable the create action rather than hit a
// non-existent local /api.
function hostedSource(): DataSource {
  return {
    capabilities: { canWrite: true, canOrchestrate: true, multiMachine: true },
    listProjects: vi.fn().mockResolvedValue([]),
    getState: vi.fn(),
    getStats: vi.fn().mockResolvedValue({ tasks: [], agents: [] }),
    subscribe: vi.fn().mockReturnValue(() => {}),
    verb: vi.fn().mockResolvedValue(undefined),
    // deliberately no postTask
  } as unknown as DataSource;
}

describe("DispatchModal — task-authoring guard", () => {
  it("local source (postTask present): Confirm enabled, no hosted note", () => {
    renderModal(new LocalServeSource());
    const btn = screen.getByRole("button", { name: "Confirm dispatch" }) as HTMLButtonElement;
    expect(btn.disabled).toBe(false);
    expect(screen.queryByTestId("dispatch-hosted-note")).toBeNull();
  });

  it("hosted source (no postTask): Confirm disabled + hosted note shown", () => {
    renderModal(hostedSource());
    const btn = screen.getByRole("button", { name: "Confirm dispatch" }) as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
    expect(screen.getByTestId("dispatch-hosted-note")).toBeTruthy();
  });
});
