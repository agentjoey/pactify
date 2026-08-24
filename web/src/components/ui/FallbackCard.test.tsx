import { render, screen, fireEvent, waitFor, within } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { FallbackCards } from "./FallbackCard";
import { DataSourceProvider, LocalServeSource, type DataSource } from "../../lib/datasource";
import { ProposalGoneError } from "../../lib/api";

const pending = {
  scope: "p3",
  task: "t1",
  seat: "build2",
  fromRole: "primary",
  toRole: "backup",
  reason: "worker run: run timeout (--run-timeout) exceeded",
};

/** A second, independent decision — what a --max-concurrency > 1 run produces. */
const other = {
  scope: "p4",
  task: "t2",
  seat: "build3",
  fromRole: "primary",
  toRole: "third",
  reason: "quota exhausted",
};

const getProposals = vi.fn();
const approve = vi.fn();

/** A source that can reach the machine holding the proposal (local serve). */
function localSource(): DataSource {
  return {
    capabilities: { canWrite: true, canOrchestrate: true, multiMachine: false, cockpit: true },
    getFallbackProposals: getProposals,
    approveFallback: approve,
  } as unknown as DataSource;
}

function renderCard(ui: React.ReactElement, source: DataSource = localSource()) {
  return render(<DataSourceProvider source={source}>{ui}</DataSourceProvider>);
}

beforeEach(() => {
  vi.clearAllMocks();
  getProposals.mockResolvedValue([]);
});

describe("FallbackCard — approved state matrix", () => {
  // 无提案 → 不渲染（零视觉噪音）
  it("renders nothing when no proposal is pending", async () => {
    const { container } = renderCard(<FallbackCards project="p" canWrite />);
    await waitFor(() => expect(getProposals).toHaveBeenCalled());
    expect(container).toBeEmptyDOMElement();
  });

  // 有提案 → 四要素 + 两个动作
  it("shows seat, from→to role and reason with both actions", async () => {
    getProposals.mockResolvedValue([pending]);
    renderCard(<FallbackCards project="p" canWrite />);
    // The seat belongs to the headline, not merely "somewhere in the card":
    // asserting on full card text let a substring of the reason satisfy this.
    const title = await screen.findByRole("heading", { name: /build2 could not run/ });
    expect(title.textContent).toContain("t1");
    const text = screen.getByTestId("fallback-card").textContent ?? "";
    expect(text).toContain("primary"); // from role
    expect(text).toContain("backup"); // to role
    expect(text).toContain("run timeout"); // reason
    expect(text).toContain("p3"); // scope — which feature this decision is about
    expect(screen.getByTestId("fallback-approve")).toBeTruthy();
    expect(screen.getByTestId("fallback-dismiss")).toBeTruthy();
  });

  // 提交中 → 两个动作都锁住（Dismiss 也不能在飞行中抽走卡片）
  it("disables both actions while approving", async () => {
    getProposals.mockResolvedValue([pending]);
    let release!: () => void;
    approve.mockReturnValue(
      new Promise((res) => {
        release = () => res({ status_url: "/s" });
      }),
    );
    renderCard(<FallbackCards project="p" canWrite />);
    fireEvent.click(await screen.findByTestId("fallback-approve"));
    await waitFor(() =>
      expect(screen.getByTestId("fallback-approve").hasAttribute("disabled")).toBe(true),
    );
    expect(screen.getByTestId("fallback-dismiss").hasAttribute("disabled")).toBe(true);
    release();
  });

  // 成功 → 卡片消失，带着「这一个」提案的 task，并通知调用方刷新 run 状态
  it("disappears after a successful approval and notifies the caller", async () => {
    getProposals.mockResolvedValue([pending]);
    approve.mockResolvedValue({ status_url: "/s" });
    const onApproved = vi.fn();
    renderCard(<FallbackCards project="p" canWrite onApproved={onApproved} />);
    fireEvent.click(await screen.findByTestId("fallback-approve"));
    await waitFor(() => expect(screen.queryByTestId("fallback-card")).toBeNull());
    // The task id is the whole point: the server 404s an approval that names no
    // pending task, and the card renders that 404 as "already handled".
    expect(approve).toHaveBeenCalledWith("p", "t1");
    expect(onApproved).toHaveBeenCalled();
  });

  // 失败 → 行内错误（role=alert 让屏幕阅读器听得到），提案保留，可重试
  it("keeps the card and announces an inline error when approval fails", async () => {
    getProposals.mockResolvedValue([pending]);
    approve.mockRejectedValue(new Error("orchestrate is already running"));
    renderCard(<FallbackCards project="p" canWrite />);
    fireEvent.click(await screen.findByTestId("fallback-approve"));
    const box = await screen.findByRole("alert");
    expect(box.textContent).toContain("already running");
    expect(screen.getByTestId("fallback-card")).toBeTruthy();
    expect(screen.getByTestId("fallback-approve").hasAttribute("disabled")).toBe(false);
  });

  // 提案已被别处消费（404）→ 卡片退场，而不是留一个永远失败的重试按钮
  it("retires the card when the server says nothing is pending", async () => {
    getProposals.mockResolvedValue([pending]);
    approve.mockRejectedValue(new ProposalGoneError("no fallback proposal is pending"));
    renderCard(<FallbackCards project="p" canWrite />);
    fireEvent.click(await screen.findByTestId("fallback-approve"));
    await waitFor(() => expect(screen.queryByTestId("fallback-card")).toBeNull());
    expect(screen.queryByRole("alert")).toBeNull();
  });

  // 只读（hosted）→ 动作不可用并说明原因
  it("is read-only without write capability", async () => {
    getProposals.mockResolvedValue([pending]);
    renderCard(<FallbackCards project="p" canWrite={false} />);
    expect(await screen.findByTestId("fallback-card")).toBeTruthy();
    expect(screen.queryByTestId("fallback-approve")).toBeNull();
    expect(screen.getByTestId("fallback-readonly")).toBeTruthy();
  });

  // 源本身够不着那台机器（relay）→ 同样只读，不给发不出去的按钮
  it("is read-only when the source cannot approve", async () => {
    const relay = {
      capabilities: { canWrite: true, canOrchestrate: true, multiMachine: true, cockpit: false },
      getFallbackProposals: getProposals,
    } as unknown as DataSource;
    getProposals.mockResolvedValue([pending]);
    renderCard(<FallbackCards project="p" canWrite />, relay);
    expect(await screen.findByTestId("fallback-card")).toBeTruthy();
    expect(screen.queryByTestId("fallback-approve")).toBeNull();
    expect(screen.getByTestId("fallback-readonly")).toBeTruthy();
  });

  // 提案没有 task → 服务端必然 404，按钮只会伪装成「已被别处处理」，故不给按钮
  it("is read-only when the proposal names no task", async () => {
    getProposals.mockResolvedValue([{ scope: "p3", seat: "build2", toRole: "backup" }]);
    renderCard(<FallbackCards project="p" canWrite />);
    expect(await screen.findByTestId("fallback-card")).toBeTruthy();
    expect(screen.queryByTestId("fallback-approve")).toBeNull();
    expect(screen.getByTestId("fallback-readonly").textContent).toContain("names no task");
  });

  // Dismiss 只关闭本次显示，不消费提案（提案仍在服务端待批）
  it("dismiss hides the card without approving", async () => {
    getProposals.mockResolvedValue([pending]);
    renderCard(<FallbackCards project="p" canWrite />);
    fireEvent.click(await screen.findByTestId("fallback-dismiss"));
    await waitFor(() => expect(screen.queryByTestId("fallback-card")).toBeNull());
    expect(approve).not.toHaveBeenCalled();
  });
});

// 并行 run 会同时挂起多个 feature：一个提案 = 一个人的决定 = 一张卡。
// 这一组是形状回归的第一道网：后端若退回旧的单对象响应，容器拿到的就不是列表，
// 卡片数直接为 0。
describe("FallbackCards — one card per proposal", () => {
  it("renders one card per pending proposal", async () => {
    getProposals.mockResolvedValue([pending, other]);
    renderCard(<FallbackCards project="p" canWrite />);
    await waitFor(() => expect(screen.getAllByTestId("fallback-card")).toHaveLength(2));
    const [a, b] = screen.getAllByTestId("fallback-card");
    // Each card names its own seat and its own feature, so a stack of them is
    // still N distinguishable decisions.
    expect(a.textContent).toContain("build2");
    expect(a.textContent).toContain("p3");
    expect(b.textContent).toContain("build3");
    expect(b.textContent).toContain("p4");
  });

  it("gives every card its own heading id so aria-labelledby resolves per card", async () => {
    getProposals.mockResolvedValue([pending, other]);
    renderCard(<FallbackCards project="p" canWrite />);
    await waitFor(() => expect(screen.getAllByTestId("fallback-card")).toHaveLength(2));
    const ids = screen
      .getAllByTestId("fallback-card")
      .map((c) => c.getAttribute("aria-labelledby") ?? "");
    expect(ids[0]).toBeTruthy();
    expect(new Set(ids).size).toBe(2);
    // …and each id actually points at that card's own heading.
    for (const card of screen.getAllByTestId("fallback-card")) {
      const id = card.getAttribute("aria-labelledby") ?? "";
      expect(within(card).getByRole("heading").getAttribute("id")).toBe(id);
    }
  });

  it("approves only the clicked proposal and leaves the others standing", async () => {
    getProposals.mockResolvedValue([pending, other]);
    approve.mockResolvedValue({ status_url: "/s" });
    renderCard(<FallbackCards project="p" canWrite />);
    await waitFor(() => expect(screen.getAllByTestId("fallback-card")).toHaveLength(2));

    const first = screen.getAllByTestId("fallback-card")[0];
    fireEvent.click(within(first).getByTestId("fallback-approve"));

    await waitFor(() => expect(screen.getAllByTestId("fallback-card")).toHaveLength(1));
    expect(approve).toHaveBeenCalledTimes(1);
    expect(approve).toHaveBeenCalledWith("p", "t1");
    expect(screen.getByTestId("fallback-card").textContent).toContain("build3");
  });

  it("dismisses only the clicked proposal", async () => {
    getProposals.mockResolvedValue([pending, other]);
    renderCard(<FallbackCards project="p" canWrite />);
    await waitFor(() => expect(screen.getAllByTestId("fallback-card")).toHaveLength(2));

    const second = screen.getAllByTestId("fallback-card")[1];
    fireEvent.click(within(second).getByTestId("fallback-dismiss"));

    await waitFor(() => expect(screen.getAllByTestId("fallback-card")).toHaveLength(1));
    expect(screen.getByTestId("fallback-card").textContent).toContain("build2");
  });

  it("an inline error on one card does not touch the other", async () => {
    getProposals.mockResolvedValue([pending, other]);
    approve.mockRejectedValue(new Error("orchestrate is already running"));
    renderCard(<FallbackCards project="p" canWrite />);
    await waitFor(() => expect(screen.getAllByTestId("fallback-card")).toHaveLength(2));

    fireEvent.click(
      within(screen.getAllByTestId("fallback-card")[0]).getByTestId("fallback-approve"),
    );
    await screen.findByRole("alert");
    expect(screen.getAllByTestId("fallback-error")).toHaveLength(1);
    expect(screen.getAllByTestId("fallback-card")).toHaveLength(2);
  });
});

describe("FallbackCard — liveness", () => {
  // 升级发生在运行中：只在挂载时取一次，卡片永远不会出现
  it("polls, so a proposal raised after mount still surfaces", async () => {
    vi.useFakeTimers();
    renderCard(<FallbackCards project="p" canWrite />);
    await vi.waitFor(() => expect(getProposals).toHaveBeenCalled());
    expect(screen.queryByTestId("fallback-card")).toBeNull();

    getProposals.mockResolvedValue([pending]);
    await vi.advanceTimersByTimeAsync(10_000);
    await vi.waitFor(() => expect(screen.queryByTestId("fallback-card")).not.toBeNull());
    vi.useRealTimers();
  });

  // 第二个 feature 后挂起：已有卡片不动，新卡片补上
  it("adds a card when a second proposal appears mid-run", async () => {
    vi.useFakeTimers();
    getProposals.mockResolvedValue([pending]);
    renderCard(<FallbackCards project="p" canWrite />);
    await vi.waitFor(() => expect(screen.queryAllByTestId("fallback-card")).toHaveLength(1));

    getProposals.mockResolvedValue([pending, other]);
    await vi.advanceTimersByTimeAsync(10_000);
    await vi.waitFor(() => expect(screen.queryAllByTestId("fallback-card")).toHaveLength(2));
    vi.useRealTimers();
  });

  // 切项目：卡片必须先清空，否则会拿 A 的文案去批 B 的 run
  it("clears the previous project's proposal when the project changes", async () => {
    getProposals.mockResolvedValue([pending]);
    const source = localSource();
    const { rerender } = renderCard(<FallbackCards project="a" canWrite />, source);
    expect(await screen.findByTestId("fallback-card")).toBeTruthy();

    let releaseB!: (v: unknown[]) => void;
    getProposals.mockReturnValue(new Promise((res) => (releaseB = res)));
    rerender(
      <DataSourceProvider source={source}>
        <FallbackCards project="b" canWrite />
      </DataSourceProvider>,
    );
    // While project b's fetch is in flight, project a's proposal must be gone.
    expect(screen.queryByTestId("fallback-card")).toBeNull();
    releaseB([]);
  });

  // Dismiss 按提案身份记：换了新提案要重新出现
  it("shows a new proposal after an earlier one was dismissed", async () => {
    vi.useFakeTimers();
    getProposals.mockResolvedValue([pending]);
    renderCard(<FallbackCards project="p" canWrite />);
    await vi.waitFor(() => expect(screen.queryByTestId("fallback-card")).not.toBeNull());
    fireEvent.click(screen.getByTestId("fallback-dismiss"));
    expect(screen.queryByTestId("fallback-card")).toBeNull();

    getProposals.mockResolvedValue([{ ...pending, task: "t2", toRole: "third" }]);
    await vi.advanceTimersByTimeAsync(10_000);
    await vi.waitFor(() => expect(screen.queryByTestId("fallback-card")).not.toBeNull());
    vi.useRealTimers();
  });

  // 但同一条提案仍在待批时，dismiss 必须一直生效（轮询不得把它推回来）
  it("keeps a dismissed proposal hidden while the server still lists it", async () => {
    vi.useFakeTimers();
    getProposals.mockResolvedValue([pending]);
    renderCard(<FallbackCards project="p" canWrite />);
    await vi.waitFor(() => expect(screen.queryByTestId("fallback-card")).not.toBeNull());
    fireEvent.click(screen.getByTestId("fallback-dismiss"));

    await vi.advanceTimersByTimeAsync(30_000);
    expect(screen.queryByTestId("fallback-card")).toBeNull();
    vi.useRealTimers();
  });

  // 读不到提案 → 按「无提案」处理，绝不邀请一次不确定的批准
  it("treats an unreadable proposal as none pending", async () => {
    getProposals.mockRejectedValue(new Error("network"));
    const { container } = renderCard(<FallbackCards project="p" canWrite />);
    await waitFor(() => expect(getProposals).toHaveBeenCalled());
    expect(container).toBeEmptyDOMElement();
  });
});

// The suite above mocks the DataSource, so it proves the component's behaviour
// but NOT that the component and the Go server agree on a wire shape — the exact
// blind spot that let FALLBACK-PAR ship a dashboard where escalations silently
// vanished. This block drives the REAL LocalServeSource over a stubbed fetch
// whose payloads are copied from internal/serve/fallback_proposal.go.
describe("FallbackCards ↔ serve wire contract", () => {
  afterEach(() => vi.restoreAllMocks());

  function stubServe(proposals: unknown[], onApprove?: (body: unknown) => Response) {
    const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/fallback-proposal")) {
        return { ok: true, status: 200, json: async () => ({ proposals }) };
      }
      if (url.endsWith("/fallback-proposal/approve")) {
        const body = JSON.parse(String(init?.body ?? "null"));
        return onApprove ? onApprove(body) : { ok: true, status: 202, json: async () => ({}) };
      }
      return { ok: false, status: 404, json: async () => ({}) };
    });
    vi.stubGlobal("fetch", fetchMock);
    return fetchMock;
  }

  it("renders one card per element of the server's {proposals: [...]}", async () => {
    stubServe([pending, other]);
    renderCard(<FallbackCards project="p1" canWrite />, new LocalServeSource());
    await waitFor(() => expect(screen.getAllByTestId("fallback-card")).toHaveLength(2));
  });

  it("posts the clicked card's task id to the approve endpoint", async () => {
    const seen: unknown[] = [];
    const fetchMock = stubServe([pending, other], (body) => {
      seen.push(body);
      return { ok: true, status: 202, json: async () => ({ status_url: "/x" }) } as unknown as Response;
    });
    renderCard(<FallbackCards project="p1" canWrite />, new LocalServeSource());
    await waitFor(() => expect(screen.getAllByTestId("fallback-card")).toHaveLength(2));

    fireEvent.click(
      within(screen.getAllByTestId("fallback-card")[1]).getByTestId("fallback-approve"),
    );
    await waitFor(() => expect(seen).toHaveLength(1));
    // Not "{}" — an empty body is a 404 the card would render as success.
    expect(seen[0]).toEqual({ task: "t2" });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/projects/p1/fallback-proposal/approve",
      expect.objectContaining({ method: "POST" }),
    );
  });
});
