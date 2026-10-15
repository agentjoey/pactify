import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { JobList, LogView, Panel, filterJobs, splitDiff } from "./Panel";
import type { Job } from "./Panel";

const jobs: Job[] = [
  {
    id: "done",
    title: "Review changes",
    issue: "AJ-659",
    harness: "kimi",
    modelRequested: "kimi-k2",
    repo: "/repo/b",
    cwd: "/repo/b",
    access: "write",
    configMode: "minimal",
    status: "succeeded",
    duration: "1m",
    inputTokens: 1200,
    outputTokens: 300,
    attention: [{ kind: "worktree_pending", message: "Awaiting apply" }],
    resumeOf: "",
    resumedBy: [],
    createdAt: "2026-10-16T00:00:00Z",
  },
  {
    id: "live",
    title: "Build the panel",
    issue: "",
    harness: "codex",
    modelRequested: "gpt-5",
    repo: "/repo/a",
    cwd: "/repo/a",
    access: "write",
    configMode: "minimal",
    status: "running",
    duration: "12s",
    inputTokens: 0,
    outputTokens: 0,
    attention: [],
    resumeOf: "",
    resumedBy: [],
    createdAt: "2026-10-15T00:00:00Z",
    activity: {
      summary: "tool exec go test",
      inputTokens: 99,
      outputTokens: 10,
      usagePartial: true,
    },
  },
];
describe("standalone panel", () => {
  it("filters status, harness and repository and pins running jobs", () => {
    expect(
      filterJobs(jobs, { status: "", harness: "", repo: "" }).map((j) => j.id),
    ).toEqual(["live", "done"]);
    expect(
      filterJobs(jobs, {
        status: "succeeded",
        harness: "kimi",
        repo: "/repo/b",
      }).map((j) => j.id),
    ).toEqual(["done"]);
    expect(
      filterJobs(jobs, { status: "running", harness: "kimi", repo: "" }),
    ).toEqual([]);
    render(<JobList jobs={jobs} />);
    expect(
      screen
        .getAllByRole("option")
        .slice(0, 6)
        .map((option) => option.textContent),
    ).toEqual([
      "All statuses",
      "queued",
      "running",
      "succeeded",
      "failed",
      "canceled",
    ]);
    expect(screen.getByLabelText("Repository")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Harness"), {
      target: { value: "kimi" },
    });
    expect(screen.queryByText("Build the panel")).not.toBeInTheDocument();
    expect(screen.getByText("Review changes")).toBeInTheDocument();
  });
  it("marks attention with readable messages and shows partial running usage", () => {
    render(<JobList jobs={jobs} />);
    expect(screen.getByLabelText("Needs attention")).toHaveTextContent(
      "Awaiting apply",
    );
    expect(screen.getByText(/99.*10.*partial/)).toBeInTheDocument();
  });
  it("renders log text safely and reports stream status", () => {
    render(
      <LogView
        lines={["tool exec go test", "<script>alert(1)</script>"]}
        status="Finished"
      />,
    );
    expect(screen.getByRole("log")).toHaveTextContent("tool exec go test");
    expect(screen.getByRole("log").querySelector("script")).toBeNull();
    expect(screen.getByText("Finished")).toBeInTheDocument();
  });
  it("groups patches by file including quoted paths", () => {
    const files = splitDiff(
      'diff --git a/a.go b/a.go\n+one\ndiff --git "a/sp ace" "b/sp ace"\n+two\n',
    );
    expect(files).toHaveLength(2);
    expect(files[1].name).toContain("sp ace");
  });
  it("opens SSE, appends lines, and closes on done and unmount", async () => {
    const sources: {
      onmessage: ((e: { data: string }) => void) | null;
      onerror: (() => void) | null;
      addEventListener: ReturnType<typeof vi.fn>;
      close: ReturnType<typeof vi.fn>;
    }[] = [];
    class MockEventSource {
      onmessage = null;
      onerror = null;
      addEventListener = vi.fn();
      close = vi.fn();
      constructor() {
        sources.push(this);
      }
    }
    vi.stubGlobal("EventSource", MockEventSource);
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string) => ({
        ok: true,
        json: async () =>
          input.endsWith("/diff")
            ? { patch: "", files: [], note: "No diff" }
            : input.includes("/api/jobs/live")
              ? {
                  job: jobs[1],
                  brief: "Build panel",
                  markdown: "",
                  result: null,
                }
              : jobs,
      })),
    );
    window.location.hash = "/jobs/live";
    const { unmount } = render(<Panel />);
    await screen.findByText("Build panel");
    expect(screen.getByLabelText("Theme")).toBeInTheDocument();
    for (const title of [
      "Task brief",
      "Result summary",
      "Live logs",
      "Diff",
      "Resume chain",
      "CLI commands",
    ]) {
      expect(
        screen.getByRole("heading", { name: title }),
      ).toBeInTheDocument();
    }
    expect(screen.getAllByRole("button", { name: "Copy" })).toHaveLength(4);
    expect(
      screen.getByText("Job data stays on this device"),
    ).toBeInTheDocument();
    const { act } = await import("@testing-library/react");
    act(() =>
      sources[0].onmessage?.({ data: JSON.stringify("message live log") }),
    );
    expect(screen.getByRole("log")).toHaveTextContent("message live log");
    const done = sources[0].addEventListener.mock.calls.find(
      (call) => call[0] === "done",
    )?.[1];
    act(() => done());
    expect(sources[0].close).toHaveBeenCalled();
    unmount();
    expect(sources[0].close).toHaveBeenCalled();
    window.location.hash = "";
    vi.unstubAllGlobals();
  });
});
