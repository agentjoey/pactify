import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { Jobs, JobList, type Job } from "./Jobs";
import { jobColumn, mergeJobChains } from "./board";

const job = (id: string, extra: Partial<Job> = {}): Job => ({
  id, title: `Title ${id}`, harness: "codex", modelRequested: "model", repo: "/repo",
  cwd: "/repo", access: "write", configMode: "minimal", status: "succeeded", duration: "1m",
  createdAt: `2026-10-16T00:00:0${id.length}Z`, inputTokens: 10, outputTokens: 2,
  attention: [], resumeOf: "", resumedBy: [], ...extra,
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); history.replaceState(null, "", "/"); });

it("assigns each job to one column with active status before attention", () => {
  for (const status of ["queued", "running", "retrying"])
    expect(jobColumn(job("a", { status, attention: [{kind: "failed", message: "old"}] }))).toBe("running");
  expect(jobColumn(job("a", {attention: [{kind: "worktree_pending", message: "Apply"}]}))).toBe("attention");
  expect(jobColumn(job("a", {status: "failed", attention: [{kind: "failed", message: "Failed"}]}))).toBe("attention");
  expect(jobColumn(job("a"))).toBe("succeeded");
  for (const status of ["failed", "canceled"]) expect(jobColumn(job("a", { status }))).toBe("failed");
});

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
  expect(within(screen.getByRole("region", {name: "RUNNING"})).getByRole("link", {name: "First title"})).toHaveAttribute("href", "#/jobs/ccc");
  fireEvent.change(screen.getByLabelText("Harness"), {target: {value: "codex"}});
  expect(screen.getAllByRole("article")).toHaveLength(1);
  expect(screen.getByText("3 runs")).toBeInTheDocument();
});

it("renders the four Board empty states", () => {
  render(<JobList jobs={[]} />);
  for (const text of ["No running jobs", "Nothing needs attention", "No succeeded jobs", "No failed jobs"])
    expect(screen.getByText(text)).toHaveClass("kb-ghost");
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
  await within(drawer).findByText("Brief bb");
  expect(within(drawer).getByText("write", {exact: true})).toBeInTheDocument();
  expect(screen.getAllByRole("article", {hidden: true})).toHaveLength(1);
  expect(within(drawer).getByRole("link", {name: /a · succeeded · codex · 1m/})).toHaveAttribute("href", "#/jobs/a");
  await act(async () => { location.hash = "/jobs/a"; window.dispatchEvent(new HashChangeEvent("hashchange")); });
  await within(drawer).findByText("Brief a");
  expect(within(drawer).getByRole("log")).not.toHaveTextContent("logs bb");
  fireEvent.keyDown(document, {key: "Escape"});
  await act(async () => { window.dispatchEvent(new HashChangeEvent("hashchange")); });
  expect(location.hash).toBe("#/jobs");
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(screen.getByRole("link", {name: "Title a"})).toHaveFocus();
});

it("puts Routing in a separate tab and keeps standalone routing read-only", async () => {
  const fetcher = vi.fn(async (path: string) => new Response(JSON.stringify(path.includes("router") ? {
    config: {aliases: {fast: {harness: "codex"}}, fallback: {}, quotaGroups: {}}, exhaustedRoutes: [], version: "1",
  } : [])));
  const source = {jobsPath: "/jobs", costPath: "/cost", routerPath: "/router", fetch: fetcher, write: vi.fn()};
  render(<Jobs source={source} standalone />);
  expect(screen.queryByText("@fast")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("tab", {name: "Routing"}));
  await screen.findByText("@fast");
  expect(screen.queryByRole("region", {name: "RUNNING"})).not.toBeInTheDocument();
  expect(screen.queryByRole("button", {name: "Edit"})).not.toBeInTheDocument();
  expect(screen.getByRole("tab", {name: "Routing"})).toHaveAttribute("aria-selected", "true");
  fireEvent.click(screen.getByRole("tab", {name: "Jobs"}));
  await screen.findByRole("region", {name: "RUNNING"});
  expect(within(screen.getByRole("tabpanel")).getByRole("region", {name: "RUNNING"})).toBeInTheDocument();
});
