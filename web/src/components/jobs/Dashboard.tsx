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
import { Quota, type QuotaData } from "./Quota";
import { JobCard } from "./Jobs";
import { mergeJobChains } from "./board";
import { sectionFor, projectName } from "./statistics";
import { StatusPill } from "../ui/Design";
import { relativeTime } from "./pipeline";
import { LiveTree } from "./LivePanel";
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
  quotaSnapshots: QuotaData["quotaSnapshots"];
}
export function Dashboard({
  source,
  jobs,
  jobsUnavailable,
  jobsError,
  readOnly = false,
  onChanged = () => {},
}: {
  source: JobsSource;
  jobs: Job[] | null;
  jobsUnavailable: boolean;
  jobsError?: string;
  readOnly?: boolean;
  onChanged?: () => void;
}) {
  const harnesses = useResource<{harnesses:{name:string;installed:boolean;version:string}[]}>(source,source.harnessesPath || source.jobsPath.replace(/\/jobs$/, "/harnesses"));
  const chains = mergeJobChains(jobs || []);
  const [notice, setNotice] = useState("");
  useEffect(() => {
    if (!notice) return;
    const timer = setTimeout(() => setNotice(""), 5000);
    return () => clearTimeout(timer);
  }, [notice]);
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
      {notice && <div className="v2-toast" role="status">{notice}</div>}
      {(jobsUnavailable || unavailable || !!harnesses.error) && (
        <p role="alert" className="panel-error">
          {jobsError || cost.error || harnesses.error || "Usage could not be loaded. Retrying shortly."}
        </p>
      )}
      <div className="kpi-overview">
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
              {jobsUnavailable ? "—" : !jobs ? "…" : kpis[key]}
            </strong>
          </section>
        ))}
        </div>
        <svg className="dot-matrix" width="216" height="72" viewBox="0 0 216 72" aria-hidden="true">
          {Array.from({ length: 24 * 8 }, (_, i) => (
            <circle key={i} cx={(i % 24) * 9 + 4.5} cy={Math.floor(i / 24) * 9 + 4.5}
              r={i % 24 < 8 ? .9 : i % 24 < 17 ? 1.6 : 2.4}
              fill="currentColor" opacity={i % 24 < 8 ? .35 : i % 24 < 17 ? .6 : 1} />
          ))}
        </svg>
      </div>
      <div className="dashboard-columns">
      <div className="dashboard-primary">
        {([["progress", "Running now"], ["attention", "Needs attention"]] as const).map(([key, title]) => {
          const rows = chains.filter(chain => sectionFor(chain.latest) === key);
          return (
            <section key={key} className="dashboard-jobs">
              <div className="section-heading">
                <h2>{title} <span className="count-pill">{rows.length}</span></h2>
                <a href={`#/jobs?status=${key}`}>All jobs →</a>
              </div>
              <div className="dashboard-job-grid">
                {rows.map(chain => <JobCard key={chain.root.id} chain={chain} source={source} readOnly={readOnly} onChanged={onChanged} notify={setNotice} />)}
                {!rows.length && <p className="panel-caption">
                  {jobsUnavailable ? "—" : key === "progress" ? "All lines idle" : "Nothing waiting for you"}
                </p>}
              </div>
            </section>
          );
        })}
      {jobsUnavailable ? (
        <section className="panel-section"><h2>Live</h2><p>—</p></section>
      ) : <LiveTree source={source} jobs={jobs} />}
      <section className="panel-section token-chart">
        <h2>Token usage · last 14 days</h2>
        {jobsUnavailable ? (
          <p>—</p>
        ) : !jobs ? (
          <p>Loading usage…</p>
        ) : (
          <TokenChart days={days} />
        )}
      </section>
      <section className="panel-section">
        <h2>By harness</h2>
        {unavailable ? (
          <p>—</p>
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
                            ? "—"
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
      </div>
      <aside className="dashboard-secondary">
        <section className="panel-section dashboard-harnesses">
          <div className="section-heading"><h2>Harnesses</h2><a href="#/settings">Manage →</a></div>
          {harnesses.error ? <p className="panel-caption">—</p>
            : !harnesses.data ? <p>Loading harnesses…</p>
            : (harnesses.data.harnesses || []).map(harness => {
              const running = chains.some(chain => chain.latest.harness === harness.name && sectionFor(chain.latest) === "progress");
              return (
                <div className="harness-summary" key={harness.name}>
                  <div><strong>{harness.name}</strong><p className="panel-caption">{harness.version || "Version unknown"}</p></div>
                  <StatusPill status={!harness.installed ? "not installed" : running ? "running" : "ready"} running={running} />
                </div>
              );
            })}
        </section>
      <section className="panel-section">
        <h2>Quota</h2>
        {unavailable ? (
          <p>—</p>
        ) : !report ? (
          <p>Loading quota…</p>
        ) : (
          <Quota report={report} now={now} />
        )}
      </section>
      <section className="panel-section recent-activity">
        <h2>Recent activity</h2>
        {chains.slice(0, 6).map(({ latest }) => (
          <a href={`#/jobs/${encodeURIComponent(latest.id)}`} key={latest.id}>
            <StatusPill status={latest.status} running={sectionFor(latest) === "progress"} />
            <strong>{latest.title || latest.id}</strong>
            <small>{projectName(latest.repo)} · {relativeTime(latest.finishedAt || latest.createdAt, now)} ago</small>
          </a>
        ))}
      </section>
      </aside>
      </div>
    </div>
  );
}
