import type { Job } from "./types";
import { projectName, sectionFor, type Section } from "./statistics";

export type LineGrouping = "project" | "harness" | "one";
export const stations = [
  { key: "queued", name: "Queued", number: "01", tone: "muted" },
  { key: "progress", name: "Running", number: "02", tone: "info" },
  { key: "attention", name: "Needs you", number: "03", tone: "warn" },
  { key: "completed", name: "Shipped", number: "04", tone: "ok" },
] as const;
export const stationFor = (job: Job): Section => sectionFor(job);

export function groupLines(jobs: Job[], grouping: LineGrouping) {
  const lines = new Map<string, Job[]>();
  for (const job of jobs) {
    const name = grouping === "project" ? projectName(job.repo)
      : grouping === "harness" ? job.harness : "All jobs";
    const line = lines.get(name);
    if (line) line.push(job);
    else lines.set(name, [job]);
  }
  return [...lines].sort(([a], [b]) => a.localeCompare(b)).map(([name, jobs]) => ({ name, jobs }));
}

export function endedToday(job: Job, now = new Date()) {
  if (!job.finishedAt) return false;
  return new Date(job.finishedAt).toDateString() === now.toDateString() && +new Date(job.finishedAt) <= +now;
}

export function relativeTime(value: string, now = Date.now()) {
  const seconds = Math.max(0, Math.floor((now - Date.parse(value)) / 1000));
  return !Number.isFinite(seconds) ? "unknown"
    : seconds < 60 ? `${seconds}s`
    : seconds < 3600 ? `${Math.floor(seconds / 60)}m`
    : seconds < 86400 ? `${Math.floor(seconds / 3600)}h` : `${Math.floor(seconds / 86400)}d`;
}

export function stationStat(key: Section, jobs: Job[], now = Date.now()) {
  const rows = jobs.filter(job => stationFor(job) === key);
  if (key === "queued") {
    const oldest = rows.reduce<Job | undefined>((a, b) => !a || b.createdAt < a.createdAt ? b : a, undefined);
    return oldest ? `oldest waiting ${relativeTime(oldest.createdAt, now)}` : "nothing waiting";
  }
  if (key === "progress") return rows.length ? `${rows.length} in progress` : "all lines idle";
  if (key === "attention") {
    const toApply = rows.filter(job => job.actions?.includes("apply") || job.attention.some(a => a.kind === "worktree_pending")).length;
    return `${toApply} to apply · ${rows.filter(job => job.status === "failed").length} failed`;
  }
  return `${rows.filter(job => endedToday(job, new Date(now))).length} done today`;
}

export function stationTodayCount(key: Section, jobs: Job[], now = new Date()) {
  return jobs.filter(job => {
    if (stationFor(job) !== key) return false;
    if (key === "completed") return endedToday(job, now);
    const time = key === "progress" ? job.startedAt || job.createdAt
      : key === "attention" ? job.finishedAt || job.createdAt : job.createdAt;
    return new Date(time).toDateString() === now.toDateString() && +new Date(time) <= +now;
  }).length;
}

// Use the layout's scroll padding as the single source for the sticky rail offset.
export function stationLeft(board: HTMLElement, station: HTMLElement) {
  const padding = parseFloat(getComputedStyle(board).scrollPaddingLeft) || 0;
  return station.getBoundingClientRect().left - board.getBoundingClientRect().left
    + board.scrollLeft - padding - board.clientLeft;
}
