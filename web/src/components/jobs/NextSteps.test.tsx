import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { NextSteps, nextCommands } from "./NextSteps";
import { JobActions } from "./JobActions";
import type { Job } from "./types";
import { dashboardJobsSource } from "./source";
const job = (extra: Partial<Job> = {}): Job => ({id:"job-123", title:"Task", status:"succeeded", harness:"codex", modelRequested:"model", access:"write", configMode:"minimal", repo:"/repo", cwd:"/repo", createdAt:"2026-10-20", duration:"1m", inputTokens:0, outputTokens:0, attention:[], resumeOf:"", resumedBy:[], ...extra});
const commands = (extra: Partial<Job>) => nextCommands(job(extra));
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });
for (const status of ["running", "retrying", "queued"]) it(`offers only cancel and logs for ${status}`, () => {
  expect(commands({status, actions:["cancel"]})).toEqual(["pactify sub cancel job-123", "pactify sub logs job-123"]);
});
for (const status of ["succeeded", "failed", "canceled"]) it(`uses server actions for ${status} with a pending worktree`, () => {
  expect(commands({status, actions:["resume", "apply", "discard"], worktree:{path:"/wt", baseCommit:"abc", status:"pending"}, attention:[{kind:"worktree_pending", message:"Review"}]})).toEqual([
    "pactify sub apply job-123", "pactify sub discard job-123", "pactify sub run --resume job-123 --worktree", "pactify sub logs job-123", "pactify sub result job-123",
  ]);
});
it("does not guess unavailable actions for old jobs or busy shared worktrees", () => {
  for (const actions of [undefined, []]) expect(commands({actions, worktree:{path:"/wt",baseCommit:"abc",status:"pending"}})).toEqual(["pactify sub logs job-123", "pactify sub result job-123"]);
});
it("offers dismiss only for unacknowledged terminal failures/warnings with any worktree state", () => {
  for (const state of [undefined, "pending", "applied", "discarded"]) {
    const worktree = state ? {path:"/wt",baseCommit:"abc",status:state} : undefined;
    expect(commands({status:"failed", actions:[], worktree, attention:[{kind:"failed",message:"Failed"}]})).toContain("pactify sub dismiss job-123");
    expect(commands({status:"failed", actions:[], worktree, dismissedAt:"2026-10-20", attention:[]})).not.toContain("pactify sub dismiss job-123");
  }
});
it("resumes without --worktree for jobs without a worktree and rejects unknown actions", () => {
  expect(commands({actions:["resume", "unknown"]})).toEqual(["pactify sub run --resume job-123", "pactify sub logs job-123", "pactify sub result job-123"]);
});
it("never copies arbitrary ids, tokens or URLs into shell commands", () => {
  for (const id of ["$(echo secret)", "https://example.test/?token=secret", "-flag", "id\ncommand"]) expect(nextCommands(job({id, actions:["resume"]}))).toEqual([]);
});
it("copies a CLI command and reports clipboard failure with manual-copy guidance", async () => {
  const writeText = vi.fn().mockResolvedValueOnce(undefined).mockRejectedValueOnce(Error("denied"));
  vi.stubGlobal("navigator", {clipboard:{writeText}});
  render(<NextSteps job={job({status:"running",actions:["cancel"]})} />);
  const copy = screen.getByRole("button", {name:"Copy pactify sub cancel job-123"});
  fireEvent.click(copy);
  expect(await screen.findByText("Copied")).toBeInTheDocument();
  expect(writeText).toHaveBeenCalledWith("pactify sub cancel job-123");
  fireEvent.click(copy);
  expect(await screen.findByText("Could not copy. Select the command and copy it manually.")).toBeInTheDocument();
});
it("keeps write actions and suppresses them in read-only mode", () => {
  const props = {job:job({actions:["dismiss","resume"]}),source:dashboardJobsSource,onChanged:vi.fn(),notify:vi.fn()};
  const {rerender} = render(<JobActions {...props} />);
  for (const name of ["Dismiss","Resume"]) expect(screen.getByRole("button",{name})).toBeInTheDocument();
  rerender(<JobActions {...props} readOnly />);
  expect(screen.queryByRole("button")).toBeNull();
});

it("prioritizes logs and cancel in compact running steps without explanatory text", () => {
  render(<NextSteps job={job({status:"running", actions:["cancel"]})} compact />);
  expect(screen.getAllByRole("button").map(button => button.getAttribute("aria-label"))).toEqual([
    "Copy pactify sub logs job-123", "Copy pactify sub cancel job-123",
  ]);
  expect(screen.queryByText(/Run a command in your terminal|Ctrl\+D/)).toBeNull();
});

it("only shows the short resume hint in full steps that offer resume", () => {
  const {rerender} = render(<NextSteps job={job({actions:["resume"]})} />);
  expect(screen.getByText(/Resume: type instructions, then Ctrl\+D\./)).toBeInTheDocument();
  rerender(<NextSteps job={job({actions:[]})} />);
  expect(screen.getByText("Run a command in your terminal.")).toBeInTheDocument();
  expect(screen.queryByText(/Resume:|Ctrl\+D/)).toBeNull();
});
