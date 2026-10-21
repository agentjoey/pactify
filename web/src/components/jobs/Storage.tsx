import { useState } from "react";
import { Notice } from "../ui/Design";
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
export function Storage({ source, readOnly = false, embedded = false }: {
  source: JobsSource;
  readOnly?: boolean;
  embedded?: boolean;
}) {
  const path = source.jobsPath.replace(/\/jobs$/, "/storage");
  const status = useResource<StorageSnapshot>(source, path, 60000, valid);
  const [notice, setNotice] = useState("");
  const data = status.data;
  return <section className="panel-section storage-content" aria-label="Storage">
    <div className="section-heading">
      <h2>{embedded ? "Locations" : "Storage"}</h2>
      {!panelBuild && !readOnly && source.write && data && <StorageCleanup source={source} path={path} onChanged={(text) => { setNotice(text); status.refresh(); }} />}
    </div>
    {status.error && <p role="alert">Storage unavailable: {status.error}</p>}
    {!data && !status.error && <p>Loading storage…</p>}
    {notice && <Notice kind="ok">{notice}</Notice>}
    {data && <>
      <div className="storage-policy" aria-label="Cleanup policy">
        <span>Retention: {storageDuration(data.policy.olderThan)}</span>
        <span>Cache cap: {storageBytes(data.policy.cacheMaxBytes)}</span>
        <span>Automatic cleanup interval: {storageDuration(data.policy.interval)}</span>
      </div>
      <p className="panel-caption">Finished resume chains are kept until every job is eligible. Pending worktrees are retained. Sizes are estimates, refreshed at most once a minute.</p>
      <dl className="storage-locations">
        {data.locations.map(location => {
          const cap = location.name === "cache" && data.policy.cacheMaxBytes > 0 ? data.policy.cacheMaxBytes : null;
          return <div key={location.name} className="storage-location">
            <dt><code>{location.name}/</code></dt>
            <dd className="storage-size">{storageBytes(location.bytes)}</dd>
            <dd className="storage-reclaimable">{storageBytes(location.reclaimable)} reclaimable</dd>
            {cap !== null && <dd className="storage-capacity">
              <div className="storage-usage" role="meter" aria-label={`${location.name} usage`}
                aria-valuemin={0} aria-valuemax={cap} aria-valuenow={Math.min(cap, Math.max(0, location.bytes))}
                aria-valuetext={`${storageBytes(location.bytes)} of ${storageBytes(cap)} cap`}>
                <span style={{width: `${Math.min(100, Math.max(0, location.bytes / cap * 100))}%`}} />
              </div>
            </dd>}
            {Object.entries(location.breakdown || {}).map(([name, bytes]) => (
              <dd key={name} className="panel-caption storage-breakdown">{name}: {storageBytes(bytes)}</dd>
            ))}
          </div>;
        })}
      </dl>
      <p className="storage-last-prune">Last prune: {data.lastRun ? `${data.lastRun.startedAt} · ${data.lastRun.endedAt ? data.lastRun.failures.length ? "completed with failures" : "completed" : "running or interrupted"}` : "Never run"}</p>
      {data.lastRun?.removed.map(location => <p className="panel-caption" key={location.location}>{location.location}: {storageItems(location.items)} removed · {storageBytes(location.bytes)} freed{storageRemovalDetails(location)}</p>)}
      {data.lastRun?.failures.map((failure, i) => <p role="alert" key={i}>{failure}</p>)}
      {data.staleWorktrees.length > 0 && <Notice kind="warn">Stale worktrees (kept): {data.staleWorktrees.join(", ")}</Notice>}
      {data.failures.map((failure, i) => <p role="alert" key={i}>{failure}</p>)}
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
