import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { Dashboard } from "./Dashboard";
import { JobCard } from "./Jobs";
import { JobDetail } from "./JobDetail";
import type { Job } from "./types";
import { seedJobs } from "../../../e2e/jobs-fixtures.mjs";
import * as stats from "./statistics";
import { Routing } from "./Routing";
import type { JobsSource } from "./source";
const report = {
  groups: [
    {
      key: "codex",
      jobs: 2,
      succeeded: 1,
      failed: 1,
      inputTokens: 12345,
      outputTokens: 6789,
      costUSD: 0.125,
      subscriptionJobs: 0,
      unpricedJobs: 0,
      usagePartialJobs: 0,
    },
  ],
  total: { usagePartialJobs: 0 },
  exhaustedRoutes: [
    {
      key: "codex/gpt-test",
      harness: "codex",
      model: "gpt-test",
      resetAt: "2027-01-01T02:00:00Z",
    },
  ],
  quotaSnapshots: [
    {
      harness: "codex",
      jobId: "a",
      observedAt: "2026-12-31T23:00:00Z",
      rateLimit:
        "5-hour window: 20% used, resets 01-01 02:00; 7-day window: 35% used, resets 01-05 01:00",
    },
  ],
};
const source: JobsSource = {
  jobsPath: "/jobs",
  costPath: "/cost",
  routerPath: "/router",
  fetch: async () => new Response(JSON.stringify(report)),
};
afterEach(cleanup);
it("uses one cost format for null, zero, sub-cent, boundary and rounded costs", () => {
  expect(stats.formatCost(null)).toBe("—");
  expect(stats.formatCost(undefined)).toBe("—");
  for (const n of [0, 0.0001, 0.00999])
    expect(stats.formatCost(n)).toBe("<$0.01");
  expect(stats.formatCost(0.01)).toBe("$0.01");
  expect(stats.formatCost(0.125)).toBe("$0.13");
  expect(stats.formatCost(12)).toBe("$12.00");
});
it("parses actual codex used windows, remaining windows and year rollover without inventing missing data", () => {
  const observed = new Date(2026, 11, 31, 23).toISOString();
  const windows = stats.parseQuotaWindows(
    report.quotaSnapshots[0].rateLimit,
    observed,
  );
  expect(windows.map((w) => [w.label, w.remaining])).toEqual([
    ["5h", 80],
    ["7d", 65],
  ]);
  expect(new Date(windows[0].resetAt!).getFullYear()).toBe(2027);
  expect(stats.parseQuotaWindows("5h: 80% remaining", observed)).toEqual([
    { label: "5h", remaining: 80, resetAt: null },
  ]);
  for (const text of ["", "Unknown quota", "5h: 150% used"])
    expect(stats.parseQuotaWindows(text, observed)).toEqual([]);
});
it("renders labeled grid lines with unscaled HTML axis labels", async () => {
  const { container } = render(
    <Dashboard source={source} jobs={[]} jobsUnavailable={false} />,
  );
  await screen.findByRole("region", { name: "By harness usage" });
  expect(container.querySelectorAll(".chart-gridline")).toHaveLength(4);
  expect(container.querySelectorAll(".chart-y-axis .chart-label")).toHaveLength(
    5,
  );
  expect(container.querySelectorAll("svg text")).toHaveLength(0);
  expect(container.querySelectorAll(".chart-x-axis .chart-label")).toHaveLength(
    14,
  );
});
it("provides a mobile scroll hint associated with the complete harness table", async () => {
  render(<Dashboard source={source} jobs={[]} jobsUnavailable={false} />);
  const region = await screen.findByRole("region", {
    name: "By harness usage",
  });
  expect(region).toHaveAttribute("aria-describedby", "harness-scroll-hint");
  expect(
    screen.getByText("Scroll horizontally to see tokens and cost →"),
  ).toBeInTheDocument();
  const row = within(region).getByRole("row", { name: /codex/ });
  expect(row).toHaveTextContent("12,345");
  expect(row).toHaveTextContent("6,789");
  expect(row).toHaveTextContent("$0.13");
});
it("renders quota routes with exact times and a progress bar for each codex window", async () => {
  render(<Dashboard source={source} jobs={[]} jobsUnavailable={false} />);
  const progress = await screen.findAllByRole("progressbar");
  expect(progress).toHaveLength(2);
  expect(
    screen.getByRole("progressbar", { name: "codex 5h remaining" }),
  ).toHaveAttribute("value", "80");
  expect(
    screen.getByRole("progressbar", { name: "codex 7d remaining" }),
  ).toHaveAttribute("value", "65");
  expect(screen.getByText(/80% remaining · resets/)).toBeInTheDocument();
  expect(
    screen
      .getByText("codex/gpt-test")
      .closest(".routing-read-row")
      ?.querySelector("time"),
  ).toHaveAttribute("datetime", report.exhaustedRoutes[0].resetAt);
});
it("shows a single quiet quota empty state", async () => {
  render(
    <Dashboard
      source={{
        ...source,
        fetch: async () =>
          new Response(
            JSON.stringify({
              ...report,
              exhaustedRoutes: [],
              quotaSnapshots: [],
            }),
          ),
      }}
      jobs={[]}
      jobsUnavailable={false}
    />,
  );
  expect(await screen.findByText("No quota data")).toHaveClass("panel-caption");
});
it("puts Routing, its path and Edit in a shared title row", async () => {
  render(
    <Routing
      expanded
      source={{
        ...source,
        write: async () => new Response("{}"),
        fetch: async () =>
          new Response(
            JSON.stringify({
              path: "/tmp/router.json",
              exists: true,
              version: "1",
              config: { aliases: {}, fallback: {}, quotaGroups: {} },
              exhaustedRoutes: [],
            }),
          ),
      }}
    />,
  );
  const edit = await screen.findByRole("button", { name: "Edit" });
  const header = edit.closest(".routing-heading");
  expect(header).not.toBeNull();
  expect(
    within(header as HTMLElement).getByRole("heading", { name: "Routing" }),
  ).toBeInTheDocument();
  expect(
    within(header as HTMLElement).getByText("/tmp/router.json"),
  ).toBeInTheDocument();
});

for (const [costUSD, expected] of [
  [0.125, "$0.13"],
  [0.001, "<$0.01"],
] as const) {
  it(`formats ${costUSD} consistently in cards and detail usage`, async () => {
    const job = { ...seedJobs()[0], costUSD } as Job;
    const noop = () => {};
    render(
      <JobCard chain={{ root: job, latest: job, runs: [job], rework: 0 }} />,
    );
    expect(screen.getByText(expected)).toBeInTheDocument();
    cleanup();
    render(
      <JobDetail
        id={job.id}
        jobs={[job]}
        source={{
          ...source,
          fetch: async () =>
            new Response(
              JSON.stringify({
                job,
                brief: "Brief",
                markdown: "Result",
                result: { costUSD },
              }),
            ),
        }}
        readOnly={false}
        onChanged={noop}
        notify={noop}
        initialTab="summary"
      />,
    );
    expect(await screen.findAllByText(expected)).toHaveLength(2);
  });
}
