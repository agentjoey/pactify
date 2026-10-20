import * as React from "react";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { JobDetail } from "../src/components/jobs/JobDetail";
import type { Job } from "../src/components/jobs/types";
import type { JobsSource } from "../src/components/jobs/source";

vi.mock("react", {spy: true});
afterEach(() => {cleanup(); vi.restoreAllMocks();});

it("preserves a manual Summary selection when a delayed default-tab effect runs", async () => {
  const job: Job = {
    id: "active", title: "Active task", status: "running", harness: "codex",
    modelRequested: "model", repo: "/repo", cwd: "/repo", access: "write",
    configMode: "minimal", duration: "1m", createdAt: "2026-10-20T12:00:00Z",
    inputTokens: 10, outputTokens: 2, attention: [], resumeOf: "", resumedBy: [],
  };
  const detail = {job};
  let initialEffect: React.EffectCallback | undefined;
  const {useEffect} = await vi.importActual<typeof React>("react");
  // Delay the first effect depending on the detail response, allowing a real
  // click after the DOM commits but before that passive effect runs.
  vi.mocked(React.useEffect).mockImplementation((setup, deps) => {
    useEffect(() => {
      if (!initialEffect && deps?.[0] === detail) {
        initialEffect = setup;
        return;
      }
      return setup();
    }, deps);
  });
  const source: JobsSource = {
    jobsPath: "/api/jobs", costPath: "/api/cost",
    fetch: async path => path.endsWith("/events")
      ? new Response("event: done\ndata: {}\n\n")
      : {json: async () => detail} as Response,
  };
  render(<JobDetail id={job.id} jobs={[job]} source={source} readOnly
    onChanged={() => {}} notify={() => {}} initialTab="" />);
  fireEvent.click(await screen.findByRole("tab", {name: "Summary"}));
  await act(async () => {initialEffect?.();});
  expect(screen.getByRole("tab", {name: "Summary"})).toHaveAttribute("aria-selected", "true");
  expect(screen.getByRole("heading", {name: "Usage"})).toBeInTheDocument();
});
