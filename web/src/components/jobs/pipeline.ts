import type { Job } from "./types";
import { projectName, sectionFor, type Section } from "./statistics";

export type LineGrouping = "project" | "harness" | "one";
export const stations = [
  { key: "progress", name: "Running", number: "01", tone: "info" },
  { key: "attention", name: "Needs you", number: "02", tone: "warn" },
  { key: "review", name: "In review", number: "03", tone: "muted" },
  { key: "completed", name: "Shipped", number: "04", tone: "ok" },
] as const;
export const stationFor = (job: Job): Section => sectionFor(job);

export function groupLines(jobs: Job[], grouping: LineGrouping, now: number) {
  const lines = new Map<string, Job[]>();
  for (const job of jobs) {
    const name = grouping === "project" ? projectName(job.repo)
      : grouping === "harness" ? job.harness : "All jobs";
    const line = lines.get(name);
    if (line) line.push(job);
    else lines.set(name, [job]);
  }
  return [...lines].map(([name, jobs]) => {
    const latestActivity = jobs.reduce((latest, job) => {
      for (const value of [job.createdAt, job.startedAt, job.finishedAt]) {
        const time = Date.parse(value || "");
        if (Number.isFinite(time)) latest = Math.max(latest, time);
      }
      return latest;
    }, 0);
    const hasWork = jobs.some(job => stationFor(job) !== "completed");
    const active = hasWork || latestActivity > now - 3 * 86400000;
    return { name, jobs, latestActivity, active, hasWork };
  }).sort((a, b) => Number(b.hasWork) - Number(a.hasWork)
    || Number(b.active) - Number(a.active)
    || b.latestActivity - a.latestActivity || a.name.localeCompare(b.name));
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
  if (key === "review") return rows.length ? `${rows.length} with the delegating session` : "nothing to review";
  if (key === "progress") return rows.length ? `${rows.length} in progress` : "all lines idle";
  if (key === "attention") {
    const failed = rows.filter(job => job.status === "failed").length;
    return `${failed} failed${rows.length > failed ? ` · ${rows.length - failed} with warnings` : ""}`;
  }
  return `${rows.filter(job => endedToday(job, new Date(now))).length} done today`;
}

export function stationTodayCount(key: Section, jobs: Job[], now = new Date()) {
  return jobs.filter(job => {
    if (stationFor(job) !== key) return false;
    if (key === "completed") return endedToday(job, now);
    const time = key === "progress" ? job.startedAt || job.createdAt
      : job.finishedAt || job.createdAt;
    return new Date(time).toDateString() === now.toDateString() && +new Date(time) <= +now;
  }).length;
}

// Use the layout's scroll padding as the single source for the sticky rail offset.
export function stationLeft(board: HTMLElement, station: HTMLElement) {
  const padding = parseFloat(getComputedStyle(board).scrollPaddingLeft) || 0;
  return station.getBoundingClientRect().left - board.getBoundingClientRect().left
    + board.scrollLeft - padding - board.clientLeft;
}
