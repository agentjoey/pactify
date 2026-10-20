import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { Jobs } from "../src/components/jobs/Jobs";
import { AppContent } from "../src/App";
import type { Job } from "../src/components/jobs/types";

const job: Job = {id:"first", title:"First → café · −1", status:"running", harness:"codex", modelRequested:"model", access:"write", configMode:"minimal", repo:"/repo", cwd:"/repo", createdAt:"2026-10-20T12:00:00Z", duration:"1m", inputTokens:0, outputTokens:0, attention:[], resumeOf:"", resumedBy:[]};
const jobs = [job, {...job,id:"second",title:"Second",status:"queued"}, ...["third","fourth","fifth"].map((id,i) => ({...job,id,title:id,status:"succeeded",createdAt:`2026-10-20T11:00:0${2-i}Z`}))];
afterEach(() => {cleanup(); vi.useRealTimers(); vi.restoreAllMocks(); history.replaceState(null,"","/"); document.querySelector('link[rel="icon"]')?.remove();});

for (const standalone of [true,false]) it(`navigates visible jobs with shortcuts in the ${standalone ? "panel" : "dashboard"}`, async () => {
  render(<Jobs jobs={jobs} loaded standalone={standalone} />);
  const first=screen.getByRole("link",{name:job.title});
  const second=screen.getByRole("link",{name:"Second"});
  fireEvent.keyDown(window,{key:"j"}); expect(first).toHaveFocus();
  fireEvent.keyDown(first,{key:"j"}); expect(second).toHaveFocus();
  fireEvent.keyDown(second,{key:"k"}); expect(first).toHaveFocus();
  fireEvent.keyDown(first,{key:"/"}); expect(screen.getByRole("searchbox")).toHaveFocus();
  for (const key of ["j","k","/","Enter","?"]) fireEvent.keyDown(screen.getByRole("searchbox"),{key});
  expect(screen.getByRole("searchbox")).toHaveFocus(); expect(location.hash).toBe("");
  screen.getByRole("searchbox").blur();
  fireEvent.keyDown(window,{key:"?"});
  expect(screen.getByRole("region",{name:"Keyboard shortcuts"})).toHaveTextContent("Open details");
  fireEvent.keyDown(window,{key:"Escape"});
  expect(screen.queryByRole("region",{name:"Keyboard shortcuts"})).toBeNull();
  first.focus(); fireEvent.keyDown(first,{key:"Enter"});
  expect(location.hash).toBe("#/jobs/first");
});

it("skips collapsed previews, follows compact rows and ignores modifiers, selects and editable text", () => {
  render(<Jobs jobs={jobs} loaded standalone />);
  const third=screen.getByRole("link",{name:"third"});
  third.focus(); fireEvent.keyDown(third,{key:"j"}); expect(third).toHaveFocus();
  fireEvent.click(screen.getByRole("button",{name:"Compact list"}));
  screen.getByRole("link",{name:"third"}).focus();
  fireEvent.keyDown(document.activeElement!,{key:"j"}); expect(screen.getByRole("link",{name:"fourth"})).toHaveFocus();
  for (const props of [{ctrlKey:true},{metaKey:true},{altKey:true},{isComposing:true}]) {
    fireEvent.keyDown(document.activeElement!,{key:"j",...props}); expect(screen.getByRole("link",{name:"fourth"})).toHaveFocus();
  }
  const project=screen.getByLabelText("Project"); project.focus(); fireEvent.keyDown(project,{key:"/"}); expect(project).toHaveFocus();
  const editable=document.createElement("div"); editable.setAttribute("contenteditable","true"); editable.tabIndex=0; document.body.append(editable);
  editable.focus(); fireEvent.keyDown(editable,{key:"/"}); expect(editable).toHaveFocus(); editable.remove();
  fireEvent.change(screen.getByRole("searchbox"),{target:{value:"Second"}});
  fireEvent.keyDown(window,{key:"j"}); expect(screen.getByRole("link",{name:"Second"})).toHaveFocus();
});

it("updates a bundled status favicon after polls, prioritizing attention over running", async () => {
  vi.useFakeTimers(); let rows=jobs;
  const source={jobsPath:"/api/jobs",costPath:"/api/cost",fetch:vi.fn(async (path:string)=>Response.json(path.endsWith("/info") ? {} : rows))};
  render(<AppContent source={source} readOnly />); await act(async()=>{});
  const icon=()=>document.querySelector('link[rel="icon"]');
  expect(icon()).toHaveAttribute("href","/favicon-running.svg");
  rows=[...jobs,{...job,id:"review",status:"failed",attention:[{kind:"failed",message:"Review"}]}];
  await act(async()=>{await vi.advanceTimersByTimeAsync(2000);});
  expect(icon()).toHaveAttribute("href","/favicon-attention.svg");
  rows=[]; await act(async()=>{await vi.advanceTimersByTimeAsync(2000);});
  expect(icon()).toHaveAttribute("href","/favicon-neutral.svg");
});

it("lays out mobile KPIs as two wide tiles followed by three tiles", () => {
  const css=readFileSync("src/components/jobs/jobs.css","utf8").split("@media (max-width: 700px)").slice(1).join("\n");
  expect(css).toMatch(/\.kpi-grid\s*\{[^}]*grid-template-columns: repeat\(6, minmax\(0, 1fr\)\)/);
  expect(css).toMatch(/\.kpi-grid > :nth-child\(-n\+2\)\s*\{[^}]*grid-column: span 3/);
  expect(css).toMatch(/\.kpi-grid > \*\s*\{[^}]*grid-column: span 2/);
});

it("keeps all self-hosted font subsets below 250 KB combined", () => {
  const sizes=readdirSync("public/fonts").filter(name=>name.endsWith(".woff2")).map(name=>statSync(`public/fonts/${name}`).size);
  expect(sizes).toHaveLength(3);
  expect(sizes.reduce((sum,size)=>sum+size,0)).toBeLessThan(250000);
});
