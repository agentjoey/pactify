import { useEffect, useState } from "react";
import type { JobsSource } from "./source";

import { formatCost } from "./statistics";

interface Totals {
  jobs: number;
  succeeded: number;
  failed: number;
  inputTokens: number;
  outputTokens: number;
  costUSD: number | null;
  subscriptionJobs: number;
  unpricedJobs: number;
  usagePartialJobs: number;
}
interface CostReport {
  groups: (Totals & { key: string })[];
  total: Totals;
  exhaustedRoutes: { harness: string; model?: string; resetAt: string | null }[];
}
const number = new Intl.NumberFormat("en-US");

function cost(total: Totals) {
  const parts: string[] = [];
  if (total.costUSD != null) parts.push(formatCost(total.costUSD));
  if (total.subscriptionJobs > 0) parts.push("subscription");
  if (total.unpricedJobs > 0) parts.push("unknown");
  return parts.join(" + ") || "—";
}
function resetsIn(resetAt: string | null) {
  if (!resetAt) return "unknown";
  const minutes = Math.max(0, Math.floor((Date.parse(resetAt) - Date.now()) / 60000));
  if (!Number.isFinite(minutes)) return "unknown";
  const hours = Math.floor(minutes / 60);
  if (hours >= 24) return `${Math.floor(hours / 24)}d ${hours % 24}h`;
  if (hours > 0) return `${hours}h ${minutes % 60}m`;
  return `${minutes}m`;
}
function UsageRow({ label, total }: { label: string; total: Totals }) {
  return <tr>
    <th scope="row">{label}</th>
    <td>{number.format(total.jobs)} ({number.format(total.succeeded)} ok / {number.format(total.failed)} failed)</td>
    <td>{number.format(total.inputTokens)}</td>
    <td>{number.format(total.outputTokens)}</td>
    <td>{cost(total)}</td>
  </tr>;
}

export function UsageCard({ source }: { source: JobsSource }) {
  const [report, setReport] = useState<CostReport | null>(null);
  const [unavailable, setUnavailable] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    async function update() {
      try {
        const response = await source.fetch(`${source.costPath}?since=7d&by=harness`, controller.signal);
        const next: CostReport = await response.json();
        if (!Array.isArray(next.groups) || !next.total || !Array.isArray(next.exhaustedRoutes)) {
          throw new Error("Invalid usage report");
        }
        if (!controller.signal.aborted) {
          setReport(next);
          setUnavailable(false);
        }
      } catch {
        if (!controller.signal.aborted) {
          setReport(null);
          setUnavailable(true);
        }
      } finally {
        if (!controller.signal.aborted) timer = setTimeout(update, 30000);
      }
    }
    void update();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [source]);

  return <section className="panel-section panel-usage" aria-label="Usage · last 7 days">
    <h2>Usage · last 7 days</h2>
    {unavailable ? <p className="panel-caption">Usage unavailable</p> : report ? <>
      <div className="panel-usage-scroll" role="region" aria-label="Usage by harness" tabIndex={0}>
        <table>
          <thead><tr>
            <th scope="col">Harness</th><th scope="col">Jobs (ok/failed)</th>
            <th scope="col">Input tokens</th><th scope="col">Output tokens</th><th scope="col">Cost</th>
          </tr></thead>
          <tbody>{report.groups.map(group => <UsageRow key={group.key} label={group.key || "unknown"} total={group} />)}</tbody>
          <tfoot><UsageRow label="Total" total={report.total} /></tfoot>
        </table>
      </div>
      {report.total.usagePartialJobs > 0 && <p className="panel-caption">* includes {number.format(report.total.usagePartialJobs)} jobs with partial usage</p>}
      {report.exhaustedRoutes.map(route => <p className="panel-caption" key={`${route.harness}/${route.model || ""}`}>
        Exhausted: {route.harness}{route.model ? `/${route.model}` : ""} (resets in {resetsIn(route.resetAt)})
      </p>)}
    </> : <p className="panel-caption">Loading usage…</p>}
  </section>;
}
