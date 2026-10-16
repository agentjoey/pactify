import { act, cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { UsageCard } from "./UsageCard";
import { dashboardJobsSource } from "./source";

const totals = {
  jobs: 2, succeeded: 1, failed: 1, canceled: 0, retries: 0, fallbacks: 0,
  inputTokens: 12345, outputTokens: 2345, costUSD: null,
  costReportedJobs: 0, subscriptionJobs: 2, unpricedJobs: 0, usagePartialJobs: 1,
};
const report = {
  since: "2026-10-10T00:00:00Z", until: "2026-10-16T12:00:00Z", by: "harness",
  groups: [
    { key: "codex", ...totals },
    { key: "opencode", ...totals, costUSD: 0.125, costReportedJobs: 2, subscriptionJobs: 0 },
    { key: "custom", ...totals, subscriptionJobs: 0, unpricedJobs: 2 },
  ],
  total: { ...totals, jobs: 6, succeeded: 3, failed: 3, inputTokens: 37035, outputTokens: 7035, costUSD: 0.125 as number | null, costReportedJobs: 2, unpricedJobs: 2, usagePartialJobs: 3 },
  skipped: { missingResult: 0, invalidResult: 0, invalidState: 0 }, quotaSnapshots: [],
  exhaustedRoutes: [
    { harness: "codex", model: "gpt-5", resetAt: "2026-10-16T14:30:00Z", recordedAt: "2026-10-16T12:00:00Z" },
    { harness: "agy", resetAt: null, recordedAt: "2026-10-16T12:00:00Z" },
  ],
};
beforeEach(() => {
  history.replaceState(null, "", "/");
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-10-16T12:00:00Z"));
});
afterEach(() => {
  cleanup(); vi.unstubAllGlobals(); vi.useRealTimers(); sessionStorage.clear();
});
async function show(value = report) {
  const fetcher = vi.fn(async (_path: string, _options?: RequestInit) => new Response(JSON.stringify(value)));
  vi.stubGlobal("fetch", fetcher);
  const view = render(<UsageCard source={dashboardJobsSource} />);
  await act(async () => {});
  return { ...view, fetcher };
}
it("renders harness jobs, formatted tokens, dollar, subscription and unknown costs with a total", async () => {
  await show();
  const card = screen.getByRole("region", { name: "Usage · last 7 days" });
  const codex = within(card).getByRole("row", { name: /codex/ });
  expect(codex).toHaveTextContent("2 (1 ok / 1 failed)");
  expect(codex).toHaveTextContent("12,345");
  expect(codex).toHaveTextContent("2,345");
  expect(codex).toHaveTextContent("subscription");
  expect(within(card).getByRole("row", { name: /opencode/ })).toHaveTextContent("$0.13");
  expect(within(card).getByRole("row", { name: /custom/ })).toHaveTextContent("unknown");
  const total = within(card).getByRole("row", { name: /Total/ });
  expect(total).toHaveTextContent("6 (3 ok / 3 failed)");
  expect(total).toHaveTextContent("37,035");
  expect(total).toHaveTextContent("$0.13 + subscription + unknown");
  expect(card).toHaveTextContent("* includes 3 jobs with partial usage");
  expect(card).toHaveTextContent("Exhausted: codex/gpt-5 (resets in 2h 30m)");
  expect(card).toHaveTextContent("Exhausted: agy (resets in unknown)");
});
it("formats explicit zero as sub-cent, keeps empty totals, and omits absent partial/quota notes", async () => {
  await show({ ...report, groups: [{ ...report.groups[1], costUSD: 0 }], total: { ...report.total, usagePartialJobs: 0 }, exhaustedRoutes: [] });
  expect(screen.getByRole("row", { name: /opencode/ })).toHaveTextContent("<$0.01");
  expect(screen.queryByText(/includes .*partial/)).not.toBeInTheDocument();
  expect(screen.queryByText(/Exhausted:/)).not.toBeInTheDocument();
  cleanup();
  await show({ ...report, groups: [], total: { ...totals, jobs: 0, succeeded: 0, failed: 0, inputTokens: 0, outputTokens: 0, subscriptionJobs: 0, usagePartialJobs: 0 }, exhaustedRoutes: [] });
  expect(within(screen.getByRole("row", { name: /Total/ })).getAllByRole("cell").map(cell => cell.textContent)).toEqual(["0 (0 ok / 0 failed)", "0", "0", "—"]);
});
it("polls every 30 seconds, retries failures, and aborts on unmount", async () => {
  const { fetcher, unmount } = await show();
  expect(fetcher).toHaveBeenCalledWith("/api/sub/cost?since=7d&by=harness", expect.objectContaining({ credentials: "same-origin" }));
  await act(async () => { await vi.advanceTimersByTimeAsync(29999); });
  expect(fetcher).toHaveBeenCalledTimes(1);
  fetcher.mockRejectedValueOnce(new Error("offline"));
  await act(async () => { await vi.advanceTimersByTimeAsync(1); });
  expect(screen.getByText("Usage unavailable")).toBeInTheDocument();
  expect(screen.queryByRole("table")).not.toBeInTheDocument();
  await act(async () => { await vi.advanceTimersByTimeAsync(30000); });
  expect(screen.queryByText("Usage unavailable")).not.toBeInTheDocument();
  const signal = fetcher.mock.calls[0][1]?.signal;
  unmount();
  expect(signal?.aborted).toBe(true);
  await act(async () => { await vi.advanceTimersByTimeAsync(30000); });
  expect(fetcher).toHaveBeenCalledTimes(3);
});
it("shows a loading state and handles malformed responses as unavailable", async () => {
  let resolve!: (response: Response) => void;
  vi.stubGlobal("fetch", vi.fn(() => new Promise<Response>(done => { resolve = done; })));
  render(<UsageCard source={dashboardJobsSource} />);
  expect(screen.getByText("Loading usage…")).toBeInTheDocument();
  await act(async () => { resolve(new Response("[]")); });
  expect(screen.getByText("Usage unavailable")).toBeInTheDocument();
});
