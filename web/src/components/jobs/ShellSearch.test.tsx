import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { AppContent } from "../../App";
import type { Job } from "./types";

// Exercise the shell listener independently of the list's later passive effect.
vi.mock("./JobsKeyboard", () => ({ JobsKeyboard: () => null }));
afterEach(() => { cleanup(); localStorage.clear(); history.replaceState(null, "", "/"); });
it("focuses visible search with / even before the list keyboard listener attaches", async () => {
  const job: Job = {
    id: "queued-job", title: "Queued job", status: "queued", harness: "codex",
    repo: "/workspace/pactify", cwd: "/workspace/pactify", modelRequested: "model",
    access: "write", configMode: "minimal", duration: "0s", createdAt: new Date().toISOString(),
    inputTokens: 0, outputTokens: 0, attention: [], resumeOf: "", resumedBy: [],
  };
  render(<AppContent readOnly source={{
    jobsPath: "/jobs", costPath: "/cost",
    fetch: vi.fn(async path => Response.json(path.includes("?all=1") ? [job] : {version: "test"})),
  }} />);
  const search = await screen.findByRole("searchbox");
  screen.getByRole("button", {name: "Filters"}).focus();
  fireEvent.keyDown(window, {key: "/"});
  expect(search).toHaveFocus();
});
