import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { AgentsPage } from "./AgentsPage";
import { getAgents, getAgentVersions, testAgent, getAgentConfig } from "../../lib/api";

vi.mock("../../lib/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../lib/api")>()),
  getAgents: vi.fn(),
  getAgentVersions: vi.fn(),
  testAgent: vi.fn(),
  getAgentConfig: vi.fn(),
}));

const rows = [
  { kind: "claude-code", installed: true, registered: true, detail: "/usr/local/bin/claude" },
  { kind: "codex-app", installed: true, registered: false, detail: "~/.codex/config.toml" },
  { kind: "cursor-cli", installed: false, registered: false, detail: "not found" },
];

const cfg = (kind: string) => ({
  kind,
  model: "claude-opus-5",
  effective_model: "claude-opus-5",
  restricted: false,
  allowed_tools: [],
  drivable: true,
  candidate_models: ["claude-opus-5"],
});

beforeEach(() => {
  vi.mocked(getAgents).mockResolvedValue(rows as never);
  vi.mocked(getAgentVersions).mockResolvedValue({ versions: { "claude-code": "2.1.259" } });
  vi.mocked(getAgentConfig).mockImplementation(async (k: string) => cfg(k) as never);
  vi.mocked(testAgent).mockReset();
});

describe("Agents page — 合并页", () => {
  it("按 installed 分成两个 section，未安装的进 Available 并说明原因", async () => {
    render(<AgentsPage author />);

    await screen.findByTestId("agents-installed");
    expect(screen.getByTestId("agents-available")).toBeInTheDocument();
    // 未安装的那条必须给出为什么，而不是只列个名字
    expect(screen.getByTestId("agent-row-cursor-cli")).toHaveTextContent("not found");
  });

  it("默认全部收起：没有任何一行渲染配置体", async () => {
    render(<AgentsPage author />);
    await screen.findByTestId("agents-installed");

    expect(screen.queryByTestId("agent-config-claude-code")).not.toBeInTheDocument();
  });

  it("展开一行才渲染它的配置体，且展开是可键盘触达的 disclosure", async () => {
    render(<AgentsPage author />);
    await screen.findByTestId("agents-installed");

    const disc = screen.getByTestId("agent-disclosure-claude-code");
    // 复核意见：行不能是 div+cursor:pointer，必须有 aria-expanded 且可聚焦
    expect(disc.tagName).toBe("BUTTON");
    expect(disc).toHaveAttribute("aria-expanded", "false");

    fireEvent.click(disc);
    await waitFor(() => expect(screen.getByTestId("agent-config-claude-code")).toBeInTheDocument());
    expect(disc).toHaveAttribute("aria-expanded", "true");
  });

  it("多开：展开第二行不会收起第一行", async () => {
    vi.mocked(getAgents).mockResolvedValue([
      rows[0],
      { kind: "codex-cli", installed: true, registered: true, detail: "/usr/local/bin/codex" },
    ] as never);
    render(<AgentsPage author />);
    await screen.findByTestId("agents-installed");

    fireEvent.click(screen.getByTestId("agent-disclosure-claude-code"));
    fireEvent.click(screen.getByTestId("agent-disclosure-codex-cli"));

    await waitFor(() => expect(screen.getByTestId("agent-config-codex-cli")).toBeInTheDocument());
    expect(screen.getByTestId("agent-config-claude-code")).toBeInTheDocument();
  });

  it("收起行显示版本号（异步到达，不阻塞列表）", async () => {
    render(<AgentsPage author />);
    await screen.findByTestId("agents-installed");

    await waitFor(() =>
      expect(screen.getByTestId("agent-row-claude-code")).toHaveTextContent("2.1.259"),
    );
  });

  it("Test 失败时必须说明是哪一层失败，而不是只给一个红叉", async () => {
    vi.mocked(testAgent).mockResolvedValue({
      kind: "claude-code",
      ok: false,
      checks: [
        { name: "cli claude-code: binary", ok: true, detail: "/usr/local/bin/claude" },
        { name: "cli claude-code: auth", ok: false, detail: "no ~/.claude — run `claude login`" },
        { name: "cli claude-code: transport", ok: true, detail: "transport: acp available" },
      ],
    });
    render(<AgentsPage author />);
    await screen.findByTestId("agents-installed");

    fireEvent.click(screen.getByTestId("agent-test-claude-code"));

    // 结果条只在展开态出现（独立验证指出收起态渲染它会破坏「收起=单行」），
    // 因此点 Test 必须自动展开该行——否则点了按钮看不到任何结果。
    const strip = await screen.findByTestId("agent-test-result-claude-code");
    expect(screen.getByTestId("agent-disclosure-claude-code")).toHaveAttribute("aria-expanded", "true");
    expect(strip).toHaveTextContent("auth");
    expect(strip).toHaveTextContent("claude login");
    // 不能只靠颜色：失败项必须带可读的文字标记
    expect(strip.textContent).toMatch(/[✕✓]/);
  });

  it("hosted 只读：author=false 时不渲染任何写操作", async () => {
    render(<AgentsPage author={false} />);
    await screen.findByTestId("agents-installed");

    expect(screen.queryByTestId("agent-register-codex-app")).not.toBeInTheDocument();
    // Test 是只读探测，只读模式下仍然允许
    expect(screen.getByTestId("agent-test-claude-code")).toBeInTheDocument();

    // 独立验证指出：旧断言只查 Register 按钮，正好绕开了配置体——只读模式下
    // 模型下拉/权限档/Save 当时全都可用且真的发出了写请求。这条堵住那道缝。
    fireEvent.click(screen.getByTestId("agent-disclosure-claude-code"));
    await waitFor(() => expect(screen.getByTestId("agent-config-claude-code")).toBeInTheDocument());
    expect(screen.getByTestId("model-select-claude-code")).toBeDisabled();
    expect(screen.getByTestId("posture-blanket-claude-code")).toBeDisabled();
  });

  it("Rescan 重新拉取列表与版本", async () => {
    render(<AgentsPage author />);
    await screen.findByTestId("agents-installed");
    const before = vi.mocked(getAgents).mock.calls.length;

    fireEvent.click(screen.getByTestId("agents-rescan"));

    await waitFor(() =>
      expect(vi.mocked(getAgents).mock.calls.length).toBeGreaterThan(before),
    );
    expect(vi.mocked(getAgentVersions).mock.calls.length).toBeGreaterThan(1);
  });

  it("零 CLI：给出可操作的 empty state，而不是一片空白", async () => {
    vi.mocked(getAgents).mockResolvedValue([] as never);
    render(<AgentsPage author />);

    const installed = await screen.findByTestId("agents-installed");
    expect(installed).toHaveTextContent(/No supported agents detected/i);
    // 空状态必须告诉人下一步做什么
    expect(installed).toHaveTextContent(/Rescan/i);
  });

  it("rescan 瞬时失败：保留已渲染的列表，只提示，不清空", async () => {
    render(<AgentsPage author />);
    await screen.findByTestId("agents-installed");
    expect(screen.getByTestId("agent-row-claude-code")).toBeInTheDocument();

    vi.mocked(getAgents).mockRejectedValueOnce(new Error("network blip"));
    fireEvent.click(screen.getByTestId("agents-rescan"));

    // 独立验证指出：原实现把整页换成 Alert，一次抖动就清空用户正在看的内容。
    await waitFor(() => expect(screen.getByText(/Rescan failed/i)).toBeInTheDocument());
    expect(screen.getByTestId("agent-row-claude-code")).toBeInTheDocument();
    expect(screen.queryByTestId("agents-error")).not.toBeInTheDocument();
  });

  it("展开态改动会显示自动保存状态（embedded 此前完全没有指示）", async () => {
    render(<AgentsPage author />);
    await screen.findByTestId("agents-installed");

    fireEvent.click(screen.getByTestId("agent-disclosure-claude-code"));
    await waitFor(() => expect(screen.getByTestId("agent-config-claude-code")).toBeInTheDocument());
    expect(screen.getByTestId("autosave-state")).toBeInTheDocument();
  });

  it("加载失败给出可重试的错误，而不是空白页", async () => {
    vi.mocked(getAgents).mockRejectedValue(new Error("boom"));
    render(<AgentsPage author />);

    expect(await screen.findByTestId("agents-error")).toHaveTextContent(/failed/i);
  });
});
