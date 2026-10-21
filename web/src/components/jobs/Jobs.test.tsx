import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Jobs } from "./Jobs";
import App from "../../App";

const job = {
 id: "live", title: "Example-689 Jobs view", harness: "codex",
 modelRequested: "default", repo: "/repo/" + "long-path".repeat(20), cwd: "/repo",
 access: "write", configMode: "minimal", status: "running", duration: "1m",
 createdAt: "2026-10-16T00:00:00Z", inputTokens: 12, outputTokens: 3,
 attention: [], resumeOf: "", resumedBy: [], worktree: {path:"/tmp/wt",baseCommit:"abc",status:"pending"},
 activity: {summary:"tool exec go test",inputTokens:99,outputTokens:8,usagePartial:true},
};
beforeEach(() => { localStorage.clear(); localStorage.setItem("pactify-jobs-view", "list"); history.replaceState(null, "", "/"); });
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.useRealTimers(); history.replaceState(null,"","/"); });
function mockAPI() {
 const cancel = vi.fn();
 let stream!: ReadableStreamDefaultController<Uint8Array>;
 const body = new ReadableStream<Uint8Array>({start(c) {stream=c;}, cancel});
 const fetcher=vi.fn(async (path:string) => {
  if(path.endsWith("/logs")) return new Response(body);
  if(path.endsWith("/diff")) return new Response(JSON.stringify({patch:"diff --git a/a.go b/a.go\n+new line",files:[],note:"",truncated:true}));
  if(path.endsWith("/live")) return new Response(JSON.stringify({job,brief:"Implement Jobs",markdown:"Final reply: Complete"}));
  return new Response(JSON.stringify([job, {...job,id:"done",title:"Completed task",harness:"kimi",status:"succeeded",activity:undefined}]));
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
 fireEvent.change(screen.getByLabelText("Harness"),{target:{value:"kimi"}});
 expect(screen.queryByRole("region",{name:"In progress"})).not.toBeInTheDocument();
});
it("opens details, result, bounded logs with automatic follow, diff and closes the stream",async()=>{
 const {stream,cancel,fetcher}=mockAPI();
 history.replaceState(null,"","/#/jobs/live");
 const {unmount}=render(<Jobs />);
 await screen.findByRole("tab", {name:"Summary"});
 fireEvent.click(screen.getByRole("tab", {name:"Summary"}));
 await screen.findByText("Final reply: Complete");
 expect(screen.getByText("Final reply: Complete")).toBeInTheDocument();
 fireEvent.click(screen.getByRole("tab", {name:"Diff"}));
 expect(await screen.findByText("Truncated at 512 KiB")).toBeInTheDocument();
 fireEvent.click(screen.getByText("a.go", {selector:"summary"}));
 expect(screen.getByText(/\+new line/)).toBeInTheDocument();
 fireEvent.click(screen.getByRole("tab", {name:"Logs"}));
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
 expect(screen.queryByText("Start your first job")).not.toBeInTheDocument();
 expect(screen.getByText("Loading jobs…")).toBeInTheDocument();
 await act(async()=>resolve(new Response("[]")));
 expect(await screen.findByText("Start your first job")).toBeInTheDocument();
});
it("reports list errors and recovers on the next poll",async()=>{
 vi.useFakeTimers();
 const fetcher=vi.fn().mockResolvedValueOnce(new Response("",{status:503})).mockResolvedValue(new Response("[]"));
 vi.stubGlobal("fetch",fetcher); render(<Jobs />);
 await act(async()=>{});
 expect(screen.getByRole("alert")).toHaveTextContent("503");
 expect(screen.queryByText("Start your first job")).not.toBeInTheDocument();
 await act(async()=>{await vi.advanceTimersByTimeAsync(4000);});
 expect(screen.queryByRole("alert")).not.toBeInTheDocument();
 expect(screen.getByText("Start your first job")).toBeInTheDocument();
});
it("handles separate detail, diff and stream errors",async()=>{
 history.replaceState(null,"","/#/jobs/missing");
 vi.stubGlobal("fetch",vi.fn(async(path:string)=>new Response(path.includes("?all")?"[]":"",{status:path.includes("?all")?200:404})));
 render(<Jobs />);
 expect(await screen.findByRole("alert")).toHaveTextContent("404");
 expect(screen.getByRole("link",{name:"Close job details"})).toBeInTheDocument();
});
it("opens Jobs without registered pact projects and navigates with hash links",async()=>{
 vi.stubGlobal("fetch",vi.fn(async()=>new Response(JSON.stringify([job]))));
 render(<App />);
 expect(await screen.findByRole("link",{name:job.title})).toBeInTheDocument();
 expect(screen.getByRole("link",{name:"Jobs"})).toHaveAttribute("aria-current","page");
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
  await screen.findByRole("tab", {name:"Summary"});
  fireEvent.click(screen.getByRole("tab", {name:"Summary"}));
  await screen.findByText("Result unavailable: invalid JSON");
 fireEvent.click(screen.getByRole("tab", {name:"Logs"}));
  expect(await screen.findByText(/502/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole("tab", {name:"Diff"}));
  expect(await screen.findByRole("alert")).toHaveTextContent("503");
  fireEvent.click(screen.getByRole("tab", {name:"Brief"}));
  expect(screen.getByText("Brief still available")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("tab", {name:"Summary"}));
  expect(screen.getByRole("heading", { name: "Run timeline" })).toBeInTheDocument();
});

it("offers page navigation through the shared header", async () => {
  vi.stubGlobal("fetch", vi.fn(async () => new Response("[]")));
  render(<App />);
  fireEvent.click(screen.getByRole("link", {name: "Settings"}));
  await act(async () => {window.dispatchEvent(new HashChangeEvent('hashchange'));});
  expect(location.hash).toBe("#/settings");
  expect(await screen.findByRole("heading", {name:"Settings"})).toBeInTheDocument();
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

it("shows fallback navigation in the drawer while cards open the run chain", async () => {
  const routed = { ...job, fallbackFrom: "first", fallbackTo: "last" };
  vi.stubGlobal("fetch", vi.fn(async (path: string) => {
    if (path.endsWith("/logs")) return new Response("");
    if (path.endsWith("/diff")) return new Response(JSON.stringify({ files: [] }));
    if (path.endsWith("/live")) return new Response(JSON.stringify({ job: routed, brief: "Fallback brief", markdown: "" }));
    return new Response(JSON.stringify([routed]));
  }));
  const { unmount } = render(<Jobs />);
  expect(await screen.findByRole("link", { name: job.title })).toHaveAttribute("href", "#/jobs/live");
  expect(screen.queryByRole("navigation", { name: "Fallback chain" })).not.toBeInTheDocument();
  unmount();
  history.replaceState(null, "", "/#/jobs/live");
  render(<Jobs />);
  await screen.findByRole("tab", {name:"Summary"});
  fireEvent.click(screen.getByRole("tab", {name:"Summary"}));
  expect(await screen.findByRole("link", { name: "fallback from first" })).toHaveAttribute("href", "#/jobs/first");
  expect(screen.getByRole("link", { name: "→ fallback to last" })).toHaveAttribute("href", "#/jobs/last");
});
