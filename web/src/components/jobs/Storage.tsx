import { useState } from "react";
import { CopyCommand } from "./NextSteps";
import { useResource } from "./data";
import type { JobsSource } from "./source";
import { StorageCleanup } from "./StorageCleanup";
import { panelBuild } from "../../buildTarget";

export interface StorageItem { path: string; bytes: number; reason: string; protected: boolean; action?: string }
export interface StorageReasonSummary { action: string; reason: string; count: number; bytes: number; examples: string[] }
export interface StorageLocation { summaries?: StorageReasonSummary[]; name: string; bytes: number; reclaimable: number; items?: StorageItem[]; count?: number; protectedCount?: number; breakdown?: Record<string, number> }
export interface StoragePlan {
  version: string;
  policy: { olderThan: string; cacheMaxBytes: number; interval: string };
  locations: StorageLocation[];
  staleWorktrees: string[];
  failures: string[];
}
export interface StorageRun { startedAt: string; endedAt?: string; removed: { location: string; bytes: number; items: number; sessions?: number; databaseBytes?: number; databaseMeasured?: boolean }[]; failures: string[] }
interface StorageSnapshot extends StoragePlan { lastRun: StorageRun | null }
export function storageBytes(n: number): string {
 if (n < 0) return `-${storageBytes(-n)}`;
  if (n < 1000) return `${n} B`;
  if (n < 1e6) return `${Number((n / 1e3).toFixed(1))} KB`;
  if (n < 1e9) return `${Number((n / 1e6).toFixed(1))} MB`;
  return `${Number((n / 1e9).toFixed(2))} GB`;
}
// Go durations from storage.json ("336h") read as days or hours; anything else is shown as is.
export function storageDuration(d: string): string {
  const m = /^(\d+)h(?:0m)?(?:0s)?$/.exec(d);
  if (!m) return d;
  const hours = Number(m[1]);
  if (hours % 24 === 0) return hours === 24 ? "1 day" : `${hours / 24} days`;
  return hours === 1 ? "1 hour" : `${hours} hours`;
}
export function storageItems(n: number): string {
  return `${n.toLocaleString("en-US")} ${n === 1 ? "item" : "items"}`;
}
const valid = (value: unknown) => {
  const v = value as StorageSnapshot;
  return !!v?.policy && Array.isArray(v.locations) && Array.isArray(v.staleWorktrees) && Array.isArray(v.failures);
};
export function Storage({ source, readOnly = false }: { source: JobsSource; readOnly?: boolean }) {
  const path = source.jobsPath.replace(/\/jobs$/, "/storage");
  const status = useResource<StorageSnapshot>(source, path, 60000, valid);
  const [notice, setNotice] = useState("");
  const data = status.data;
  return <section className="panel-section" aria-label="Storage">
    <div className="section-heading"><h2>Storage</h2>
      {!panelBuild && !readOnly && source.write && data && <StorageCleanup source={source} path={path} onChanged={(text) => { setNotice(text); status.refresh(); }} />}
    </div>
    {status.error && <p role="alert">Storage unavailable: {status.error}</p>}
    {!data && !status.error && <p>Loading storage…</p>}
    {notice && <p role="status">{notice}</p>}
    {data && <>
      <p>Retention: {storageDuration(data.policy.olderThan)} · Cache cap: {storageBytes(data.policy.cacheMaxBytes)} · Automatic cleanup interval: {storageDuration(data.policy.interval)}</p>
      <p className="panel-caption">Finished resume chains are kept until every job is eligible. Pending worktrees are retained. Sizes are estimates, refreshed at most once a minute.</p>
      <dl>{data.locations.map(l => <div key={l.name} className="storage-location">
        <dt><code>{l.name}/</code></dt><dd>{storageBytes(l.bytes)} · {storageBytes(l.reclaimable)} reclaimable</dd>
        {Object.entries(l.breakdown || {}).map(([name, bytes]) => <dd key={name} className="panel-caption">{name}: {storageBytes(bytes)}</dd>)}
      </div>)}</dl>
      <p>Last prune: {data.lastRun ? `${data.lastRun.startedAt} · ${data.lastRun.endedAt ? data.lastRun.failures.length ? "completed with failures" : "completed" : "running or interrupted"}` : "Never run"}</p>
      {data.lastRun?.removed.map(l => <p className="panel-caption" key={l.location}>{l.location}: {storageItems(l.items)} removed · {storageBytes(l.bytes)} freed{storageRemovalDetails(l)}</p>)}
      {data.lastRun?.failures.map((f, i) => <p role="alert" key={i}>{f}</p>)}
      {data.staleWorktrees.length > 0 && <p className="version-warning">Stale worktrees (kept): {data.staleWorktrees.join(", ")}</p>}
      {data.failures.map((f, i) => <p role="alert" key={i}>{f}</p>)}
    </>}
    {(panelBuild || readOnly || !source.write) && <CopyCommand command="pactify sub prune --yes" />}
  </section>;
}

export function storageSummaries(location: StorageLocation): StorageReasonSummary[] {
  if (location.summaries) return location.summaries;
  const groups = new Map<string, StorageReasonSummary>();
  for (const item of location.items || []) {
    const action = item.protected ? "KEEP" : item.action === "sweep-sessions" ? "SWEEP SESSIONS" : "REMOVE";
    const key = action + ":" + item.reason;
    let group = groups.get(key);
    if (!group) { group = { action, reason: item.reason, count: 0, bytes: 0, examples: [] }; groups.set(key, group); }
    group.count++; group.bytes += item.bytes;
    if (group.examples.length < 5) group.examples.push(item.path);
  }
  return [...groups.values()];
}

export function storageRemovalDetails(l: StorageRun["removed"][number]): string {
  const parts: string[] = [];
  if (l.sessions || l.databaseMeasured || l.databaseBytes !== undefined) parts.push(`${l.sessions || 0} sessions deleted`);
  if (l.databaseMeasured || l.databaseBytes !== undefined) parts.push(`database size delta: ${storageBytes(l.databaseBytes || 0)}`);
  return parts.length ? " · " + parts.join(" · ") : "";
}
