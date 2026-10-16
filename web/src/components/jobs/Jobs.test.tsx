import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Jobs } from "./Jobs";
import App from "../../App";

const job = {
 id: "live", title: "AJ-689 Jobs view", issue: "AJ-689", harness: "codex",
 modelRequested: "default", repo: "/repo/" + "long-path".repeat(20), cwd: "/repo",
 access: "write", configMode: "minimal", status: "running", duration: "1m",
 createdAt: "2026-10-16T00:00:00Z", inputTokens: 12, outputTokens: 3,
 attention: [], resumeOf: "", resumedBy: [], worktree: {path:"/tmp/wt",baseCommit:"abc",status:"pending"},
 activity: {summary:"tool exec go test",inputTokens:99,outputTokens:8,usagePartial:true},
};
beforeEach(() => { localStorage.clear(); history.replaceState(null, "", "/"); });
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.useRealTimers(); history.replaceState(null,"","/"); });
function mockAPI() {
 const cancel = vi.fn();
 let stream!: ReadableStreamDefaultController<Uint8Array>;
 const body = new ReadableStream<Uint8Array>({start(c) {stream=c;}, cancel});
 const fetcher=vi.fn(async (path:string) => {
  if(path.endsWith("/logs")) return new Response(body);
  if(path.endsWith("/diff")) return new Response(JSON.stringify({patch:"diff --git a/a.go b/a.go\n+new line",files:[],note:"",truncated:true}));
  if(path.endsWith("/live")) return new Response(JSON.stringify({job,brief:"Implement Jobs",markdown:"Final reply: Complete"}));
  return new Response(JSON.stringify([job, {...job,id:"done",title:"Completed task",status:"succeeded",activity:undefined}]));
 });
 vi.stubGlobal("fetch",fetcher);
 return {fetcher,stream,cancel};
}
it("lists job summaries, running first, using the dashboard API", async () => {
 const {fetcher}=mockAPI(); render(<Jobs />);
 const link=await screen.findByRole("link",{name:job.title});
 expect(screen.getAllByRole("article")[0]).toContainElement(link);
 expect(screen.getByText(/99.*8.*partial/)).toBeInTheDocument();
 expect(screen.getAllByText(/worktree · pending/)).toHaveLength(2);
 expect(screen.getByText("tool exec go test")).toBeInTheDocument();
 expect(fetcher).toHaveBeenCalledWith("/api/sub/jobs?all=1",expect.objectContaining({credentials:"same-origin"}));
 fireEvent.change(screen.getByLabelText("Status"),{target:{value:"failed"}});
 expect(screen.getByText("No jobs match these filters.")).toBeInTheDocument();
});
it("opens details, result, bounded logs with automatic follow, diff and closes the stream",async()=>{
 const {stream,cancel,fetcher}=mockAPI();
 history.replaceState(null,"","/#/jobs/live");
 const {unmount}=render(<Jobs />);
 await screen.findByText("Implement Jobs");
 expect(screen.getByText("Final reply: Complete")).toBeInTheDocument();
 expect(await screen.findByText("Truncated")).toBeInTheDocument();
 fireEvent.click(screen.getByText("a/a.go b/a.go"));
 expect(screen.getByText(/\+new line/)).toBeInTheDocument();
 const log=screen.getByRole("log");
 Object.defineProperty(log,"scrollHeight",{value:420});
 await act(async()=> {stream.enqueue(new TextEncoder().encode(Array.from({length:1005},(_,i)=>`data: "message ${i}"\n\n`).join("")));});
 expect(log.textContent?.split("\n")).toHaveLength(1000);
 expect(log).toHaveTextContent("message 1004");
 expect(log.scrollTop).toBe(420);
 expect(fetcher.mock.calls.some(([path])=>path==="/api/sub/jobs/live/logs")).toBe(true);
 unmount();
 await waitFor(()=>expect(cancel).toHaveBeenCalled());
});
it("shows an honest empty state only after loading",async()=>{
 let resolve!: (value:Response)=>void;
 vi.stubGlobal("fetch",vi.fn(()=>new Promise<Response>(r=>{resolve=r;})));
 render(<Jobs />);
 expect(screen.queryByText("No jobs match these filters.")).not.toBeInTheDocument();
 expect(screen.getByText("Loading jobs…")).toBeInTheDocument();
 await act(async()=>resolve(new Response("[]")));
 expect(await screen.findByText("No jobs match these filters.")).toBeInTheDocument();
});
it("reports list errors and recovers on the next poll",async()=>{
 vi.useFakeTimers();
 const fetcher=vi.fn().mockResolvedValueOnce(new Response("",{status:503})).mockResolvedValue(new Response("[]"));
 vi.stubGlobal("fetch",fetcher); render(<Jobs />);
 await act(async()=>{});
 expect(screen.getByRole("alert")).toHaveTextContent("503");
 expect(screen.queryByText("No jobs match these filters.")).not.toBeInTheDocument();
 await act(async()=>{await vi.advanceTimersByTimeAsync(2000);});
 expect(screen.queryByRole("alert")).not.toBeInTheDocument();
 expect(screen.getByText("No jobs match these filters.")).toBeInTheDocument();
});
it("handles separate detail, diff and stream errors",async()=>{
 history.replaceState(null,"","/#/jobs/missing");
 vi.stubGlobal("fetch",vi.fn(async(path:string)=>new Response(path.includes("?all")?"[]":"",{status:path.includes("?all")?200:404})));
 render(<Jobs />);
 expect(await screen.findByRole("alert")).toHaveTextContent("404");
 expect(screen.getByRole("link",{name:"← All jobs"})).toBeInTheDocument();
});
it("supports the saved Jobs lens without registered pact projects",async()=>{
 localStorage.setItem("pactify:lens","jobs");
 vi.stubGlobal("fetch",vi.fn(async(path:string)=>new Response(JSON.stringify(path.startsWith("/api/sub/")?[job]:[]))));
 render(<App />);
 expect(await screen.findByRole("link",{name:job.title})).toBeInTheDocument();
 expect(screen.getByTestId("lens-jobs")).toHaveAttribute("aria-pressed","true");
 fireEvent.click(screen.getByTestId("lens-dashboard"));
 fireEvent.click(screen.getByTestId("lens-jobs"));
 expect(localStorage.getItem("pactify:lens")).toBe("jobs");
 expect(within(screen.getByRole("group",{name:"lens"})).getByRole("button",{name:"Jobs"})).toBeInTheDocument();
});

it("keeps the detail usable when result, diff and logs are unavailable", async () => {
  history.replaceState(null, "", "/#/jobs/live");
  vi.stubGlobal("fetch", vi.fn(async (path: string) => {
    if (path.endsWith("/logs")) return new Response("", { status: 502 });
    if (path.endsWith("/diff")) return new Response("", { status: 503 });
    if (path.endsWith("/live")) return new Response(JSON.stringify({ job, brief: "Brief still available", markdown: "", resultError: "Result unavailable: invalid JSON" }));
    return new Response(JSON.stringify([job]));
  }));
  render(<Jobs />);
  await screen.findByText("Brief still available");
  expect(await screen.findByText(/502/)).toBeInTheDocument();
  expect(screen.getAllByRole("alert").map((node) => node.textContent)).toEqual(expect.arrayContaining([
    "Result unavailable: invalid JSON", expect.stringContaining("503"),
  ]));
  expect(screen.getByRole("heading", { name: "Resume chain" })).toBeInTheDocument();
});

it("offers mobile view selection and the Jobs keyboard shortcut", async () => {
  vi.stubGlobal("fetch", vi.fn(async () => new Response("[]")));
  render(<App />);
  fireEvent.change(screen.getByLabelText("View", { exact: true }), { target: { value: "jobs" } });
  await screen.findByTestId("view-jobs");
  expect(localStorage.getItem("pactify:lens")).toBe("jobs");
  fireEvent.click(screen.getByTestId("lens-dashboard"));
  fireEvent.keyDown(document.body, { key: "4" });
  await screen.findByTestId("view-jobs");
});

it("keeps standalone theme selection", async () => {
 vi.stubGlobal("fetch",vi.fn(async()=>new Response("[]")));
 render(<Jobs standalone />);
 await screen.findByRole("heading",{name:"Delegated jobs"});
 fireEvent.change(screen.getByLabelText("Theme"),{target:{value:"light"}});
 expect(document.documentElement.dataset.theme).toBe("light");
});

it("shows a failure class badge on failed cards and tolerates legacy jobs", async () => {
 vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify([
  {...job, id:"quota", title:"Quota exhausted task", status:"failed", failureClass:"quota", activity:undefined},
  {...job, id:"legacy", title:"Legacy failed task", status:"failed", activity:undefined},
 ]))));
 render(<Jobs />);
 const link=await screen.findByRole("link",{name:"Quota exhausted task"});
 expect(within(link.closest("article")!).getByText("quota", {exact:true})).toBeInTheDocument();
 expect(screen.getByRole("link",{name:"Legacy failed task"})).toBeInTheDocument();
});


it("shows retry progress and completed retry counts", async () => {
 vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify([
  {...job, retryAttempt: 1, retryLimit: 2},
  {...job, id: "retried", title: "Recovered task", status: "succeeded", activity: undefined, retryCount: 2},
 ]))));
 render(<Jobs />);
 expect(await screen.findByText("retrying (1/2)")).toBeInTheDocument();
 expect(screen.getByText("retries: 2")).toBeInTheDocument();
});
