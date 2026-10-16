import type { Job } from "./Jobs";

export type JobColumn = "running" | "attention" | "succeeded" | "failed";
export const JOB_COLUMNS: {key: JobColumn; label: string; empty: string; color: string}[] = [
  {key: "running", label: "RUNNING", empty: "No running jobs", color: "var(--color-role-design)"},
  {key: "attention", label: "NEEDS ATTENTION", empty: "Nothing needs attention", color: "var(--color-warn)"},
  {key: "succeeded", label: "SUCCEEDED", empty: "No succeeded jobs", color: "var(--color-success)"},
  {key: "failed", label: "FAILED / CANCELED", empty: "No failed jobs", color: "var(--color-danger)"},
];

export function jobColumn(job: Job): JobColumn {
  if (["queued", "running", "retrying"].includes(job.status)) return "running";
  if (job.attention.length) return "attention";
  return job.status === "succeeded" ? "succeeded" : "failed";
}

export interface JobChain {
  root: Job;
  latest: Job;
  runs: Job[];
  rework: number;
}
const chronological = (a: Job, b: Job) => a.createdAt.localeCompare(b.createdAt) || a.id.localeCompare(b.id);

// Only explicit links establish a chain. Undirected components keep one-sided
// links and forks together; causal ordering handles equal timestamps. Missing
// peers are ignored, and cycles break deterministically by creation time/id.
export function mergeJobChains(jobs: Job[]): JobChain[] {
  const byId = new Map(jobs.map(job => [job.id, job]));
  const neighbors = new Map([...byId.keys()].map(id => [id, new Set<string>()]));
  const children = new Map([...byId.keys()].map(id => [id, new Set<string>()]));
  const resumes = new Map([...byId.keys()].map(id => [id, new Set<string>()]));
  function link(parent: string, child: string, resume = false) {
    if (!byId.has(parent) || !byId.has(child) || parent === child) return;
    neighbors.get(parent)!.add(child);
    neighbors.get(child)!.add(parent);
    children.get(parent)!.add(child);
    if (resume) resumes.get(parent)!.add(child);
  }
  for (const job of byId.values()) {
    link(job.resumeOf, job.id, true);
    for (const id of job.resumedBy) link(job.id, id, true);
    if (job.fallbackFrom) link(job.fallbackFrom, job.id);
    if (job.fallbackTo) link(job.id, job.fallbackTo);
  }
  const seen = new Set<string>();
  const chains: JobChain[] = [];
  for (const job of [...byId.values()].sort(chronological)) {
    if (seen.has(job.id)) continue;
    const component: string[] = [];
    const stack = [job.id];
    seen.add(job.id);
    while (stack.length) {
      const id = stack.pop()!;
      component.push(id);
      for (const peer of neighbors.get(id)!) {
        if (!seen.has(peer)) { seen.add(peer); stack.push(peer); }
      }
    }
    const indegree = new Map(component.map(id => [id, 0]));
    for (const id of component) for (const child of children.get(id)!) indegree.set(child, indegree.get(child)! + 1);
    const remaining = new Set(component);
    const runs: Job[] = [];
    while (remaining.size) {
      const ordered = [...remaining].map(id => byId.get(id)!).sort(chronological);
      const next = ordered.find(row => indegree.get(row.id) === 0) ?? ordered[0];
      remaining.delete(next.id);
      runs.push(next);
      for (const child of children.get(next.id)!) indegree.set(child, indegree.get(child)! - 1);
    }
    chains.push({root: runs[0], latest: runs[runs.length - 1], runs,
      rework: component.reduce((n, id) => n + resumes.get(id)!.size, 0)});
  }
  return chains.sort((a, b) => chronological(b.latest, a.latest));
}
