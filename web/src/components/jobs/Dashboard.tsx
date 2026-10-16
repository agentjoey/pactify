import { useEffect, useState } from "react";
import type { Job } from "./types";
import type { JobsSource } from "./source";
import { useResource } from "./data";
import {
  dailyTokens,
  dashboardKpis,
  durationSeconds,
  formatDuration,
  formatCost,
} from "./statistics";
import { TokenChart } from "./TokenChart";
import { Quota } from "./Quota";
interface Total {
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
interface Cost {
  groups: (Total & { key: string })[];
  total: Total;
  exhaustedRoutes: {
    key: string;
    harness: string;
    model?: string;
    resetAt: string | null;
  }[];
  quotaSnapshots: {
    harness: string;
    jobId: string;
    observedAt: string;
    rateLimit: string;
  }[];
}
export function Dashboard({
  source,
  jobs,
  jobsUnavailable,
}: {
  source: JobsSource;
  jobs: Job[] | null;
  jobsUnavailable: boolean;
}) {
  const cost = useResource<Cost>(
    source,
    `${source.costPath}?since=7d&by=harness`,
  );
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  const kpis = dashboardKpis(jobs || [], new Date(now));
  const days = dailyTokens(jobs || [], new Date(now));
  const report =
    !cost.error &&
    cost.data &&
    Array.isArray(cost.data.groups) &&
    Array.isArray(cost.data.exhaustedRoutes)
      ? cost.data
      : null;
  const unavailable = !!cost.error || (!!cost.data && !report);
  return (
    <div className="dashboard-content">
      <div className="kpi-grid">
        {(
          [
            ["progress", "In progress"],
            ["queued", "Queued"],
            ["attention", "Needs attention"],
            ["succeeded", "Succeeded (7d)"],
            ["failed", "Failed (7d)"],
          ] as const
        ).map(([key, label]) => (
          <section className="panel-section kpi" aria-label={label} key={key}>
            <h2>{label}</h2>
            <strong>
              {jobsUnavailable ? "Unavailable" : !jobs ? "…" : kpis[key]}
            </strong>
          </section>
        ))}
      </div>
      <section className="panel-section token-chart">
        <h2>Token usage · last 14 days</h2>
        {jobsUnavailable ? (
          <p>Unavailable</p>
        ) : !jobs ? (
          <p>Loading usage…</p>
        ) : (
          <TokenChart days={days} />
        )}
      </section>
      <section className="panel-section">
        <h2>By harness</h2>
        {unavailable ? (
          <p>Unavailable</p>
        ) : !report ? (
          <p>Loading usage…</p>
        ) : (
          <>
            <p id="harness-scroll-hint" className="harness-scroll-hint">
              Scroll horizontally to see tokens and cost →
            </p>
            <div
              className="table-scroll"
              aria-describedby="harness-scroll-hint"
              role="region"
              aria-label="By harness usage"
              tabIndex={0}
            >
              <table>
                <thead>
                  <tr>
                    {[
                      "Harness",
                      "Jobs",
                      "Success rate",
                      "Avg duration",
                      "Input tokens",
                      "Output tokens",
                      "Cost",
                    ].map((h) => (
                      <th key={h} scope="col">
                        {h}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {report.groups.map((group) => {
                    const durations = (jobs || [])
                      .filter(
                        (job) =>
                          job.harness === group.key &&
                          !["queued", "running", "retrying"].includes(
                            job.status,
                          ) &&
                          Date.parse(job.finishedAt || job.createdAt) >=
                            new Date(
                              new Date(now).setHours(0, 0, 0, 0),
                            ).getTime() -
                              6 * 86400000,
                      )
                      .map((j) => durationSeconds(j.duration));
                    return (
                      <tr key={group.key}>
                        <th scope="row">{group.key || "unknown"}</th>
                        <td>{group.jobs}</td>
                        <td>
                          {group.jobs
                            ? `${Math.round((group.succeeded / group.jobs) * 100)}%`
                            : "—"}
                        </td>
                        <td>
                          {jobsUnavailable
                            ? "Unavailable"
                            : durations.length
                              ? formatDuration(
                                  durations.reduce((a, b) => a + b, 0) /
                                    durations.length,
                                )
                              : "—"}
                        </td>
                        <td>{group.inputTokens.toLocaleString("en-US")}</td>
                        <td>{group.outputTokens.toLocaleString("en-US")}</td>
                        <td>
                          {[
                            group.costUSD != null
                              ? formatCost(group.costUSD)
                              : "",
                            group.subscriptionJobs ? "subscription" : "",
                            group.unpricedJobs ? "unknown" : "",
                          ]
                            .filter(Boolean)
                            .join(" + ") || "—"}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
            {!!report.total?.usagePartialJobs && (
              <p className="panel-caption">
                Includes {report.total.usagePartialJobs} jobs with partial usage
              </p>
            )}
          </>
        )}
      </section>
      <section className="panel-section">
        <h2>Quota</h2>
        {unavailable ? (
          <p>Unavailable</p>
        ) : !report ? (
          <p>Loading quota…</p>
        ) : (
          <Quota report={report} now={now} />
        )}
      </section>
    </div>
  );
}
