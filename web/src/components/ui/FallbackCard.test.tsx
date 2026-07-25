import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { FallbackCard } from "./FallbackCard";
import { getFallbackProposal, approveFallback } from "../../lib/api";

vi.mock("../../lib/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../lib/api")>()),
  getFallbackProposal: vi.fn(),
  approveFallback: vi.fn(),
}));

const mockGet = vi.mocked(getFallbackProposal);
const mockApprove = vi.mocked(approveFallback);

const pending = {
  pending: true,
  task: "t1",
  seat: "w",
  fromRole: "primary",
  toRole: "backup",
  reason: "worker run: run timeout (--run-timeout) exceeded",
};

beforeEach(() => {
  vi.clearAllMocks();
});

describe("FallbackCard — approved state matrix", () => {
  // 无提案 → 不渲染（零视觉噪音）
  it("renders nothing when no proposal is pending", async () => {
    mockGet.mockResolvedValue({ pending: false });
    const { container } = render(<FallbackCard project="p" canWrite />);
    await waitFor(() => expect(mockGet).toHaveBeenCalled());
    expect(container).toBeEmptyDOMElement();
  });

  // 有提案 → 四要素 + 两个动作
  it("shows seat, from→to role and reason with both actions", async () => {
    mockGet.mockResolvedValue(pending);
    render(<FallbackCard project="p" canWrite />);
    expect(await screen.findByTestId("fallback-card")).toBeTruthy();
    const text = screen.getByTestId("fallback-card").textContent ?? "";
    expect(text).toContain("w"); // seat
    expect(text).toContain("primary"); // from role
    expect(text).toContain("backup"); // to role
    expect(text).toContain("run timeout"); // reason
    expect(screen.getByTestId("fallback-approve")).toBeTruthy();
    expect(screen.getByTestId("fallback-dismiss")).toBeTruthy();
  });

  // 提交中 → 按钮 disabled
  it("disables the action while approving", async () => {
    mockGet.mockResolvedValue(pending);
    let release!: () => void;
    mockApprove.mockReturnValue(
      new Promise((res) => {
        release = () => res({ status_url: "/s" });
      }),
    );
    render(<FallbackCard project="p" canWrite />);
    const btn = await screen.findByTestId("fallback-approve");
    fireEvent.click(btn);
    await waitFor(() => expect(screen.getByTestId("fallback-approve").hasAttribute("disabled")).toBe(true));
    release();
  });

  // 成功 → 卡片消失（提案已被消费）
  it("disappears after a successful approval", async () => {
    mockGet.mockResolvedValue(pending);
    mockApprove.mockResolvedValue({ status_url: "/s" });
    render(<FallbackCard project="p" canWrite />);
    fireEvent.click(await screen.findByTestId("fallback-approve"));
    await waitFor(() => expect(screen.queryByTestId("fallback-card")).toBeNull());
  });

  // 失败 → 行内错误，提案保留，可重试
  it("keeps the card and shows an inline error when approval fails", async () => {
    mockGet.mockResolvedValue(pending);
    mockApprove.mockRejectedValue(new Error("orchestrate is already running"));
    render(<FallbackCard project="p" canWrite />);
    fireEvent.click(await screen.findByTestId("fallback-approve"));
    expect(await screen.findByTestId("fallback-error")).toBeTruthy();
    expect(screen.getByTestId("fallback-error").textContent).toContain("already running");
    expect(screen.getByTestId("fallback-card")).toBeTruthy();
    expect(screen.getByTestId("fallback-approve").hasAttribute("disabled")).toBe(false);
  });

  // 错误里的 API 路径前缀是 writeJSON 的产物，对人是噪音 —— 只留服务端说的话
  it("strips the endpoint prefix from the error message", async () => {
    mockGet.mockResolvedValue(pending);
    mockApprove.mockRejectedValue(
      new Error("/api/projects/p/fallback-proposal/approve: orchestrate is already running"),
    );
    render(<FallbackCard project="p" canWrite />);
    fireEvent.click(await screen.findByTestId("fallback-approve"));
    const box = await screen.findByTestId("fallback-error");
    expect(box.textContent?.trim()).toBe("orchestrate is already running");
  });

  // 只读（hosted）→ 动作不可用并说明原因
  it("is read-only without write capability", async () => {
    mockGet.mockResolvedValue(pending);
    render(<FallbackCard project="p" canWrite={false} />);
    expect(await screen.findByTestId("fallback-card")).toBeTruthy();
    expect(screen.queryByTestId("fallback-approve")).toBeNull();
    expect(screen.getByTestId("fallback-readonly")).toBeTruthy();
  });

  // Dismiss 只关闭本次显示，不消费提案（提案仍在服务端待批）
  it("dismiss hides the card without approving", async () => {
    mockGet.mockResolvedValue(pending);
    render(<FallbackCard project="p" canWrite />);
    fireEvent.click(await screen.findByTestId("fallback-dismiss"));
    await waitFor(() => expect(screen.queryByTestId("fallback-card")).toBeNull());
    expect(mockApprove).not.toHaveBeenCalled();
  });
});
