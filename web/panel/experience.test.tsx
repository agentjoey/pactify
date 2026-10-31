import { beforeEach } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import App from "../src/App";
import { Settings } from "../src/components/jobs/Settings";
import { Panel } from "./Panel";
import { JobList } from "../src/components/jobs/Jobs";
import type { Job } from "../src/components/jobs/types";
const job: Job = {id:"latest", title:"Check the result", status:"running", harness:"codex", modelRequested:"model", access:"write", configMode:"minimal", repo:"/repo", cwd:"/repo", createdAt:"2026-10-20", duration:"1m", inputTokens:0, outputTokens:0, attention:[], resumeOf:"first", resumedBy:[], actions:["cancel"]};
afterEach(() => {cleanup(); vi.unstubAllGlobals(); vi.useRealTimers(); history.replaceState(null,"","/");});
it("shows onboarding without filters only when there are no jobs", async () => {
  vi.stubGlobal("fetch", vi.fn(async () => Response.json([])));
  render(<Panel />);
  await screen.findByRole("heading",{name:"Start your first job"});
  for (const text of ["/delegate", "/delegate-codex", "pactify sub run", "pactify sub doctor"]) expect(screen.getByText(text)).toBeInTheDocument();
  expect(screen.queryByLabelText("Project")).toBeNull();
  expect(screen.getByRole("link",{name:"Read the getting started guide"})).toHaveAttribute("href","https://github.com/agentjoey/pactify#readme");
});
it("distinguishes filters with no matches from an empty history", () => {
  history.replaceState(null,"","/#/jobs?search=missing");
  render(<JobList jobs={[job]} readOnly />);
  expect(screen.getByText("No jobs match these filters.")).toBeInTheDocument();
  expect(screen.getByLabelText("Project")).toBeInTheDocument();
});
it("shows skeletons during first load", () => {
  vi.stubGlobal("fetch", vi.fn(() => new Promise(() => {})));
  render(<Panel />);
  expect(screen.getByRole("status",{name:"Loading jobs"})).toBeInTheDocument();
  expect(document.querySelectorAll(".loading-skeleton").length).toBeGreaterThan(1);
  expect(screen.queryByText("Start your first job")).toBeNull();
});
for (const page of ["jobs","dashboard","settings"]) it(`ends the session on 401 and stops all ${page} polling`, async () => {
  vi.useFakeTimers();
  history.replaceState(null,"",`/#/${page}`);
  const fetcher = vi.fn(async () => new Response("",{status:401}));
  vi.stubGlobal("fetch", fetcher);
  render(<Panel />);
  await act(async () => {});
  expect(screen.getByRole("heading",{name:"Session ended"})).toBeInTheDocument();
  expect(screen.getByRole("button",{name:"Copy pactify sub panel"})).toBeInTheDocument();
  const count = fetcher.mock.calls.length;
  await act(async () => {await vi.advanceTimersByTimeAsync(60000);});
  expect(fetcher).toHaveBeenCalledTimes(count);
});
it("shows one 5xx banner and KPI placeholders, then retries", async () => {
  vi.useFakeTimers(); history.replaceState(null,"","/#/dashboard");
  const fetcher = vi.fn(async () => new Response("",{status:503}));
  vi.stubGlobal("fetch", fetcher); render(<Panel />);
  await act(async () => {});
  expect(screen.getAllByRole("alert")).toHaveLength(1);
  expect(document.querySelectorAll(".kpi strong")).toHaveLength(5);
  for (const node of document.querySelectorAll(".kpi strong")) expect(node).toHaveTextContent("—");
  expect(screen.queryAllByText("Unavailable")).toHaveLength(0);
  const count = fetcher.mock.calls.length;
  await act(async () => {await vi.advanceTimersByTimeAsync(4000);});
  expect(fetcher.mock.calls.length).toBeGreaterThan(count);
});
it("uses the task title, run number, Live default and heading focus in details", async () => {
  history.replaceState(null,"","/#/jobs/latest");
  const first = {...job,id:"first",title:"Build a local panel",status:"succeeded",resumeOf:"",resumedBy:["latest"]};
  vi.stubGlobal("fetch",vi.fn(async (path:string) => path.endsWith("/latest") ? Response.json({job,brief:"Task",markdown:"Done"}) : path.includes("?all") ? Response.json([first,job]) : path.endsWith("/events") ? new Response('event: done\ndata: {}\n\n') : Response.json({})));
  render(<Panel />);
  const modal = await screen.findByRole("dialog");
  const heading = await within(modal).findByRole("heading",{name:"Build a local panel"});
  await waitFor(() => expect(heading).toHaveFocus());
  expect(within(modal).getByText("Run 2 of 2")).toBeInTheDocument();
  expect(within(modal).getByRole("tab",{name:"Live"})).toHaveAttribute("aria-selected","true");
  expect(within(modal).getByRole("heading",{name:"Next steps"})).toBeInTheDocument();
  expect(within(modal).getByRole("button",{name:"Copy pactify sub cancel latest"})).toBeInTheDocument();
  expect(within(modal).queryByRole("button",{name:"Cancel"})).toBeNull();
  fireEvent.click(within(modal).getByRole("tab",{name:"Summary"}));
  expect(within(modal).getByText("Write access")).toBeInTheDocument();
});
it("shows CLI next steps on Needs-attention cards", () => {
  render(<JobList jobs={[{...job,status:"failed",resumeOf:"",actions:["resume"],attention:[{kind:"failed",message:"Check result"}]}]} readOnly />);
  expect(screen.getByRole("heading",{name:"Next steps"})).toBeInTheDocument();
  expect(screen.getByRole("button",{name:"Copy pactify sub logs latest"})).toBeInTheDocument();
});
it("uses plain wording for job metadata and Settings", async () => {
  render(<JobList jobs={[{...job,status:"failed",failureClass:"harness",resumeOf:"",attention:[]}]} />);
  expect(screen.getByText("Agent error")).toBeInTheDocument();
});

it("keeps the card's fallback title for untitled run-chain roots", async () => {
  history.replaceState(null,"","/#/jobs/latest");
  const first = {...job,id:"first",title:"",status:"succeeded",resumeOf:"",resumedBy:["latest"]};
  vi.stubGlobal("fetch",vi.fn(async (path:string) => path.endsWith("/latest") ? Response.json({job,brief:"Task",markdown:"Done"}) : path.includes("?all") ? Response.json([first,job]) : path.endsWith("/events") ? new Response('event: done\ndata: {}\n\n') : Response.json({})));
  render(<Panel />);
  const modal = await screen.findByRole("dialog");
  expect(await within(modal).findByRole("heading",{name:"first"})).toBeInTheDocument();
});

it("chooses the default tab from current detail data when the list is stale", async () => {
  history.replaceState(null,"","/#/jobs/latest");
  const fetcher=vi.fn(async (path:string) => path.endsWith("/latest") ? Response.json({job,brief:"Task",markdown:"Done"}) : path.includes("?all") ? Response.json([{...job,status:"succeeded"}]) : path.endsWith("/events") ? new Response('event: done\ndata: {}\n\n') : Response.json({}));
  vi.stubGlobal("fetch",fetcher);
  render(<Panel />);
  const modal = await screen.findByRole("dialog");
  expect(await within(modal).findByRole("tab",{name:"Live"})).toHaveAttribute("aria-selected","true");
});

it("names the settings command as code without displaying Markdown punctuation", () => {
  render(<Settings readOnly source={{jobsPath:"/jobs",costPath:"/cost",fetch:vi.fn(() => new Promise<Response>(() => {}))}} />);
  expect(screen.getByText("pactify serve",{selector:"code"}).parentElement).toHaveTextContent("Edit settings with pactify serve (dashboard) or the CLI.");
});

for (const status of ["succeeded", "failed"]) {
  it(`limits ${status} cards to two primary commands and keeps the full list in details`, async () => {
    const row: Job = {
      ...job, status, resumeOf: "", actions: ["resume", "apply", "discard"],
      worktree: {path: "/wt", baseCommit: "abc", status: "pending"},
      attention: [{kind: status === "failed" ? "failed" : "warnings", message: "Review changes"}, {kind: "worktree_pending", message: "Awaiting apply"}],
    };
    vi.stubGlobal("fetch", vi.fn(async (path: string) =>
      path.endsWith("/latest")
        ? Response.json({job: row, brief: "Task", markdown: "Done"})
        : Response.json([row]),
    ));
    render(<Panel />);
    const card = await screen.findByRole("article");
    const commands = within(card).getAllByRole("button", {name: /^Copy pactify sub /});
    expect(commands).toHaveLength(2);
    // Worktree approval stays with the delegating session, so cards never lead with apply or discard.
    expect(commands.map(button => button.getAttribute("aria-label"))).toEqual(
      ["Copy pactify sub run --resume latest --worktree", "Copy pactify sub logs latest"],
    );
    expect(within(card).queryByText(/Run a command in your terminal|Ctrl\+D/)).toBeNull();
    const review = within(card).getByRole("button", {name: "Review"});
    expect(review).toHaveAttribute("title", "More in details");
    expect(within(card).queryByRole("button", {name: "More in details"})).toBeNull();
    fireEvent.click(review);
    await act(async () => {window.dispatchEvent(new HashChangeEvent("hashchange"));});
    const modal = await screen.findByRole("dialog");
    const nextSteps = await within(modal).findByRole("region", {name: "Next steps"});
    expect(within(nextSteps).getAllByRole("button", {name: /^Copy pactify sub /})).toHaveLength(6);
    for (const command of ["apply latest", "discard latest", "run --resume latest --worktree", "logs latest", "result latest"]) {
      expect(within(nextSteps).getByRole("button", {name: `Copy pactify sub ${command}`})).toBeInTheDocument();
    }
    expect(within(nextSteps).getByText(/Run a command in your terminal/)).toHaveTextContent("Resume: type instructions, then Ctrl+D.");
  });
}

for (const readOnly of [false, true]) {
  it(`separates scrolling content from the pinned ${readOnly ? "panel" : "dashboard"} footer`, async () => {
    const row: Job = {...job, status:"succeeded", resumeOf:"", actions:["resume", "apply", "discard"], worktree:{path:"/wt",baseCommit:"abc",status:"pending"}};
    history.replaceState(null,"","/#/jobs/latest");
    vi.stubGlobal("fetch",vi.fn(async (path:string) => path.endsWith("/latest") ? Response.json({job:row,brief:"Task",markdown:"Done"}) : Response.json([row])));
    render(readOnly ? <Panel /> : <App />);
    const modal = await screen.findByRole("dialog");
    await within(modal).findByRole("tab", {name:"Summary"});
    const scroller = modal.querySelector<HTMLElement>(".detail-scroll");
    const footer = modal.querySelector<HTMLElement>(".detail-footer")!;
    expect(scroller).not.toBeNull();
    expect(scroller).toContainElement(within(modal).getByRole("tabpanel"));
    expect(scroller).not.toContainElement(footer);
    if (readOnly) {
      expect(within(footer as HTMLElement).getByRole("heading",{name:"Next steps"})).toBeInTheDocument();
      const commands = within(footer as HTMLElement).getByRole("region",{name:"Next steps"});
      expect(within(commands).getAllByRole("button",{name:/^Copy pactify sub /})).toHaveLength(5);
      expect(scroller!.querySelector(".next-steps")).toBeNull();
      expect(within(footer).queryByRole("button",{name:"Show commands"})).toBeNull();
    } else {
      expect(within(footer as HTMLElement).getByRole("button",{name:"Resume"})).toBeInTheDocument();
      for (const name of ["Apply", "Discard"]) expect(within(footer as HTMLElement).queryByRole("button",{name})).toBeNull();
    }
  });
}

// Legacy list behavior remains covered explicitly; pipeline defaults have their own suite.
beforeEach(() => { localStorage.setItem("pactify-jobs-view", "list"); });
afterEach(() => { localStorage.clear(); });
