import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import App from "./App";

const jobs = ["first", "second"].map((id) => ({
  id, title: `Job ${id}`, issue: "AJ-689", harness: "codex", status: "succeeded",
  modelRequested: "default", repo: "/repo/" + "long-path".repeat(30), cwd: "/repo",
  access: "write", configMode: "minimal", duration: "1m", createdAt: "2026-10-16T00:00:00Z",
  inputTokens: 12, outputTokens: 3, attention: [], resumeOf: "", resumedBy: [],
}));

beforeEach(() => {
  localStorage.clear();
  localStorage.setItem("pactify:lens", "board");
  history.replaceState(null, "", "/");
  delete document.documentElement.dataset.theme;
  vi.stubGlobal("fetch", vi.fn(async (path: string) => {
    if (path.endsWith("/logs")) return new Response('event: done\ndata: {}\n\n');
    if (path.endsWith("/diff")) return new Response(JSON.stringify({ patch: "", files: [], note: "" }));
    const job = jobs.find((job) => path === `/api/sub/jobs/${job.id}`);
    if (job) return new Response(JSON.stringify({ job, brief: `Brief ${job.id}`, markdown: "Completed" }));
    return new Response(JSON.stringify(path.startsWith("/api/sub/") ? jobs : []));
  }));
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  history.replaceState(null, "", "/");
  delete document.documentElement.dataset.theme;
});

for (const hash of ["#/jobs", "#/jobs/first"]) {
  it(`opens ${hash} on load instead of the saved Board lens`, async () => {
    history.replaceState(null, "", "/" + hash);
    render(<App />);
    await screen.findByRole("heading", { name: hash === "#/jobs" ? "Delegated jobs" : "Job first" });
    expect(screen.getByTestId("lens-jobs")).toHaveAttribute("aria-pressed", "true");
    expect(localStorage.getItem("pactify:lens")).toBe("jobs");
  });
}

it("follows hash changes from Board, between details and back to the Jobs list", async () => {
  render(<App />);
  for (const id of ["first", "second", ""]) {
    await act(async () => {
      history.replaceState(null, "", "/#/jobs" + (id ? `/${id}` : ""));
      window.dispatchEvent(new HashChangeEvent("hashchange"));
    });
    await screen.findByRole("heading", { name: id ? `Job ${id}` : "Delegated jobs" });
    expect(screen.getByTestId("lens-jobs")).toHaveAttribute("aria-pressed", "true");
  }
});

for (const target of ["dashboard", "board", "cockpit", "settings", "mobile", "keyboard"]) {
  it(`clears the Jobs hash when leaving through ${target}`, async () => {
    localStorage.setItem("pactify:lens", "jobs");
    history.replaceState(null, "", "/?keep=1#/jobs/first");
    render(<App />);
    await screen.findByRole("heading", { name: "Job first" });
    if (target === "mobile") fireEvent.change(screen.getByLabelText("View", { exact: true }), { target: { value: "board" } });
    else if (target === "keyboard") fireEvent.keyDown(document.body, { key: "2" });
    else fireEvent.click(screen.getByTestId(target === "settings" ? "toolbar-settings" : `lens-${target}`));
    expect(location.hash).toBe("");
    expect(location.search).toBe("?keep=1");
    expect(screen.queryByTestId("view-jobs")).not.toBeInTheDocument();
  });
}

it("creates a shareable list hash when selecting the Jobs tab", async () => {
  render(<App />);
  fireEvent.click(screen.getByTestId("lens-jobs"));
  await screen.findByRole("heading", { name: "Delegated jobs" });
  expect(location.hash).toBe("#/jobs");
});

it("renders embedded Jobs without a panel header or changing the dashboard theme", async () => {
  localStorage.setItem("pactify:lens", "jobs");
  localStorage.setItem("pactify-jobs-theme", "light");
  render(<App />);
  await screen.findByRole("heading", { name: "Delegated jobs" });
  expect(screen.queryByLabelText("Theme")).not.toBeInTheDocument();
  expect(screen.getByTestId("view-jobs").querySelector("header")).toBeNull();
  expect(document.documentElement.dataset.theme).toBeUndefined();
  expect(screen.getByTestId("toolbar")).not.toHaveAttribute("style");
  fireEvent.click(screen.getByTestId("lens-board"));
  expect(document.documentElement.dataset.theme).toBeUndefined();
  expect(localStorage.getItem("pactify-jobs-theme")).toBe("light");
});

it("keeps panel theme overrides out of the dashboard stylesheet", () => {
  const tokens = readFileSync(resolve(process.cwd(), "src/tokens.css"), "utf8");
  expect(tokens).not.toContain(":root[data-theme]");
  expect(readFileSync(resolve(process.cwd(), "src/index.css"), "utf8")).toContain("color-scheme: dark");
});
