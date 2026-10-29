import { afterEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { groupLines, type LineGrouping } from "./pipeline";
import { JobList } from "./Jobs";
import type { Job } from "./types";

const now = Date.parse("2026-10-29T12:00:00Z");
const ago = (days: number) => new Date(now - days * 86400000).toISOString();
const job = (id: string, extra: Partial<Job> = {}): Job => ({
  id, title: id, repo: `/repo/${id}`, harness: id, status: "succeeded",
  modelRequested: "model", cwd: "/repo", access: "write", configMode: "minimal",
  duration: "1m", createdAt: ago(10), inputTokens: 0, outputTokens: 0,
  attention: [], resumeOf: "", resumedBy: [], ...extra,
});
const fixtures = () => [
  job("quiet-old", { finishedAt: ago(12) }),
  job("recent", { finishedAt: ago(0.1) }),
  job("needs-you", { attention: [{ kind: "failed", message: "Job failed" }], status: "failed" }),
  job("running", { status: "running", startedAt: ago(2) }),
  job("quiet-new", { finishedAt: ago(4) }),
];
afterEach(() => { cleanup(); vi.restoreAllMocks(); localStorage.clear(); history.replaceState(null, "", "/"); });

for (const grouping of ["project", "harness"] as LineGrouping[]) {
  it(`orders ${grouping} lines by current work, recent activity and quiet activity`, () => {
    const rows = fixtures();
    const lines = groupLines(rows, grouping, now);
    expect(lines.map(line => line.name)).toEqual(["running", "needs-you", "recent", "quiet-new", "quiet-old"]);
    expect(lines.map(line => line.active)).toEqual([true, true, true, false, false]);
    expect(lines[0].latestActivity).toBe(Date.parse(ago(2)));
    expect(rows.map(row => row.id)).toEqual(fixtures().map(row => row.id));
  });
}
it("uses created, started and ended activity with an explicit three-day boundary", () => {
  const lines = groupLines([
    job("created", { createdAt: ago(1) }), job("started", { startedAt: ago(2) }),
    job("boundary", { finishedAt: ago(3) }), job("invalid", { createdAt: "invalid" }),
    job("queued", { status: "queued" }), job("retrying", { status: "retrying" }),
  ], "project", now);
  expect(lines.filter(line => line.active).map(line => line.name).sort()).toEqual(["created", "queued", "retrying", "started"]);
  expect(groupLines(fixtures(), "one", now)).toMatchObject([{ name: "All jobs", active: true }]);
  expect(groupLines([job("old")], "one", now)[0].active).toBe(false);
  expect(groupLines([], "one", now)).toEqual([]);
});

it("collapses quiet lines and expands individual lines and all lines in place", () => {
  vi.spyOn(Date, "now").mockReturnValue(now);
  render(<JobList jobs={fixtures()} />);
  expect(screen.getByRole("button", { name: "Active" })).toHaveAttribute("aria-pressed", "true");
  expect(screen.getByText("Quiet lines · 2")).toBeInTheDocument();
  expect(screen.getByText("Nothing running or waiting, last activity 3+ days ago")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "needs-you" })).toBeInTheDocument();
  expect(screen.queryByRole("link", { name: "quiet-new" })).toBeNull();
  const chip = screen.getByRole("button", { name: "quiet-new · 4d ago" });
  fireEvent.click(chip);
  expect(chip).toHaveAttribute("aria-expanded", "true");
  expect(screen.getByRole("link", { name: "quiet-new" })).toBeInTheDocument();
  expect(screen.queryByRole("link", { name: "quiet-old" })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Show quiet lines" }));
  expect(screen.getByRole("link", { name: "quiet-old" })).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Hide quiet lines" }));
  expect(screen.queryByRole("link", { name: "quiet-new" })).toBeNull();
});
it("persists All lines and restores it without a quiet section", () => {
  vi.spyOn(Date, "now").mockReturnValue(now);
  const view = render(<JobList jobs={fixtures()} />);
  fireEvent.click(screen.getByRole("button", { name: "All lines" }));
  expect(screen.queryByText(/Quiet lines ·/)).toBeNull();
  expect(screen.getByRole("link", { name: "quiet-old" })).toBeInTheDocument();
  view.unmount(); render(<JobList jobs={fixtures()} />);
  expect(screen.getByRole("button", { name: "All lines" })).toHaveAttribute("aria-pressed", "true");
  fireEvent.click(screen.getByRole("button", { name: "Active" }));
  expect(screen.getByText("Quiet lines · 2")).toBeInTheDocument();
});
it("falls back to Active when storage is unavailable or contains an invalid choice", () => {
  vi.spyOn(Date, "now").mockReturnValue(now);
  vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("disabled"); });
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("disabled"); });
  const view = render(<JobList jobs={fixtures()} />);
  expect(screen.getByRole("button", { name: "Active" })).toHaveAttribute("aria-pressed", "true");
  fireEvent.click(screen.getByRole("button", { name: "All lines" }));
  view.unmount();
  vi.restoreAllMocks();
  vi.spyOn(Date, "now").mockReturnValue(now);
  localStorage.setItem("pactify-jobs-lines", "invalid");
  render(<JobList jobs={fixtures()} />);
  expect(screen.getByRole("button", { name: "Active" })).toHaveAttribute("aria-pressed", "true");
});
for (const query of ["search=quiet", "repo=%2Frepo%2Fquiet-old", "harness=quiet-old", "status=completed"]) {
  it(`reveals matching quiet lines for ${query}`, () => {
    vi.spyOn(Date, "now").mockReturnValue(now);
    history.replaceState(null, "", `/#/jobs?${query}`);
    render(<JobList jobs={fixtures()} />);
    expect(screen.getByRole("link", { name: "quiet-old" })).toBeInTheDocument();
    expect(screen.queryByText(/Quiet lines ·/)).toBeNull();
  });
}
it("confirms Dismiss, posts once, and refreshes the card out of Needs you", async () => {
  const initial = job("failed", { createdAt: new Date().toISOString(), status: "failed", actions: ["dismiss"], attention: [{ kind: "failed", message: "Job failed" }] });
  const write = vi.fn().mockResolvedValue(new Response('{"ok":true}', { status: 200 }));
  const changed = vi.fn();
  const props = { source: { jobsPath: "/api/sub/jobs", costPath: "/cost", fetch: vi.fn(), write }, onChanged: changed };
  const view = render(<JobList jobs={[initial]} {...props} />);
  fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
  expect(screen.getByText("Dismiss this job? It stays in history but leaves Needs you.")).toBeInTheDocument();
  expect(write).not.toHaveBeenCalled();
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Confirm Dismiss" }));
  await vi.waitFor(() => expect(changed).toHaveBeenCalledOnce());
  expect(write).toHaveBeenCalledWith("/api/sub/jobs/failed/dismiss", expect.objectContaining({ method: "POST", body: "{}" }));
  view.rerender(<JobList jobs={[{ ...initial, attention: [], actions: [], dismissedAt: new Date().toISOString() }]} {...props} />);
  expect(screen.queryByRole("button", { name: "Dismiss" })).toBeNull();
  expect(screen.getByRole("region", { name: "Shipped" })).toContainElement(screen.getByRole("link", { name: "failed" }));
  view.rerender(<JobList jobs={[initial]} {...props} readOnly />);
  expect(screen.queryByRole("button", { name: "Dismiss" })).toBeNull();
});
it("keeps station keyboard navigation inside an expanded quiet line", () => {
  vi.spyOn(Date, "now").mockReturnValue(now);
  const { container } = render(<JobList jobs={fixtures()} />);
  fireEvent.click(screen.getByRole("button", { name: "quiet-new · 4d ago" }));
  const boards = container.querySelectorAll<HTMLElement>(".pipeline-scroll");
  const mainScroll = vi.fn(), quietScroll = vi.fn();
  boards[0].scrollTo = mainScroll;
  boards[1].scrollTo = quietScroll;
  screen.getByRole("link", { name: "quiet-new" }).focus();
  fireEvent.keyDown(document.activeElement!, { key: "h" });
  expect(quietScroll).toHaveBeenCalledOnce();
  expect(mainScroll).not.toHaveBeenCalled();
});
