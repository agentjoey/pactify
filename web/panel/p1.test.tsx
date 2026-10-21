import { beforeEach } from "vitest";
import { act, cleanup, fireEvent, render, renderHook, screen, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { AppContent } from "../src/App";
import { JobList, filterJobs } from "../src/components/jobs/Jobs";
import { JobDetail } from "../src/components/jobs/JobDetail";
import { Settings } from "../src/components/jobs/Settings";
import { useResource } from "../src/components/jobs/data";
import type { Job } from "../src/components/jobs/types";
import type { JobsSource } from "../src/components/jobs/source";
const job: Job = {id:"unique-id",title:"Task",harness:"codex",modelRequested:"requested-model",modelActual:"actual-model",repo:"/unique-repo",cwd:"/repo",status:"succeeded",access:"write",configMode:"minimal",duration:"2m",createdAt:"2026-10-20T12:00:00Z",inputTokens:1234,outputTokens:42,attention:[],resumeOf:"",resumedBy:[]};
const source = (fetcher: JobsSource["fetch"] = vi.fn(async () => Response.json([]))): JobsSource => ({jobsPath:"/api/jobs",costPath:"/api/cost",fetch:fetcher});
afterEach(() => {cleanup(); vi.useRealTimers(); vi.restoreAllMocks(); history.replaceState(null,"","/");});
for (const search of ["UNIQUE-ID","unique-repo","requested-model","actual-model"]) it(`searches ${search} in the utility and board`, () => {
  expect(filterJobs([job],{search,status:"",harness:"",repo:""})).toEqual([job]);
  history.replaceState(null,"",`/#/jobs?search=${search}`);
  render(<JobList jobs={[job]} />);
  expect(screen.getByRole("link",{name:"Task"})).toBeInTheDocument();
});
it("filters attention with a quick chip and toggles compact completed history", () => {
  render(<JobList jobs={[job,{...job,id:"attention",title:"Review",attention:[{kind:"warnings",message:"Check"}]}]} readOnly />);
  fireEvent.click(screen.getByRole("button",{name:"Needs attention"}));
  expect(screen.queryByRole("region",{name:"Completed"})).toBeNull();
  fireEvent.click(screen.getByRole("button",{name:"Needs attention"}));
  fireEvent.click(screen.getByRole("button",{name:"Compact list"}));
  const row=screen.getByRole("list",{name:"Completed jobs"});
  for (const value of ["succeeded","Task","codex · actual-model","2m","subscription"]) expect(within(row).getByText(value)).toBeInTheDocument();
  expect(row).toHaveTextContent("1.2K in / 42 out");
});
it("keeps summary cost in Usage and labels subscription and unknown harness costs", async () => {
  const row={...job,costUSD:0.12};
  render(<JobDetail id={row.id} jobs={[row]} source={source(vi.fn(async () => Response.json({job:row})))} readOnly onChanged={()=>{}} notify={()=>{}} initialTab="summary" />);
  const usage=(await screen.findByRole("heading",{name:"Usage"})).parentElement!;
  expect(within(usage).getByText("$0.12")).toBeInTheDocument();
  cleanup(); render(<JobList jobs={[job,{...job,id:"other",harness:"opencode"}]} />);
  expect(screen.getByText("subscription")).toBeInTheDocument();
  expect(screen.getByText("unknown")).toBeInTheDocument();
});
it("colours literal diff lines, counts file changes, collapses large files and offers the full command", async () => {
  const row={...job,worktree:{path:"/wt's dir",baseCommit:"abc",status:"pending"}};
  const patch="diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1 +1 @@\n-old\n+<script>unsafe</script>\n";
  const fetcher=vi.fn(async (path:string) => Response.json(path.endsWith("/diff") ? {patch:patch+"diff --git a/b b/b\n"+"+large\n".repeat(401),files:[],truncated:true,maxBytes:524288} : {job:row}));
  const {container}=render(<JobDetail id={row.id} jobs={[row]} source={source(fetcher)} readOnly onChanged={()=>{}} notify={()=>{}} initialTab="diff" />);
  expect(await screen.findByText("Truncated at 512 KiB")).toBeInTheDocument();
  const summaries=container.querySelectorAll("details summary");
  expect(summaries[0]).toHaveTextContent("+1 / −1");
  expect(summaries[1].parentElement).not.toHaveAttribute("open");
  expect(screen.getByText("+<script>unsafe</script>")).toHaveClass("diff-added");
  expect(screen.getByText("-old")).toHaveClass("diff-removed");
  expect(screen.getByText("@@ -1 +1 @@")).toHaveClass("diff-hunk");
  expect(screen.getByText("+++ b/a")).toHaveClass("diff-header");
  expect(container.querySelector("script")).toBeNull();
  expect(screen.getByRole("button",{name:"Copy git -C '/wt'\\''s dir' diff 'abc' --"})).toBeInTheDocument();
});
it("pauses hidden polling, backs off to 30s, resets on success and stops at 401", async () => {
  vi.useFakeTimers(); let hidden=true;
  vi.spyOn(document,"hidden","get").mockImplementation(()=>hidden);
  const fetcher=vi.fn().mockRejectedValue(new Error("offline"));
  const src=source(fetcher);
  renderHook(()=>useResource(src,"/jobs",2000));
  await act(async()=>{await vi.advanceTimersByTimeAsync(10000);});
  expect(fetcher).not.toHaveBeenCalled();
  hidden=false; await act(async()=>{document.dispatchEvent(new Event("visibilitychange"));});
  expect(fetcher).toHaveBeenCalledTimes(1);
  for (const [delay,count] of [[4000,2],[8000,3],[16000,4],[30000,5],[30000,6]]) {
    await act(async()=>{await vi.advanceTimersByTimeAsync(delay-1);}); expect(fetcher).toHaveBeenCalledTimes(count-1);
    await act(async()=>{await vi.advanceTimersByTimeAsync(1);}); expect(fetcher).toHaveBeenCalledTimes(count);
  }
  fetcher.mockImplementation(async () => Response.json([]));
  await act(async()=>{await vi.advanceTimersByTimeAsync(30000);});
  await act(async()=>{await vi.advanceTimersByTimeAsync(2000);}); expect(fetcher).toHaveBeenCalledTimes(8);
  hidden=true; act(()=>document.dispatchEvent(new Event("visibilitychange")));
  await act(async()=>{await vi.advanceTimersByTimeAsync(30000);}); expect(fetcher).toHaveBeenCalledTimes(8);
  hidden=false; fetcher.mockRejectedValue(Object.assign(new Error("Session ended"),{status:401}));
  await act(async()=>{document.dispatchEvent(new Event("visibilitychange"));});
  await act(async()=>{await vi.advanceTimersByTimeAsync(60000);}); expect(fetcher).toHaveBeenCalledTimes(9);
});
it("shows relative updates, chain attention count, docs, version and read-only guidance", async () => {
  vi.useFakeTimers();
  const fetcher=vi.fn(async (path:string)=>Response.json(path.endsWith("/info") ? {version:"1.2.3"} : [{...job,attention:[{kind:"warnings",message:"Check"}]}]));
  render(<AppContent source={source(fetcher)} readOnly />); await act(async()=>{});
  expect(document.title).toBe("(1) pactify panel");
  expect(screen.getByRole("status")).toHaveTextContent("live · 0s ago");
  expect(screen.getByText("1.2.3")).toBeInTheDocument();
  expect(screen.getByTitle(/read-only.*CLI.*pactify serve/)).toBeInTheDocument();
  expect(screen.getByRole("link",{name:"Docs"})).toHaveAttribute("rel","noopener noreferrer");
  expect(screen.getByRole("link",{name:"Docs"})).toHaveAttribute("target","_blank");
  await act(async()=>{await vi.advanceTimersByTimeAsync(1000);}); expect(screen.getByRole("status")).toHaveTextContent("live · 1s ago");
});
it("shows read-only doctor status and a copyable CLI hint in Settings", async () => {
  const fetcher=vi.fn(async (path:string)=>Response.json(path.endsWith("/doctor") ? {lastRun:null,harnesses:[{name:"codex",installed:true,version:"9.1"}]} : path.endsWith("/harnesses") ? {harnesses:[]} : {version:"v",config:{}}));
  render(<Settings source={{...source(fetcher),routerPath:"/api/router"}} readOnly />);
  fireEvent.click(screen.getByRole("button",{name:"Doctor"}));
  expect(await screen.findByText("Last doctor run: unknown")).toBeInTheDocument();
  expect(screen.getByText("codex · 9.1")).toBeInTheDocument();
  expect(screen.getByRole("button",{name:"Copy pactify sub doctor"})).toBeInTheDocument();
});

it("marks dashboard 401 errors so shared polling can stop", async () => {
  const { dashboardJobsSource } = await import("../src/components/jobs/source");
  vi.stubGlobal("fetch", vi.fn(async () => new Response("", {status:401})));
  try {
    await expect(dashboardJobsSource.fetch("/api/sub/jobs", new AbortController().signal)).rejects.toMatchObject({status:401});
  } finally {
    vi.unstubAllGlobals();
  }
});

// Legacy list behavior remains covered explicitly; pipeline defaults have their own suite.
beforeEach(() => { localStorage.setItem("pactify-jobs-view", "list"); });
afterEach(() => { localStorage.clear(); });
