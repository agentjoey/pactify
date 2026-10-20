import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { JobList } from "../src/components/jobs/Jobs";
import { JobDetail } from "../src/components/jobs/JobDetail";
import { DiffView } from "../src/components/jobs/DiffView";
import type { Job } from "../src/components/jobs/types";
const job: Job = {
  id:"completed", title:"A long completed task title", harness:"codex", modelRequested:"model",
  repo:"/workspace/project", cwd:"/workspace/project", status:"succeeded", duration:"2m",
  createdAt:"2026-10-20T12:00:00Z", access:"write", configMode:"minimal", inputTokens:1234,
  outputTokens:42, attention:[], resumeOf:"", resumedBy:[], actions:["resume","apply","discard"],
  worktree:{path:"/worktree",baseCommit:"abc",status:"pending"},
};
afterEach(() => {cleanup(); vi.unstubAllGlobals(); history.replaceState(null,"","/");});
it("gives compact history eight column cells and retains the full truncated title", () => {
  render(<JobList jobs={[job]} readOnly />);
  fireEvent.click(screen.getByRole("button",{name:"Compact list"}));
  const row = within(screen.getByRole("list",{name:"Completed jobs"})).getByRole("listitem");
  expect([...row.querySelectorAll("[data-column]")].map(cell => cell.getAttribute("data-column")))
    .toEqual(["status","title","harness","project","duration","tokens","cost","time"]);
  const title = within(row).getByRole("link",{name:job.title});
  expect(title).toHaveClass("compact-title");
  expect(title).toHaveAttribute("title",job.title);
  expect(row.querySelector('[data-column="project"]')).toHaveTextContent("project");
  expect(row.querySelector('[data-column="tokens"]')).toHaveTextContent("1.2K in / 42 out");
});
for (const narrow of [false,true]) it(`renders one command list in ${narrow ? "narrow" : "wide"} mode and handles resizing`, async () => {
  let change = () => {};
  const media = {matches:narrow, addEventListener:vi.fn((_event:string, listener:()=>void) => {change=listener;}),removeEventListener:vi.fn()};
  vi.stubGlobal("matchMedia",vi.fn(() => media));
  const source = {jobsPath:"/api/jobs",costPath:"/api/cost",fetch:vi.fn(async () => Response.json({job}))};
  const {container} = render(<JobDetail id={job.id} jobs={[job]} source={source} readOnly
    onChanged={()=>{}} notify={()=>{}} initialTab="summary" />);
  await screen.findByRole("heading",{name:"Usage"});
  const footer = container.querySelector(".detail-footer")! as HTMLElement;
  if (narrow) {
    expect(container.querySelectorAll(".next-steps")).toHaveLength(0);
    fireEvent.click(within(footer).getByRole("button",{name:"Show commands"}));
    expect(within(footer).getByRole("button",{name:"Hide commands"})).toHaveAttribute("aria-expanded","true");
    expect(container.querySelector(".detail-scroll")).toContainElement(screen.getByRole("region",{name:"Terminal commands"}));
  } else {
    expect(within(footer).getByRole("region",{name:"Next steps"})).toBeInTheDocument();
    expect(screen.queryByRole("button",{name:"Show commands"})).toBeNull();
    expect(container.querySelector(".detail-scroll .next-steps")).toBeNull();
  }
  expect(container.querySelectorAll(".next-steps")).toHaveLength(1);
  expect(container.querySelectorAll(".next-steps .copy-command")).toHaveLength(5);
  await act(async () => {media.matches=!narrow; change();});
  // Wide mode owns the list in the footer; narrow mode initially hides it.
  expect(container.querySelectorAll(".next-steps")).toHaveLength(narrow ? 1 : 0);
});
for (const [header,path] of [
  ["a/src/main.go b/src/main.go","src/main.go"],
  ["a/src/old.go b/src/new.go","src/old.go → src/new.go"],
  ['"a/src/sp ace.go" "b/src/sp ace.go"',"src/sp ace.go"],
  ["a/src/sp ace.go b/src/sp ace.go","src/sp ace.go"],
  [String.raw`"a/src/\344\270\255.go" "b/src/\344\270\255.go"`,"src/中.go"],
]) it(`normalizes the diff header ${header}`, () => {
  const {container}=render(<DiffView job={job} diff={{patch:`diff --git ${header}\n-old\n+new\n`,files:[]}} />);
  expect(container.querySelector("summary")?.textContent?.trim()).toBe(`${path} +1 / −1`);
});
