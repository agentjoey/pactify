import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { Jobs, JobList, type Job } from "./Jobs";
import { mergeJobChains } from "./board";

const job = (id: string, extra: Partial<Job> = {}): Job => ({
  id, title: `Title ${id}`, harness: "codex", modelRequested: "model", repo: "/repo",
  cwd: "/repo", access: "write", configMode: "minimal", status: "succeeded", duration: "1m",
  createdAt: `2026-10-16T00:00:0${id.length}Z`, inputTokens: 10, outputTokens: 2,
  attention: [], resumeOf: "", resumedBy: [], ...extra,
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); history.replaceState(null, "", "/"); });

it("keeps a single run and merges repeated resumes in causal order, independent of input order", () => {
  const a = job("a", {resumedBy: ["b"]});
  const b = job("b", {resumeOf: "a", resumedBy: ["c"]});
  const c = job("c", {resumeOf: "b", status: "running"});
  expect(mergeJobChains([a])[0]).toMatchObject({root: a, latest: a, runs: [a], rework: 0});
  expect(mergeJobChains([c, b, a])[0]).toMatchObject({root: a, latest: c, runs: [a,b,c], rework: 2});
});

it("merges fallback and mixed resume/fallback chains, retaining the first title", () => {
  const a = job("a", {fallbackTo: "b"});
  const b = job("b", {harness: "opencode", fallbackFrom: "a", resumedBy: ["c"]});
  const c = job("c", {harness: "opencode", resumeOf: "b"});
  expect(mergeJobChains([b, a])[0]).toMatchObject({root: a, latest: b, runs: [a,b], rework: 0});
  expect(mergeJobChains([c, a, b])[0]).toMatchObject({root: a, latest: c, runs: [a,b,c], rework: 1});
});

it("tolerates cycles, missing ids, one-sided links and forks without losing or duplicating runs", () => {
  const rows = [job("a", {resumeOf: "b", resumedBy: ["c", "missing"]}), job("b", {resumeOf: "a"}), job("c"), job("z", {fallbackFrom: "missing"})];
  const chains = mergeJobChains(rows);
  expect(chains).toHaveLength(2);
  expect(chains.flatMap(c => c.runs.map(j => j.id)).sort()).toEqual(["a","b","c","z"]);
  expect(mergeJobChains([...rows].reverse())).toEqual(chains);
  expect(rows[0].resumedBy).toEqual(["c", "missing"]);
});

it("renders one TaskCard-style card per chain using latest status and whole-chain filters", () => {
  render(<JobList jobs={[
    job("a", {title: "First title", harness: "codex", fallbackTo: "bb", failureClass: "quota"}),
    job("bb", {harness: "opencode", fallbackFrom: "a", resumedBy: ["ccc"]}),
    job("ccc", {harness: "opencode", resumeOf: "bb", status: "running", access: "read-only", activity: {summary: "Latest activity", inputTokens: 20, outputTokens: 3}, worktree: {path: "/wt", baseCommit: "abc", status: "pending"}}),
  ]} />);
  expect(screen.getAllByRole("article")).toHaveLength(1);
  const card = screen.getByRole("article");
  expect(card).toHaveClass("task-card");
  for (const text of ["3 runs", "rework 1", "fallback → opencode", "quota", "read-only", "Latest activity"])
    expect(within(card).getByText(text)).toBeInTheDocument();
  expect(within(screen.getByRole("region", {name: "In progress"})).getByRole("link", {name: "First title"})).toHaveAttribute("href", "#/jobs/ccc");
  fireEvent.change(screen.getByLabelText("Harness"), {target: {value: "codex"}});
  expect(screen.getAllByRole("article")).toHaveLength(1);
  expect(screen.getByText("3 runs")).toBeInTheDocument();
});

it("opens a deep-linked drawer over the board, switches runs, resets logs, closes and restores focus", async () => {
  const rows = [job("a", {resumedBy: ["bb"]}), job("bb", {resumeOf: "a"})];
  vi.stubGlobal("fetch", vi.fn(async (path: string) => {
    if (path.endsWith("/logs")) return new Response(`data: "logs ${path.includes('/bb/') ? 'bb' : 'a'}"\n\nevent: done\ndata: {}\n\n`);
    if (path.endsWith("/diff")) return new Response(JSON.stringify({files: []}));
    const row = rows.find(j => path.endsWith(`/${j.id}`));
    return new Response(JSON.stringify(row ? {job: row, brief: `Brief ${row.id}`, markdown: "Result"} : rows));
  }));
  history.replaceState(null, "", "/#/jobs/bb");
  render(<Jobs />);
  const drawer = await screen.findByRole("dialog", {name: "Job details"});
  await within(drawer).findByRole("heading", {name: "Title bb"});
  fireEvent.click(within(drawer).getByRole("tab", {name: "Brief"}));
  await within(drawer).findByText("Brief bb");
  fireEvent.click(within(drawer).getByRole("tab", {name: "Summary"}));
  expect(within(drawer).getByText("write · minimal config", {exact: true})).toBeInTheDocument();
  expect(screen.getAllByRole("article", {hidden: true})).toHaveLength(1);
  expect(within(drawer).getByRole("link", {name: /a · succeeded · codex · 1m/})).toHaveAttribute("href", "#/jobs/a");
  await act(async () => { location.hash = "/jobs/a"; window.dispatchEvent(new HashChangeEvent("hashchange")); });
  await within(drawer).findByRole("heading", {name: "Title a"});
  fireEvent.click(within(drawer).getByRole("tab", {name: "Brief"}));
  await within(drawer).findByText("Brief a");
  fireEvent.click(within(drawer).getByRole("tab", {name: "Logs"}));
  await within(drawer).findByText("logs a");
  expect(within(drawer).getByRole("log")).not.toHaveTextContent("logs bb");
  fireEvent.keyDown(document, {key: "Escape"});
  await act(async () => { window.dispatchEvent(new HashChangeEvent("hashchange")); });
  expect(location.hash).toBe("#/jobs");
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(screen.getByRole("link", {name: "Title a"})).toHaveFocus();
});
