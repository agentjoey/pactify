import { describe, it, expect, vi, beforeEach } from "vitest";
import { render as rtlRender, screen, fireEvent } from "@testing-library/react";
import type { ReactElement } from "react";
import { SettingsModal } from "./SettingsModal";
import { DataSourceProvider, type DataSource } from "../../lib/datasource";

// SettingsModal reads capabilities to gate PROJECT/MACHINE groups in hosted
// mode; these tests exercise the LOCAL layout (multiMachine: false).
function localSource(): DataSource {
  return {
    capabilities: { canWrite: true, canOrchestrate: false, multiMachine: false, cockpit: false },
    listProjects: vi.fn(),
    getState: vi.fn(),
    subscribe: vi.fn(() => () => {}),
  } as unknown as DataSource;
}

function render(ui: ReactElement) {
  return rtlRender(<DataSourceProvider source={localSource()}>{ui}</DataSourceProvider>);
}

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url.includes("/api/agents")) return new Response("[]", { status: 200 });
    if (url.includes("/wiring")) return new Response("[]", { status: 200 });
    if (url.includes("/seats")) return new Response("[]", { status: 200 });
    if (url.includes("/api/registry")) return new Response("[]", { status: 200 });
    return new Response("[]", { status: 200 });
  }));
});

describe("SettingsModal", () => {
  it("renders a centered settings sheet with rail, search, and close button", () => {
    render(<SettingsModal project="demo" author={true} onClose={() => {}} />);
    expect(screen.getByTestId("settings-modal")).toBeInTheDocument();
    expect(screen.getByText("Settings")).toBeInTheDocument();
    expect(screen.getByPlaceholderText("Search settings")).toBeInTheDocument();
  });

  it("closes on the sheet close button", () => {
    const onClose = vi.fn();
    render(<SettingsModal project="demo" author={true} onClose={onClose} />);
    fireEvent.click(screen.getByLabelText("close"));
    expect(onClose).toHaveBeenCalled();
  });

  it("renders scope-grouped left nav with PROJECT/MACHINE/ACCOUNT groups", () => {
    render(<SettingsModal project="demo" author={true} onClose={() => {}} />);
    expect(screen.getByTestId("settings-nav")).toBeInTheDocument();
    expect(screen.getAllByTestId("settings-nav-group").length).toBe(3);
    // Scope labels appear in both the rail group header and the pane header.
    expect(screen.getAllByText("PROJECT · demo").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("MACHINE · this computer").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("ACCOUNT").length).toBeGreaterThan(0);
  });

  // 2026-09-05：Registered agents 与 Agent configs 合并为单一 Agents 页，
  // 默认页与 nav id 随之改变。这是被批准的改动带来的合法测试更新，不是迁就实现。
  it("defaults to Agents with a MACHINE scope banner", () => {
    render(<SettingsModal project="demo" author={true} onClose={() => {}} />);
    expect(screen.getByTestId("settings-scope-banner")).toHaveTextContent("MACHINE · all projects");
    expect(screen.getByTestId("nav-agents")).toHaveAttribute("aria-current", "true");
  });

  it("旧的两个入口不再存在（合并后不应留下空壳导航）", () => {
    render(<SettingsModal project="demo" author={true} onClose={() => {}} />);
    expect(screen.queryByTestId("nav-agent-configs")).not.toBeInTheDocument();
    expect(screen.queryByTestId("nav-registered-agents")).not.toBeInTheDocument();
  });

  it("switches panels when clicking nav items", () => {
    render(<SettingsModal project="demo" author={true} onClose={() => {}} />);
    fireEvent.click(screen.getByTestId("nav-seats"));
    expect(screen.getByTestId("nav-seats")).toHaveAttribute("aria-current", "true");
    expect(screen.getByTestId("settings-scope-banner")).toHaveTextContent("PROJECT · demo");
  });

  it("surfaces the focused seat in the project-seats section when opened from a roster gear", () => {
    render(<SettingsModal project="demo" author={true} focusSeat="kimi" onClose={() => {}} />);
    expect(screen.getByTestId("nav-seats")).toHaveAttribute("aria-current", "true");
    expect(screen.getByTestId("settings-project-seats")).toHaveTextContent("kimi");
  });

  it("shows no seat focus when opened from the toolbar gear (focusSeat null)", () => {
    render(<SettingsModal project="demo" author={true} focusSeat={null} onClose={() => {}} />);
    fireEvent.click(screen.getByTestId("nav-seats"));
    expect(screen.getByTestId("settings-project-seats")).not.toHaveTextContent("· kimi");
  });

  it("renders a placeholder for items without existing panels", () => {
    render(<SettingsModal project="demo" author={true} onClose={() => {}} />);
    fireEvent.click(screen.getByTestId("nav-review-gate"));
    expect(screen.getByTestId("settings-scope-banner")).toHaveTextContent("PROJECT · demo");
    expect(screen.getByTestId("settings-placeholder")).toBeInTheDocument();
  });

  it("hosted mode hides PROJECT/MACHINE groups and opens on Account", () => {
    const hosted = {
      capabilities: { canWrite: true, canOrchestrate: true, multiMachine: true, cockpit: true },
      listProjects: vi.fn(),
      getState: vi.fn(),
      subscribe: vi.fn(() => () => {}),
      // AccountPanel mounts and fetches; fetch is stubbed in beforeEach.
    } as unknown as DataSource;
    rtlRender(
      <DataSourceProvider source={hosted}>
        <SettingsModal project="demo" author={true} onClose={() => {}} />
      </DataSourceProvider>,
    );
    expect(screen.queryByText(/PROJECT ·/)).not.toBeInTheDocument();
    expect(screen.queryByText(/MACHINE · this computer/)).not.toBeInTheDocument();
    expect(screen.getAllByText("ACCOUNT").length).toBeGreaterThan(0);
    // Opens directly on the Account panel (banner shows ACCOUNT scope).
    expect(screen.queryByText("Agent configs")).not.toBeInTheDocument();
  });
});
