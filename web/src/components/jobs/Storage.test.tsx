import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { Storage } from "./Storage";
import type { JobsSource } from "./source";

afterEach(cleanup);
const plan = {
  version: "plan-1", computedAt: "2026-10-21T00:00:00Z",
  policy: { olderThan: "336h", cacheMaxBytes: 5000000000, interval: "24h" },
  locations: [{ name: "jobs", bytes: 1000, reclaimable: 700, items: [
    { path: "jobs/old", bytes: 700, protected: false, reason: "finished resume chain older than retention" },
    { path: "jobs/active", bytes: 300, protected: true, reason: "running job" },
  ] }], staleWorktrees: ["stale-job"], failures: [], lastRun: null,
};
function fixture(readOnly = false) {
  const source: JobsSource = {
    jobsPath: "/jobs", costPath: "/cost",
    fetch: vi.fn(async () => Response.json(plan)),
    write: vi.fn(async () => Response.json({ endedAt: "now", removed: [{ location: "jobs", bytes: 700, items: 1 }], failures: [] })),
  };
  render(<Storage source={source} readOnly={readOnly} />);
  return source;
}
it("shows policy, sizes, stale worktrees and a copyable command in the read-only panel", async () => {
  const source = fixture(true);
  const region = screen.getByRole("region", { name: "Storage" });
  expect(await within(region).findByText(/5 GB/)).toBeInTheDocument();
  expect(within(region).getByText(/stale-job/)).toBeInTheDocument();
  expect(within(region).getByRole("button", { name: "Copy pactify sub prune --yes" })).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Clean up now" })).not.toBeInTheDocument();
  expect(source.write).not.toHaveBeenCalled();
});
it("loads a plan and requires confirmation before cleanup", async () => {
  const source = fixture();
  fireEvent.click(await screen.findByRole("button", { name: "Clean up now" }));
  const dialog = await screen.findByRole("dialog", { name: "Confirm storage cleanup" });
  expect(within(dialog).getByText(/jobs\/old/)).toBeInTheDocument();
  expect(within(dialog).getByText(/running job/)).toBeInTheDocument();
  expect(source.write).not.toHaveBeenCalled();
  fireEvent.click(within(dialog).getByRole("button", { name: "Confirm cleanup" }));
  expect(await screen.findByRole("status")).toHaveTextContent(/freed 700 B/i);
  expect(source.write).toHaveBeenCalledWith("/storage/prune", expect.objectContaining({ method: "POST", body: JSON.stringify({ version: "plan-1" }) }));
});
it("keeps failed cleanup visible and allows closing the plan without a write", async () => {
  const source = fixture();
  fireEvent.click(await screen.findByRole("button", { name: "Clean up now" }));
  fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Cancel" }));
  expect(source.write).not.toHaveBeenCalled();
});

it("presents a native database sweep as session cleanup", async () => {
  const nativePlan = { ...plan, locations: [{ name: "harness-homes", bytes: 1000, reclaimable: 0, items: [{ path: "harness-homes/opencode/share/opencode/opencode.db", bytes: 1000, protected: false, reason: "native orphan-session sweep; reclaimable bytes unknown", action: "sweep-sessions" }] }] };
  const source: JobsSource = { jobsPath: "/jobs", costPath: "/cost", fetch: vi.fn(async () => Response.json(nativePlan)), write: vi.fn() };
  render(<Storage source={source} />);
  fireEvent.click(await screen.findByRole("button", { name: "Clean up now" }));
  const dialog = await screen.findByRole("dialog");
  expect(within(dialog).getByText("Clean orphan sessions")).toBeInTheDocument();
  expect(within(dialog).queryByText("Remove", { exact: true })).not.toBeInTheDocument();
});

it("summarizes large cleanup plans with at most five examples per reason", async () => {
  const large = { ...plan, locations: [{ name: "cache", bytes: 30000, reclaimable: 30000, items: Array.from({ length: 1000 }, (_, i) => ({ path: `cache/entry-${i}`, bytes: 30, protected: false, reason: "cache LRU eviction" })) }] };
  const source: JobsSource = { jobsPath: "/jobs", costPath: "/cost", fetch: vi.fn(async () => Response.json(large)), write: vi.fn() };
  render(<Storage source={source} />);
  fireEvent.click(await screen.findByRole("button", { name: "Clean up now" }));
  const dialog = await screen.findByRole("dialog");
  expect(within(dialog).getAllByRole("listitem").length).toBeLessThanOrEqual(5);
  expect(within(dialog).getByRole("heading", { name: /1,000 items/ })).toBeInTheDocument();
});

it("labels deleted sessions and measured database deltas", async () => {
  const value = { ...plan, lastRun: { startedAt: "now", endedAt: "now", failures: [], removed: [{ location: "harness-homes", items: 2, sessions: 2, bytes: 1000, databaseBytes: 1000 }] } };
  const source: JobsSource = { jobsPath: "/jobs", costPath: "/cost", fetch: vi.fn(async () => Response.json(value)) };
  render(<Storage source={source} readOnly />);
  expect(await screen.findByText(/2 sessions deleted/)).toHaveTextContent(/database size delta: 1 KB/);
});
it("formats retention durations and item counts for people", async () => {
  const { storageDuration, storageItems } = await import("./Storage");
  expect(storageDuration("336h")).toBe("14 days");
  expect(storageDuration("24h")).toBe("1 day");
  expect(storageDuration("36h")).toBe("36 hours");
  expect(storageDuration("1h")).toBe("1 hour");
  expect(storageDuration("90m")).toBe("90m");
  expect(storageItems(1)).toBe("1 item");
  expect(storageItems(0)).toBe("0 items");
  expect(storageItems(2708)).toBe("2,708 items");
  fixture(true);
  const region = screen.getByRole("region", { name: "Storage" });
  expect(await within(region).findByText(/Retention: 14 days/)).toBeInTheDocument();
  expect(within(region).getByText(/interval: 1 day/)).toBeInTheDocument();
});

it("shows location rows and a usage bar only when a real cap exists", async () => {
  const data = {...plan, locations:[
    {name:"cache",bytes:4800000000,reclaimable:200000000,breakdown:{npm:100000000}},
    {name:"jobs",bytes:1000,reclaimable:700},
  ]};
  render(<Storage embedded readOnly source={{jobsPath:"/jobs",costPath:"/cost",fetch:vi.fn(async()=>Response.json(data))}}/>);
  const cache = await screen.findByRole("meter", {name:"cache usage"});
  expect(cache).toHaveAttribute("aria-valuemax", "5000000000");
  expect(cache).toHaveAttribute("aria-valuenow", "4800000000");
  expect(screen.queryByRole("meter", {name:"jobs usage"})).toBeNull();
  expect(screen.getByText("npm: 100 MB")).toBeInTheDocument();
  expect(screen.queryByRole("heading", {name:"Storage"})).toBeNull();
  expect(screen.getByRole("heading", {name:"Locations"})).toBeInTheDocument();
});
