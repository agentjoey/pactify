import { afterEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { groupLines, stations, stationFor } from "./pipeline";
import { Settings } from "./Settings";
import { initializeTheme } from "../../theme";
import { JobList } from "./Jobs";
import type { Job } from "./types";
const job = (id: string, extra: Partial<Job> = {}): Job => ({id,title:id,status:"succeeded",repo:"/repo/pactify",harness:"codex",modelRequested:"model",cwd:"/repo",access:"write",configMode:"minimal",duration:"1m",createdAt:new Date().toISOString(),inputTokens:0,outputTokens:0,attention:[],resumeOf:"",resumedBy:[],...extra});
afterEach(() => { cleanup(); localStorage.clear(); history.replaceState(null,"","/"); });
for (const status of ["queued","running","retrying","succeeded","failed","canceled"])
  for (const kind of ["", "worktree_pending", "failed", "quota", "warnings"])
    it(`maps ${status}/${kind || 'none'} to the existing section rule`, () => {
      // A pending worktree alone is approved in the delegating session, not here.
      const expected = status === "queued" ? "Queued" : ["running","retrying"].includes(status) ? "Running" : kind && kind !== "worktree_pending" ? "Needs you" : "Shipped";
      expect(stations.find(s => s.key === stationFor(job("a", {status,attention:kind ? [{kind,message:kind}] : []})))?.name).toBe(expected);
    });
it("groups lines by project name, harness and one line", () => {
  const jobs = [job("a",{createdAt:"2026-10-01T00:00:00Z"}), job("b",{repo:"/other/pactify",harness:"kimi",createdAt:"2026-10-02T00:00:00Z"}),job("c",{repo:"/repo/tool",createdAt:"2026-10-03T00:00:00Z"})];
  expect(groupLines(jobs,"project", Date.parse("2026-10-29T00:00:00Z")).map(l => [l.name,l.jobs.length])).toEqual([["tool",1],["pactify",2]]);
  expect(groupLines(jobs,"harness", Date.parse("2026-10-29T00:00:00Z")).map(l => [l.name,l.jobs.length])).toEqual([["codex",2],["kimi",1]]);
  expect(groupLines(jobs,"one", Date.parse("2026-10-29T00:00:00Z"))[0].jobs).toHaveLength(3);
});
it("defaults to Lines, expands Shipped in place, and persists List", () => {
  const jobs = Array.from({length:5},(_,i)=>job(`shipped-${i}`));
  const view=render(<JobList jobs={jobs}/>);
  expect(screen.getByRole("button",{name:"Lines"})).toHaveAttribute("aria-pressed","true");
  expect(screen.getAllByText(/^shipped-/, {selector:"a"})).toHaveLength(3);
  fireEvent.click(screen.getByRole("button",{name:"+2 more"}));
  expect(screen.getAllByText(/^shipped-/, {selector:"a"})).toHaveLength(5);
  fireEvent.click(screen.getByRole("button",{name:"List"}));
  view.unmount();render(<JobList jobs={jobs}/>);
  expect(screen.getByRole("button",{name:"List"})).toHaveAttribute("aria-pressed","true");
});

it("keeps station shortcuts, job navigation and detail links in Lines", () => {
  render(<JobList jobs={[job("waiting",{status:"queued"}),job("running",{status:"running"}),job("review",{attention:[{kind:"warnings",message:"Review"}]})]}/>);
  fireEvent.keyDown(window,{key:"j"});expect(screen.getByRole("link",{name:"waiting"})).toHaveFocus();
  fireEvent.keyDown(document.activeElement!,{key:"l"});expect(screen.getByRole("link",{name:"running"})).toHaveFocus();
  fireEvent.keyDown(document.activeElement!,{key:"l"});expect(screen.getByRole("link",{name:"review"})).toHaveFocus();
  fireEvent.keyDown(document.activeElement!,{key:"h"});expect(screen.getByRole("link",{name:"running"})).toHaveFocus();
  fireEvent.keyDown(document.activeElement!,{key:"Enter"});expect(location.hash).toBe("#/jobs/running");
});
it("keeps panel Lines copyable and suppresses writes even with a writable source", () => {
  const write=vi.fn();
  render(<JobList readOnly source={{jobsPath:"/jobs",costPath:"/cost",fetch:vi.fn(),write}} jobs={[job("ready",{actions:["apply","discard","resume"],attention:[{kind:"warnings",message:"Check"},{kind:"worktree_pending",message:"Ready"}],worktree:{path:"/wt",baseCommit:"abc",status:"pending"}})]}/>);
  expect(screen.getByRole("button",{name:"Copy pactify sub run --resume ready --worktree"})).toBeInTheDocument();
  expect(screen.queryByRole("button",{name:/^Copy pactify sub (apply|discard) /})).toBeNull();
  expect(screen.queryByRole("button",{name:/^(Apply|Discard|Resume|Cancel)$/})).toBeNull();expect(write).not.toHaveBeenCalled();
});

it("offers manual theme selection in Settings Appearance", () => {
  initializeTheme();
  render(<Settings readOnly source={{jobsPath:"/jobs",costPath:"/cost",fetch:vi.fn(()=>new Promise<Response>(()=>{}))}}/>);
  fireEvent.click(screen.getByRole("button",{name:"Appearance"}));
  fireEvent.click(screen.getByRole("button",{name:"Light"}));
  initializeTheme();expect(document.documentElement.dataset.theme).toBe("light");
  fireEvent.click(screen.getByRole("button",{name:"Dark"}));
  expect(document.documentElement.dataset.theme).toBe("dark");
});

it("discloses line filters without hiding search or breaking their values", () => {
  render(<JobList jobs={[job("codex-job"),job("kimi-job",{harness:"kimi"})]}/>);
  const button=screen.getByRole("button",{name:"Filters"});
  expect(button).toHaveAttribute("aria-expanded","false");
  expect(screen.queryByRole("combobox",{name:"Harness"})).toBeNull();
  fireEvent.click(button);
  fireEvent.change(screen.getByRole("combobox",{name:"Harness"}),{target:{value:"kimi"}});
  expect(screen.queryByRole("link",{name:"codex-job"})).toBeNull();
  expect(screen.getByRole("link",{name:"kimi-job"})).toBeInTheDocument();
  fireEvent.click(button);
  expect(screen.getByRole("searchbox")).toBeVisible();
});
