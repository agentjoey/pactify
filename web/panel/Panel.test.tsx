import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { JobList, LogView, Panel, filterJobs, splitDiff } from "./Panel";
import type { Job } from "./Panel";

const jobs: Job[] = [
  {
    id: "done",
    title: "Review changes",
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
      "All repositories",
      "/repo/a",
      "/repo/b",
      "All harnesses",
      "codex",
      "kimi",
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
  it("opens authenticated SSE with fetch, appends lines, and closes on done and unmount", async () => {
    let controller: ReadableStreamDefaultController<Uint8Array>;
    const cancelStream = vi.fn();
    const body = new ReadableStream<Uint8Array>({
      start(value) { controller = value; },
      cancel: cancelStream,
    });
    sessionStorage.setItem("pactify-panel-session", "test-session");
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string) => input.endsWith("/logs") ? new Response(body) : ({
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
      "Run timeline",
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
    await act(async () => {
      controller!.enqueue(new TextEncoder().encode('data: "message live log"\n\n'));
    });
    expect(screen.getByRole("log")).toHaveTextContent("message live log");
    await act(async () => {
      controller!.enqueue(new TextEncoder().encode('event: done\ndata: {}\n\n'));
    });
    expect(screen.getByText("Finished")).toBeInTheDocument();
    expect(cancelStream).toHaveBeenCalled();
    expect(vi.mocked(fetch).mock.calls.find(([path]) => String(path).endsWith("/logs"))?.[1]).toMatchObject({
      credentials: "omit", headers: { Authorization: "Bearer test-session" },
    });
    unmount();
    sessionStorage.clear();
    window.location.hash = "";
    vi.unstubAllGlobals();
  });
});
