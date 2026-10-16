import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { JobList, Jobs, type Job } from "./Jobs";
import {
  sectionFor,
  completedDays,
  dashboardKpis,
  dailyTokens,
} from "./statistics";
import { JobActions } from "./JobActions";
import { Settings } from "./Settings";
import { Dashboard } from "./Dashboard";
import App from "../../App";

export const makeJob = (id: string, extra: Partial<Job> = {}): Job => ({
  id,
  title: `AJ-705 ${id}`,
  harness: "codex",
  modelRequested: "model",
  repo: "/repo/pactify",
  cwd: "/repo",
  access: "write",
  configMode: "minimal",
  status: "succeeded",
  duration: "1m",
  createdAt: new Date().toISOString(),
  finishedAt: new Date().toISOString(),
  inputTokens: 1000,
  outputTokens: 200,
  attention: [],
  resumeOf: "",
  resumedBy: [],
  actions: [],
  ...extra,
});
const snapshot = {
  version: "v1",
  path: "/router.json",
  exists: true,
  exhaustedRoutes: [],
  config: {
    defaults: { codex: { model: "old", effort: "low" } },
    aliases: { fast: { harness: "codex" } },
    fallback: { codex: ["kimi"] },
    quotaGroups: {},
  },
};
const harness = {
  name: "codex",
  installed: true,
  version: "2",
  verifiedVersion: "1",
  readOnly: true,
  experimental: false,
  models: { items: ["old", "new"], error: "", fetchedAt: "" },
  efforts: ["low", "high"],
  default: { model: "old" },
};
function source(rows: Job[] = [], status = 200) {
  const write = vi.fn(
    async (_path: string, _init: RequestInit) =>
      new Response(
        JSON.stringify(
          status === 422
            ? {
                errors: [
                  { field: "defaults.codex.model", message: "Invalid model" },
                ],
              }
            : status === 409
              ? { error: "Changed elsewhere" }
              : { jobId: "next" },
        ),
        { status },
      ),
  );
  const fetcher = vi.fn(
    async (path: string) =>
      new Response(
        JSON.stringify(
          path.includes("harnesses")
            ? { harnesses: [harness] }
            : path.includes("router")
              ? snapshot
              : rows,
        ),
      ),
  );
  return {
    jobsPath: "/jobs",
    costPath: "/cost",
    routerPath: "/router",
    harnessesPath: "/harnesses",
    fetch: fetcher,
    write,
  };
}
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.useRealTimers();
  history.replaceState(null, "", "/");
});

it("partitions running, queued, terminal attention and completed independently", () => {
  expect(
    sectionFor(
      makeJob("a", {
        status: "retrying",
        attention: [{ kind: "failed", message: "old" }],
      }),
    ),
  ).toBe("progress");
  expect(sectionFor(makeJob("a", { status: "queued" }))).toBe("queued");
  expect(
    sectionFor(
      makeJob("a", { attention: [{ kind: "warnings", message: "Review" }] }),
    ),
  ).toBe("attention");
  expect(sectionFor(makeJob("a", { status: "failed" }))).toBe("completed");
});
it("groups by local completion day, stacks cards and expands and collapses", () => {
  const rows = ["a", "b", "c", "d"].map((id) => makeJob(id));
  expect(
    completedDays(
      rows.map((root) => ({ root, latest: root, runs: [root], rework: 0 })),
    ),
  ).toHaveLength(1);
  render(<JobList jobs={rows} />);
  expect(screen.getByText("Today · 4 completed")).toBeInTheDocument();
  expect(screen.getAllByRole("article")).toHaveLength(3);
  fireEvent.click(screen.getByRole("button", { name: "Show all" }));
  expect(screen.getAllByRole("article")).toHaveLength(4);
  fireEvent.click(screen.getByRole("button", { name: "Collapse" }));
  expect(screen.getAllByRole("article")).toHaveLength(3);
});
it("hides empty section headings and shows the global empty state", () => {
  render(<JobList jobs={[]} />);
  expect(screen.getByText("No delegation jobs yet")).toBeInTheDocument();
  expect(screen.queryByRole("heading", { name: "Queued" })).toBeNull();
});
it("persists project, status and title search in the URL without splitting chains", () => {
  render(
    <JobList
      jobs={[
        makeJob("a", { resumedBy: ["b"] }),
        makeJob("b", { status: "running", harness: "kimi", resumeOf: "a" }),
      ]}
    />,
  );
  fireEvent.change(screen.getByLabelText("Harness"), {
    target: { value: "codex" },
  });
  fireEvent.change(screen.getByPlaceholderText("Search jobs"), {
    target: { value: "AJ-705 a" },
  });
  expect(screen.getAllByRole("article")).toHaveLength(1);
  expect(location.hash).toContain("harness=codex");
  expect(location.hash).toContain("search=AJ-705+a");
});
for (const action of ["cancel", "apply", "discard"] as const) {
  it(`${action} renders only server actions, confirms consequences and refreshes after success`, async () => {
    const src = source();
    const changed = vi.fn();
    render(
      <JobActions
        job={makeJob("a", { actions: [action] })}
        source={src}
        onChanged={changed}
        notify={vi.fn()}
      />,
    );
    const label = action[0].toUpperCase() + action.slice(1);
    expect(screen.queryByRole("button", { name: "Resume" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: label }));
    expect(src.write).not.toHaveBeenCalled();
    const dialog = screen.getByRole("dialog");
    expect(dialog.textContent).toMatch(/stop|changes|worktree/i);
    fireEvent.click(
      within(dialog).getByRole("button", { name: `Confirm ${label}` }),
    );
    await act(async () => {});
    expect(src.write).toHaveBeenCalledWith(
      `/jobs/a/${action}`,
      expect.objectContaining({ method: "POST" }),
    );
    expect(changed).toHaveBeenCalled();
  });
}
it("resume sends multiline instructions, shows the new id and refreshes", async () => {
  const src = source();
  const notify = vi.fn();
  const changed = vi.fn();
  render(
    <JobActions
      job={makeJob("a", { actions: ["resume"] })}
      source={src}
      onChanged={changed}
      notify={notify}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Resume" }));
  expect(
    screen.getByRole("dialog", { name: "Resume AJ-705 a" }),
  ).toBeInTheDocument();
  fireEvent.change(screen.getByLabelText("Instructions for the next run"), {
    target: { value: "Fix\nthen test" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Start" }));
  await act(async () => {});
  expect(JSON.parse(src.write.mock.calls[0][1].body as string)).toEqual({
    brief: "Fix\nthen test",
  });
  expect(notify).toHaveBeenCalledWith("Started next");
  expect(changed).toHaveBeenCalled();
});
for (const action of ["cancel", "resume", "apply", "discard"])
  it(`${action} displays backend failures`, async () => {
    const src = source();
    src.write.mockImplementation(
      async () =>
        new Response(JSON.stringify({ error: "Worktree conflict" }), {
          status: 409,
        }),
    );
    const notify = vi.fn();
    render(
      <JobActions
        job={makeJob("a", { actions: [action] })}
        source={src}
        notify={notify}
        onChanged={vi.fn()}
      />,
    );
    fireEvent.click(
      screen.getByRole("button", {
        name: action[0].toUpperCase() + action.slice(1),
      }),
    );
    if (action === "resume")
      fireEvent.change(screen.getByLabelText("Instructions for the next run"), {
        target: { value: "retry" },
      });
    fireEvent.click(
      within(screen.getByRole("dialog")).getByRole("button", {
        name:
          action === "resume"
            ? "Start"
            : `Confirm ${action[0].toUpperCase() + action.slice(1)}`,
      }),
    );
    await act(async () => {});
    expect(notify).toHaveBeenCalledWith("Worktree conflict");
  });
it("opens a tabbed deep-linked modal, switches runs, streams logs, closes with Escape", async () => {
  const rows = [
    makeJob("a", { resumedBy: ["b"] }),
    makeJob("b", { resumeOf: "a" }),
  ];
  const src = source(rows);
  src.fetch.mockImplementation(async (path) => {
    if (path.endsWith("/logs"))
      return new Response('data: "hello"\n\nevent: done\ndata: {}\n\n');
    if (path.endsWith("/diff"))
      return new Response(
        JSON.stringify({ patch: "+changed", files: [], note: "" }),
      );
    const row = rows.find((row) => path.endsWith("/" + row.id));
    return new Response(
      JSON.stringify(
        row
          ? {
              job: row,
              brief: `Brief ${row.id}`,
              markdown: "Summary result",
              result: { warnings: ["Check this"] },
            }
          : rows,
      ),
    );
  });
  history.replaceState(null, "", "/#/jobs/b");
  render(<Jobs source={src} />);
  const dialog = await screen.findByRole("dialog", { name: "Job details" });
  await within(dialog).findByText("Summary result");
  for (const name of ["Summary", "Logs", "Diff", "Brief"])
    expect(within(dialog).getByRole("tab", { name })).toBeInTheDocument();
  fireEvent.click(within(dialog).getByRole("tab", { name: "Logs" }));
  await within(dialog).findByText("hello");
  fireEvent.click(within(dialog).getByRole("tab", { name: "Diff" }));
  await within(dialog).findByText("+changed");
  fireEvent.click(within(dialog).getByRole("tab", { name: "Brief" }));
  expect(within(dialog).getByText("Brief b")).toBeInTheDocument();
  fireEvent.click(within(dialog).getByRole("tab", { name: "Summary" }));
  fireEvent.click(within(dialog).getByRole("link", { name: /a ·/ }));
  await act(async () => {
    window.dispatchEvent(new HashChangeEvent("hashchange"));
  });
  await within(dialog).findByText("a · succeeded · codex · 1m");
  fireEvent.keyDown(document, { key: "Escape" });
  await act(async () => {
    window.dispatchEvent(new HashChangeEvent("hashchange"));
  });
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(location.hash).toBe("#/jobs");
});
it("calculates chain KPIs and fourteen calendar days without dropping separate-run usage", () => {
  const now = new Date("2026-10-16T12:00:00");
  const rows = [
    makeJob("a", {
      createdAt: now.toISOString(),
      finishedAt: now.toISOString(),
      resumedBy: ["b"],
    }),
    makeJob("b", {
      createdAt: now.toISOString(),
      // A running job has no finish time; the fixture default is the wall clock.
      finishedAt: undefined,
      status: "running",
      resumeOf: "a",
      activity: { summary: "", inputTokens: 50, outputTokens: 5 },
    }),
    makeJob("c", {
      createdAt: "2026-01-01T00:00:00Z",
      finishedAt: "2026-01-01T00:00:00Z",
      status: "failed",
    }),
  ];
  expect(dashboardKpis(rows, now)).toEqual({
    progress: 1,
    queued: 0,
    attention: 0,
    succeeded: 0,
    failed: 0,
  });
  const days = dailyTokens(rows, now);
  expect(days).toHaveLength(14);
  expect(days[13].harnesses.codex).toBe(1255);
});
it("keeps Dashboard cards independent when cost is unavailable", async () => {
  const src = source();
  src.fetch.mockImplementation(async () => {
    throw Error("offline");
  });
  render(
    <Dashboard
      source={src}
      jobs={[makeJob("a", { status: "running" })]}
      jobsUnavailable={false}
    />,
  );
  expect(screen.getByLabelText("In progress")).toHaveTextContent("1");
  expect(
    screen.getByRole("img", { name: "Token usage · last 14 days" }),
  ).toBeInTheDocument();
  await screen.findAllByText("Unavailable");
});
it("saves only defaults while preserving aliases, fallbacks, groups and version", async () => {
  const src = source();
  render(<Settings source={src} />);
  await screen.findByLabelText("codex Default model");
  fireEvent.change(screen.getByLabelText("codex Default model"), {
    target: { value: "new" },
  });
  fireEvent.change(screen.getByLabelText("codex Effort"), {
    target: { value: "high" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save defaults" }));
  await act(async () => {});
  const payload = JSON.parse(src.write.mock.calls[0][1].body as string);
  expect(payload).toEqual({
    ...snapshot,
    config: {
      ...snapshot.config,
      defaults: { codex: { model: "new", effort: "high" } },
    },
    path: undefined,
    exists: undefined,
    exhaustedRoutes: undefined,
  });
});
for (const status of [409, 422])
  it(`preserves Settings draft and handles ${status}`, async () => {
    const src = source([], status);
    render(<Settings source={src} />);
    await screen.findByLabelText("codex Default model");
    fireEvent.change(screen.getByLabelText("codex Default model"), {
      target: { value: "new" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save defaults" }));
    await screen.findByText(
      status === 409
        ? "Config changed elsewhere — reload to see the latest"
        : "Invalid model",
    );
    expect(screen.getByLabelText("codex Default model")).toHaveValue("new");
    if (status === 409) {
      fireEvent.click(screen.getByRole("button", { name: "Reload defaults" }));
      await act(async () => {});
      expect(screen.getByLabelText("codex Default model")).toHaveValue("old");
    }
  });
it("panel Settings and actions show only data even if the source exposes writes", async () => {
  const src = source();
  render(<Settings source={src} readOnly />);
  await screen.findByText("Edit settings from the dashboard or the CLI.");
  await screen.findByText("verified 1");
  expect(screen.queryByRole("combobox")).toBeNull();
  expect(
    screen.queryByRole("button", { name: /Save|Edit|Refresh models|Clear/ }),
  ).toBeNull();
  cleanup();
  render(
    <JobActions
      job={makeJob("a", { actions: ["cancel", "resume", "apply", "discard"] })}
      source={src}
      readOnly
      notify={vi.fn()}
      onChanged={vi.fn()}
    />,
  );
  expect(screen.queryByRole("button")).toBeNull();
});
it("shell defaults to Jobs and supports all three direct page links", async () => {
  const src = source();
  vi.stubGlobal("fetch", src.fetch);
  render(<App />);
  await screen.findByRole("heading", { name: "Jobs" });
  for (const label of ["Dashboard", "Settings", "Jobs"]) {
    await act(async () => {
      location.hash = "/" + label.toLowerCase();
      window.dispatchEvent(new HashChangeEvent("hashchange"));
    });
    expect(screen.getByRole("heading", { name: label })).toBeInTheDocument();
  }
});
it("keeps filters when closing a detail and traps focus inside it", async () => {
  const row = makeJob("a");
  const src = source([row]);
  src.fetch.mockImplementation(
    async (path) =>
      new Response(
        JSON.stringify(
          path.includes("/a")
            ? { job: row, brief: "Brief", markdown: "Result" }
            : [row],
        ),
      ),
  );
  history.replaceState(null, "", "/#/jobs/a?harness=codex&search=AJ-705");
  render(<Jobs source={src} />);
  const dialog = await screen.findByRole("dialog");
  await within(dialog).findByText("Result");
  const first = within(dialog).getByRole("link", { name: "Close job details" });
  first.focus();
  fireEvent.keyDown(first, { key: "Tab", shiftKey: true });
  expect(first).not.toHaveFocus();
  expect(dialog).toContainElement(document.activeElement as HTMLElement);
  fireEvent.keyDown(document, { key: "Escape" });
  await act(async () => {
    window.dispatchEvent(new HashChangeEvent("hashchange"));
  });
  expect(location.hash).toContain("harness=codex");
  expect(screen.getByPlaceholderText("Search jobs")).toHaveValue("AJ-705");
});
it("uses activity usage for retrying and advances elapsed time every second", async () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-10-16T12:00:00Z"));
  const row = makeJob("a", {
    status: "retrying",
    createdAt: "2026-10-16T10:59:59Z",
    activity: { summary: "Working", inputTokens: 12000, outputTokens: 2000000 },
  });
  render(<JobList jobs={[row]} />);
  expect(screen.getByText("12K in / 2M out")).toHaveAttribute(
    "title",
    "12,000 input / 2,000,000 output tokens",
  );
  expect(screen.getByText("1h 0m 1s")).toBeInTheDocument();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(screen.getByText("1h 0m 2s")).toBeInTheDocument();
});
it("supports failed model discovery, refresh, manual entry and Harness default clearing", async () => {
  const src = source();
  let broken = true;
  src.fetch.mockImplementation(
    async (path) =>
      new Response(
        JSON.stringify(
          path.includes("harnesses")
            ? {
                harnesses: [
                  {
                    ...harness,
                    models: {
                      items: [],
                      error: broken ? "Discovery failed" : "",
                    },
                  },
                ],
              }
            : snapshot,
        ),
      ),
  );
  render(<Settings source={src} />);
  await screen.findByText("Discovery failed");
  fireEvent.change(screen.getByLabelText("codex Default model"), {
    target: { value: "manual/model" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save defaults" }));
  await act(async () => {});
  expect(
    JSON.parse(src.write.mock.calls[0][1].body as string).config.defaults.codex
      .model,
  ).toBe("manual/model");
  broken = false;
  fireEvent.click(screen.getByRole("button", { name: "Refresh models" }));
  await act(async () => {});
  expect(src.fetch).toHaveBeenCalledWith(
    "/harnesses?refresh=1",
    expect.any(AbortSignal),
  );
  fireEvent.change(screen.getByLabelText("codex Default model"), {
    target: { value: "" },
  });
  fireEvent.change(screen.getByLabelText("codex Effort"), {
    target: { value: "" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save defaults" }));
  await act(async () => {});
  expect(
    JSON.parse(src.write.mock.calls[1][1].body as string).config.defaults,
  ).toEqual({});
});
it("places Review on attention cards and opens Diff directly without rendering unoffered actions", async () => {
  const row = makeJob("a", {
    actions: ["apply"],
    attention: [{ kind: "worktree_pending", message: "Review changes" }],
  });
  const src = source([row]);
  render(<JobList jobs={[row]} source={src} />);
  const card = screen.getByRole("article");
  expect(
    within(card).getByRole("button", { name: "Apply" }),
  ).toBeInTheDocument();
  for (const name of ["Cancel", "Resume", "Discard"])
    expect(within(card).queryByRole("button", { name })).toBeNull();
  fireEvent.click(within(card).getByRole("button", { name: "Review" }));
  expect(location.hash).toBe("#/jobs/a?tab=diff");
});
it("opens a confirmation outside the scrolled detail and Escape closes only that confirmation", async () => {
  const row = makeJob("a", { actions: ["discard"] });
  const src = source([row]);
  src.fetch.mockImplementation(
    async (path) =>
      new Response(
        JSON.stringify(
          path.endsWith("/a")
            ? { job: row, brief: "Brief", markdown: "Result" }
            : [row],
        ),
      ),
  );
  history.replaceState(null, "", "/#/jobs/a");
  render(<Jobs source={src} />);
  const detail = await screen.findByRole("dialog", { name: "Job details" });
  await within(detail).findByText("Result");
  fireEvent.click(within(detail).getByRole("button", { name: "Discard" }));
  const confirmation = screen.getByRole("dialog", {
    name: "Discard AJ-705 a?",
  });
  expect(detail).not.toContainElement(confirmation);
  fireEvent.keyDown(
    within(confirmation).getByRole("button", { name: "Back" }),
    { key: "Escape" },
  );
  expect(
    screen.queryByRole("dialog", { name: "Discard AJ-705 a?" }),
  ).toBeNull();
  expect(
    screen.getByRole("dialog", { name: "Job details" }),
  ).toBeInTheDocument();
  expect(location.hash).toBe("#/jobs/a");
});
it("does not treat a malformed jobs response as an empty successful list", async () => {
  const src = source();
  src.fetch.mockImplementation(async () => new Response("{}"));
  render(<Jobs source={src} />);
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Invalid response",
  );
  expect(screen.queryByText("No delegation jobs yet")).toBeNull();
});
it("makes covered cards in a collapsed stack inert", () => {
  const { container } = render(
    <JobList jobs={["a", "b", "c"].map((id) => makeJob(id))} />,
  );
  const cards = container.querySelectorAll(".job-stack article");
  expect(cards[0]).not.toHaveAttribute("inert");
  expect(cards[1]).toHaveAttribute("inert");
  expect(cards[2]).toHaveAttribute("inert");
});
it("uses completion dates for seven calendar day KPIs and zero fills chart days", () => {
  const now = new Date(2026, 9, 16, 12);
  const start = new Date(2026, 9, 10);
  const rows = [
    makeJob("a", {
      createdAt: "2026-01-01T00:00:00Z",
      finishedAt: start.toISOString(),
    }),
    makeJob("b", {
      status: "failed",
      finishedAt: new Date(+start - 1).toISOString(),
    }),
    makeJob("c", {
      status: "failed",
      finishedAt: now.toISOString(),
      attention: [{ kind: "failed", message: "Review" }],
    }),
  ];
  expect(dashboardKpis(rows, now)).toEqual({
    progress: 0,
    queued: 0,
    attention: 1,
    succeeded: 1,
    failed: 1,
  });
  const chart = dailyTokens(rows, now);
  expect(chart[0].harnesses).toEqual({});
  expect(chart[13].harnesses.codex).toBe(1200);
});
it("uses the specified empty message when filters leave all four sections empty", () => {
  history.replaceState(null, "", "/#/jobs?search=missing");
  render(<JobList jobs={[makeJob("a")]} />);
  expect(screen.getByText("No delegation jobs yet")).toBeInTheDocument();
});
it("announces action failures outside the inert background while preserving resume instructions", async () => {
  const row = makeJob("a", {
    status: "failed",
    attention: [{ kind: "failed", message: "Failed" }],
    actions: ["resume"],
  });
  const src = source([row]);
  src.write.mockImplementation(
    async () =>
      new Response(JSON.stringify({ error: "Retry later" }), { status: 409 }),
  );
  const { container } = render(<Jobs source={src} />);
  fireEvent.click(await screen.findByRole("button", { name: "Resume" }));
  fireEvent.change(screen.getByLabelText("Instructions for the next run"), {
    target: { value: "Keep these instructions" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Start" }));
  const toast = await screen.findByText("Retry later");
  expect(toast).toHaveAttribute("role", "status");
  expect(container).not.toContainElement(toast);
  expect(screen.getByLabelText("Instructions for the next run")).toHaveValue(
    "Keep these instructions",
  );
});
