import type { Job } from "./types";
import { mergeJobChains, type JobChain } from "./board";

export type Section = "progress" | "queued" | "attention" | "completed";
export function sectionFor(job: Job): Section {
  if (["running", "retrying"].includes(job.status)) return "progress";
  if (job.status === "queued") return "queued";
  // Worktree approval belongs to the delegating session, so a pending
  // worktree alone leaves the job completed rather than needing attention.
  return job.attention.some((a) => a.kind !== "worktree_pending") ? "attention" : "completed";
}
export const projectName = (repo: string) =>
  repo.replace(/\/$/, "").split("/").pop() || repo || "Unknown project";
export const shortNumber = (n: number) =>
  n >= 1e6
    ? `${+(n / 1e6).toFixed(1)}M`
    : n >= 1000
      ? `${+(n / 1000).toFixed(1)}K`
      : String(n);
export function tokens(job: Job) {
  const live = ["running", "retrying"].includes(job.status);
  return {
    input: live
      ? (job.activity?.inputTokens ?? job.inputTokens)
      : job.inputTokens,
    output: live
      ? (job.activity?.outputTokens ?? job.outputTokens)
      : job.outputTokens,
  };
}
export function durationSeconds(value: string) {
  return [...value.matchAll(/([\d.]+)(h|m|s)/g)].reduce(
    (n, m) => n + Number(m[1]) * ({ h: 3600, m: 60, s: 1 }[m[2]] || 0),
    0,
  );
}
export function formatDuration(seconds: number) {
  seconds = Math.max(0, Math.round(seconds));
  return seconds >= 3600
    ? `${Math.floor(seconds / 3600)}h ${Math.floor(seconds / 60) % 60}m ${seconds % 60}s`
    : seconds >= 60
      ? `${Math.floor(seconds / 60)}m ${seconds % 60}s`
      : `${seconds}s`;
}
function dayKey(value: string | Date) {
  const date = new Date(value);
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
}
export function completedDays(chains: JobChain[], now = new Date()) {
  const groups = new Map<string, JobChain[]>();
  for (const chain of chains) {
    const day = dayKey(chain.latest.finishedAt || chain.latest.createdAt);
    groups.set(day, [...(groups.get(day) || []), chain]);
  }
  const yesterday = new Date(now);
  yesterday.setDate(yesterday.getDate() - 1);
  return [...groups]
    .sort(([a], [b]) => b.localeCompare(a))
    .map(([day, rows]) => ({
      day,
      rows,
      label:
        day === dayKey(now)
          ? "Today"
          : day === dayKey(yesterday)
            ? "Yesterday"
            : new Date(day + "T12:00:00").toLocaleDateString("en-US", {
                month: "short",
                day: "numeric",
                year: "numeric",
              }),
    }));
}
export function dashboardKpis(jobs: Job[], now = new Date()) {
  const since = new Date(now);
  since.setHours(0, 0, 0, 0);
  since.setDate(since.getDate() - 6);
  const result = {
    progress: 0,
    queued: 0,
    attention: 0,
    succeeded: 0,
    failed: 0,
  };
  for (const { latest } of mergeJobChains(jobs)) {
    const section = sectionFor(latest);
    if (section !== "completed") result[section]++;
    const ended = Date.parse(latest.finishedAt || latest.createdAt);
    if (
      ended >= +since &&
      ended <= +now &&
      ["succeeded", "failed"].includes(latest.status)
    )
      result[latest.status as "succeeded" | "failed"]++;
  }
  return result;
}
export function dailyTokens(jobs: Job[], now = new Date()) {
  const days = Array.from({ length: 14 }, (_, i) => {
    const date = new Date(now);
    date.setDate(date.getDate() - 13 + i);
    return { day: dayKey(date), harnesses: {} as Record<string, number> };
  });
  for (const job of jobs) {
    const day = days.find(
      (d) => d.day === dayKey(job.finishedAt || job.createdAt),
    );
    if (!day) continue;
    const usage = tokens(job);
    day.harnesses[job.harness] =
      (day.harnesses[job.harness] || 0) + usage.input + usage.output;
  }
  return days;
}

export function formatCost(value: number | null | undefined) {
  if (value == null || !Number.isFinite(value)) return "—";
  return value < 0.01 ? "<$0.01" : `$${value.toFixed(2)}`;
}

export interface QuotaWindow {
  label: string;
  remaining: number;
  resetAt: string | null;
}
// Rate-limit summaries use the local MM-DD HH:mm format from sub/envelope.go.
export function parseQuotaWindows(
  text: string,
  observedAt: string,
): QuotaWindow[] {
  const windows: QuotaWindow[] = [];
  for (const part of text.split(";")) {
    const match = part.match(
      /(\d+)[- ]?(hour|day|minute|h|d|m)(?: window)?:\s*(\d+(?:\.\d+)?)%\s*(used|remaining)/i,
    );
    if (!match) continue;
    const percent = Number(match[3]);
    if (percent > 100) continue;
    const label = match[1] + match[2][0].toLowerCase();
    const reset = part.match(/resets\s+(\d{2})-(\d{2})\s+(\d{2}):(\d{2})/);
    let resetAt: string | null = null;
    const observed = new Date(observedAt);
    if (reset && Number.isFinite(+observed)) {
      // Choose the nearest year so snapshots around New Year retain their reset date.
      const dates = [-1, 0, 1].map(
        (offset) =>
          new Date(
            observed.getFullYear() + offset,
            Number(reset[1]) - 1,
            Number(reset[2]),
            Number(reset[3]),
            Number(reset[4]),
          ),
      );
      dates.sort((a, b) => Math.abs(+a - +observed) - Math.abs(+b - +observed));
      resetAt = dates[0].toISOString();
    }
    windows.push({
      label,
      remaining: match[4].toLowerCase() === "used" ? 100 - percent : percent,
      resetAt,
    });
  }
  return windows;
}

// Match sub cost: reported dollars take precedence over subscription labels.
export function jobCost(job: Job) {
  if (job.costUSD != null) return formatCost(job.costUSD);
  return ["codex", "kimi", "agy"].includes(job.harness) ? "subscription" : "unknown";
}
