import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { LiveTree } from "./LivePanel";
import type { Job } from "./types";
import type { JobsSource } from "./source";
const job: Job = {
  id: "running",
  title: "Work",
  harness: "codex",
  modelRequested: "gpt",
  effort: "high",
  repo: "/repo",
  cwd: "/repo",
  access: "write",
  configMode: "minimal",
  status: "running",
  duration: "3m",
  createdAt: "2026-10-16T00:00:00Z",
  inputTokens: 0,
  outputTokens: 0,
  attention: [],
  resumeOf: "parent",
  resumedBy: [],
  retryCount: 2,
};
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});
it("renders streamed tool/usage/diff data, pauses hidden streams, resets replayed tails and aborts on unmount", async () => {
  let canceled = 0;
  const fetch = vi.fn(async (path: string) => {
    if (path === "/router")
      return new Response(
        JSON.stringify({
          config: {
            aliases: { fast: { harness: "codex", model: "gpt" } },
            fallback: { codex: ["kimi"] },
          },
          exhaustedRoutes: [{ key: "kimi/k2" }],
        }),
      );
    return new Response(
      new ReadableStream({
        start(controller) {
          controller.enqueue(
            new TextEncoder().encode(
              [
                { kind: "state", state: "turn_started" },
                {
                  kind: "tool",
                  tool: {
                    id: "/one",
                    name: "shell",
                    text: "go test ./...",
                    phase: "start",
                  },
                },
                {
                  kind: "usage",
                  usage: { inputTokens: 100, outputTokens: 20 },
                },
                { kind: "diff", diff: { files: 2, added: 10, deleted: 1 } },
                { kind: "message", text: "Checking tests", final: true },
              ]
                .map((e) => `data: ${JSON.stringify(e)}\n\n`)
                .join(""),
            ),
          );
        },
        cancel() {
          canceled++;
        },
      }),
    );
  });
  const source: JobsSource = {
    jobsPath: "/jobs",
    costPath: "/cost",
    routerPath: "/router",
    fetch,
  };
  const visibility = vi
    .spyOn(document, "visibilityState", "get")
    .mockReturnValue("visible");
  const view = render(<LiveTree source={source} jobs={[job]} />);
  await screen.findByText("1 observed call");
  expect(screen.getByRole("heading", { name: "Session log" })).toBeTruthy();
  expect(screen.getByText("120 tokens")).toBeTruthy();
  expect(screen.getByText("2 files · +10 / −1")).toBeTruthy();
  expect(screen.getByRole("log").textContent).toContain("Checking tests");
  expect(screen.getByText("retry ×2")).toBeTruthy();
  expect(screen.getByText("rework → running")).toBeTruthy();
  visibility.mockReturnValue("hidden");
  document.dispatchEvent(new Event("visibilitychange"));
  await waitFor(() => expect(canceled).toBe(1));
  visibility.mockReturnValue("visible");
  document.dispatchEvent(new Event("visibilitychange"));
  await screen.findByText("1 observed call");
  expect(screen.queryByText("2 observed calls")).toBeNull();
  view.unmount();
  await waitFor(() => expect(canceled).toBe(2));
});
it("uses terminal summary totals and never turns a completed job back into running", async () => {
  const source: JobsSource = {
    jobsPath: "/jobs",
    costPath: "/cost",
    fetch: async () =>
      new Response(
        'data: {"kind":"state","state":"turn_started"}\n\nevent: done\ndata: {}\n\n',
      ),
  };
  render(
    <LiveTree
      source={source}
      jobs={[
        {
          ...job,
          status: "succeeded",
          inputTokens: 20,
          outputTokens: 5,
          changedFiles: 1,
          addedLines: 2,
          deletedLines: 0,
        },
      ]}
    />,
  );
  await screen.findByText("Finished");
  expect(
    document.querySelector(".live-harness")?.getAttribute("data-phase"),
  ).toBe("done");
  expect(screen.getByText("25 tokens")).toBeTruthy();
  expect(screen.getByText("1 files · +2 / −0")).toBeTruthy();
});
