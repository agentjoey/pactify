import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { FallbackCard } from "./FallbackCard";
import { DataSourceProvider, type DataSource } from "../../lib/datasource";
import { ProposalGoneError } from "../../lib/api";

const pending = {
  pending: true,
  task: "t1",
  seat: "build2",
  fromRole: "primary",
  toRole: "backup",
  reason: "worker run: run timeout (--run-timeout) exceeded",
};

const getProposal = vi.fn();
const approve = vi.fn();

/** A source that can reach the machine holding the proposal (local serve). */
function localSource(): DataSource {
  return {
    capabilities: { canWrite: true, canOrchestrate: true, multiMachine: false, cockpit: true },
    getFallbackProposal: getProposal,
    approveFallback: approve,
  } as unknown as DataSource;
}

function renderCard(ui: React.ReactElement, source: DataSource = localSource()) {
  return render(<DataSourceProvider source={source}>{ui}</DataSourceProvider>);
}

beforeEach(() => {
  vi.clearAllMocks();
  getProposal.mockResolvedValue({ pending: false });
});

describe("FallbackCard — approved state matrix", () => {
  // 无提案 → 不渲染（零视觉噪音）
  it("renders nothing when no proposal is pending", async () => {
    const { container } = renderCard(<FallbackCard project="p" canWrite />);
    await waitFor(() => expect(getProposal).toHaveBeenCalled());
    expect(container).toBeEmptyDOMElement();
  });

  // 有提案 → 四要素 + 两个动作
  it("shows seat, from→to role and reason with both actions", async () => {
    getProposal.mockResolvedValue(pending);
    renderCard(<FallbackCard project="p" canWrite />);
    // The seat belongs to the headline, not merely "somewhere in the card":
    // asserting on full card text let a substring of the reason satisfy this.
    const title = await screen.findByRole("heading", { name: /build2 could not run/ });
    expect(title.textContent).toContain("t1");
    const text = screen.getByTestId("fallback-card").textContent ?? "";
    expect(text).toContain("primary"); // from role
    expect(text).toContain("backup"); // to role
    expect(text).toContain("run timeout"); // reason
    expect(screen.getByTestId("fallback-approve")).toBeTruthy();
    expect(screen.getByTestId("fallback-dismiss")).toBeTruthy();
  });

  // 提交中 → 两个动作都锁住（Dismiss 也不能在飞行中抽走卡片）
  it("disables both actions while approving", async () => {
    getProposal.mockResolvedValue(pending);
    let release!: () => void;
    approve.mockReturnValue(
      new Promise((res) => {
        release = () => res({ status_url: "/s" });
      }),
    );
    renderCard(<FallbackCard project="p" canWrite />);
    fireEvent.click(await screen.findByTestId("fallback-approve"));
    await waitFor(() =>
      expect(screen.getByTestId("fallback-approve").hasAttribute("disabled")).toBe(true),
    );
    expect(screen.getByTestId("fallback-dismiss").hasAttribute("disabled")).toBe(true);
    release();
  });

  // 成功 → 卡片消失，并通知调用方刷新 run 状态
  it("disappears after a successful approval and notifies the caller", async () => {
    getProposal.mockResolvedValue(pending);
    approve.mockResolvedValue({ status_url: "/s" });
    const onApproved = vi.fn();
    renderCard(<FallbackCard project="p" canWrite onApproved={onApproved} />);
    fireEvent.click(await screen.findByTestId("fallback-approve"));
    await waitFor(() => expect(screen.queryByTestId("fallback-card")).toBeNull());
    expect(approve).toHaveBeenCalledWith("p");
    expect(onApproved).toHaveBeenCalled();
  });

  // 失败 → 行内错误（role=alert 让屏幕阅读器听得到），提案保留，可重试
  it("keeps the card and announces an inline error when approval fails", async () => {
    getProposal.mockResolvedValue(pending);
    approve.mockRejectedValue(new Error("orchestrate is already running"));
    renderCard(<FallbackCard project="p" canWrite />);
    fireEvent.click(await screen.findByTestId("fallback-approve"));
    const box = await screen.findByRole("alert");
    expect(box.textContent).toContain("already running");
    expect(screen.getByTestId("fallback-card")).toBeTruthy();
    expect(screen.getByTestId("fallback-approve").hasAttribute("disabled")).toBe(false);
  });

  // 提案已被别处消费（404）→ 卡片退场，而不是留一个永远失败的重试按钮
  it("retires the card when the server says nothing is pending", async () => {
    getProposal.mockResolvedValue(pending);
    approve.mockRejectedValue(new ProposalGoneError("no fallback proposal is pending"));
    renderCard(<FallbackCard project="p" canWrite />);
    fireEvent.click(await screen.findByTestId("fallback-approve"));
    await waitFor(() => expect(screen.queryByTestId("fallback-card")).toBeNull());
    expect(screen.queryByRole("alert")).toBeNull();
  });

  // 只读（hosted）→ 动作不可用并说明原因
  it("is read-only without write capability", async () => {
    getProposal.mockResolvedValue(pending);
    renderCard(<FallbackCard project="p" canWrite={false} />);
    expect(await screen.findByTestId("fallback-card")).toBeTruthy();
    expect(screen.queryByTestId("fallback-approve")).toBeNull();
    expect(screen.getByTestId("fallback-readonly")).toBeTruthy();
  });

  // 源本身够不着那台机器（relay）→ 同样只读，不给发不出去的按钮
  it("is read-only when the source cannot approve", async () => {
    const relay = {
      capabilities: { canWrite: true, canOrchestrate: true, multiMachine: true, cockpit: false },
      getFallbackProposal: getProposal,
    } as unknown as DataSource;
    getProposal.mockResolvedValue(pending);
    renderCard(<FallbackCard project="p" canWrite />, relay);
    expect(await screen.findByTestId("fallback-card")).toBeTruthy();
    expect(screen.queryByTestId("fallback-approve")).toBeNull();
    expect(screen.getByTestId("fallback-readonly")).toBeTruthy();
  });

  // Dismiss 只关闭本次显示，不消费提案（提案仍在服务端待批）
  it("dismiss hides the card without approving", async () => {
    getProposal.mockResolvedValue(pending);
    renderCard(<FallbackCard project="p" canWrite />);
    fireEvent.click(await screen.findByTestId("fallback-dismiss"));
    await waitFor(() => expect(screen.queryByTestId("fallback-card")).toBeNull());
    expect(approve).not.toHaveBeenCalled();
  });
});

describe("FallbackCard — liveness", () => {
  // 升级发生在运行中：只在挂载时取一次，卡片永远不会出现
  it("polls, so a proposal raised after mount still surfaces", async () => {
    vi.useFakeTimers();
    renderCard(<FallbackCard project="p" canWrite />);
    await vi.waitFor(() => expect(getProposal).toHaveBeenCalled());
    expect(screen.queryByTestId("fallback-card")).toBeNull();

    getProposal.mockResolvedValue(pending);
    await vi.advanceTimersByTimeAsync(10_000);
    await vi.waitFor(() => expect(screen.queryByTestId("fallback-card")).not.toBeNull());
    vi.useRealTimers();
  });

  // 切项目：卡片必须先清空，否则会拿 A 的文案去批 B 的 run
  it("clears the previous project's proposal when the project changes", async () => {
    getProposal.mockResolvedValue(pending);
    const source = localSource();
    const { rerender } = renderCard(<FallbackCard project="a" canWrite />, source);
    expect(await screen.findByTestId("fallback-card")).toBeTruthy();

    let releaseB!: (v: { pending: boolean }) => void;
    getProposal.mockReturnValue(new Promise((res) => (releaseB = res)));
    rerender(
      <DataSourceProvider source={source}>
        <FallbackCard project="b" canWrite />
      </DataSourceProvider>,
    );
    // While project b's fetch is in flight, project a's proposal must be gone.
    expect(screen.queryByTestId("fallback-card")).toBeNull();
    releaseB({ pending: false });
  });

  // Dismiss 按提案身份记：换了新提案要重新出现
  it("shows a new proposal after an earlier one was dismissed", async () => {
    vi.useFakeTimers();
    getProposal.mockResolvedValue(pending);
    renderCard(<FallbackCard project="p" canWrite />);
    await vi.waitFor(() => expect(screen.queryByTestId("fallback-card")).not.toBeNull());
    fireEvent.click(screen.getByTestId("fallback-dismiss"));
    expect(screen.queryByTestId("fallback-card")).toBeNull();

    getProposal.mockResolvedValue({ ...pending, task: "t2", toRole: "third" });
    await vi.advanceTimersByTimeAsync(10_000);
    await vi.waitFor(() => expect(screen.queryByTestId("fallback-card")).not.toBeNull());
    vi.useRealTimers();
  });

  // 读不到提案 → 按「无提案」处理，绝不邀请一次不确定的批准
  it("treats an unreadable proposal as none pending", async () => {
    getProposal.mockRejectedValue(new Error("network"));
    const { container } = renderCard(<FallbackCard project="p" canWrite />);
    await waitFor(() => expect(getProposal).toHaveBeenCalled());
    expect(container).toBeEmptyDOMElement();
  });
});
