import { panelFetch, streamLogs } from "./auth";
import { useEffect, useState } from "react";

export interface Job {
  id: string;
  title: string;
  issue: string;
  harness: string;
  modelRequested: string;
  modelActual?: string;
  repo: string;
  cwd: string;
  access: string;
  configMode: string;
  status: string;
  duration: string;
  createdAt: string;
  inputTokens: number;
  outputTokens: number;
  attention: { kind: string; message: string }[];
  activity?: {
    summary: string;
    inputTokens?: number;
    outputTokens?: number;
    usagePartial?: boolean;
  };
  worktree?: { path: string; baseCommit: string; status: string };
  resumeOf: string;
  resumedBy: string[];
}
interface Detail {
  job: Job;
  brief: string;
  markdown: string;
  resultError?: string;
  result?: { finalText: string; warnings?: string[] };
}
interface Diff {
  patch: string;
  files: { Path: string; Added: number; Deleted: number }[];
  truncated?: boolean;
  note: string;
}
interface Filters {
  status: string;
  harness: string;
  repo: string;
}
const labels: Record<string, string> = {
  queued: "queued",
  running: "running",
  succeeded: "succeeded",
  failed: "failed",
  canceled: "canceled",
};
const active = (j: Job) =>
  j.status === "running" ? 2 : j.status === "queued" ? 1 : 0;
const jobLink = (id: string) => `#/jobs/${encodeURIComponent(id)}`;

export function filterJobs(jobs: Job[], filters: Filters): Job[] {
  return jobs
    .filter(
      (j) =>
        (!filters.status || j.status === filters.status) &&
        (!filters.harness || j.harness === filters.harness) &&
        (!filters.repo || j.repo === filters.repo),
    )
    .sort(
      (a, b) =>
        active(b) - active(a) ||
        b.createdAt.localeCompare(a.createdAt) ||
        b.id.localeCompare(a.id),
    );
}
function Status({ job }: { job: Job }) {
  return (
    <span className={`panel-status panel-status-${job.status}`}>
      <i />
      {labels[job.status] || job.status}
    </span>
  );
}
function Attention({ job }: { job: Job }) {
  return job.attention.length > 0 ? (
    <div className="panel-attention" aria-label="Needs attention">
      <strong>Needs attention</strong>
      <span>{job.attention.map((a) => a.message).join(" · ")}</span>
    </div>
  ) : null;
}
function Usage({ job }: { job: Job }) {
  const partial =
    job.status === "running" && job.activity?.inputTokens !== undefined;
  return (
    <span>
      {partial ? job.activity?.inputTokens : job.inputTokens} in /{" "}
      {partial ? (job.activity?.outputTokens ?? 0) : job.outputTokens} out
      {partial && job.activity?.usagePartial ? " · partial" : ""}
    </span>
  );
}
function Metadata({ job }: { job: Job }) {
  return (
    <div className="panel-metadata">
      <span>{job.harness}</span>
      <span>{job.modelActual || job.modelRequested || "Default model"}</span>
      <span>{job.duration}</span>
      <Usage job={job} />
    </div>
  );
}
export function JobList({ jobs }: { jobs: Job[] }) {
  const [filters, setFilters] = useState<Filters>({
    status: "",
    harness: "",
    repo: "",
  });
  const rows = filterJobs(jobs, filters);
  return (
    <>
      <div className="panel-filters">
        <label>
          Status
          <select
            aria-label="Status"
            value={filters.status}
            onChange={(e) => setFilters({ ...filters, status: e.target.value })}
          >
            <option value="">All statuses</option>
            {Object.entries(labels).map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </select>
        </label>
        <label>
          Harness
          <select
            aria-label="Harness"
            value={filters.harness}
            onChange={(e) =>
              setFilters({ ...filters, harness: e.target.value })
            }
          >
            <option value="">All harnesses</option>
            {[...new Set(jobs.map((j) => j.harness))].sort().map((h) => (
              <option key={h}>{h}</option>
            ))}
          </select>
        </label>
        <label className="panel-repo-filter">
          Repository
          <select
            aria-label="Repository"
            value={filters.repo}
            onChange={(e) => setFilters({ ...filters, repo: e.target.value })}
          >
            <option value="">All repositories</option>
            {[...new Set(jobs.map((j) => j.repo))]
              .filter(Boolean)
              .sort()
              .map((repo) => (
                <option key={repo}>{repo}</option>
              ))}
          </select>
        </label>
        <span className="panel-count">Jobs: {rows.length}</span>
      </div>
      <div className="panel-job-grid">
        {rows.map((job) => (
          <article
            className={`panel-job ${active(job) ? "panel-job-active" : ""}`}
            key={job.id}
          >
            <div className="panel-job-top">
              <Status job={job} />
              <span className="panel-mono">{job.issue || job.id}</span>
            </div>
            <h2>
              <a href={jobLink(job.id)}>{job.title || job.issue || job.id}</a>
            </h2>
            <Metadata job={job} />
            <div className="panel-repository" title={job.repo}>
              {job.repo || "No repository recorded"}
            </div>
            <div className="panel-mode">
              {job.worktree ? `worktree · ${job.worktree.status}` : "workspace"}{" "}
              · {job.access || "unknown access"} · {job.configMode || "unknown"}{" "}
              config
            </div>
            {job.activity && (
              <p className="panel-activity">
                <span className="panel-live-dot" />
                {job.activity.summary || "Waiting for activity…"}
              </p>
            )}
            <Attention job={job} />
          </article>
        ))}
      </div>
      {rows.length === 0 && (
        <div className="panel-empty">No jobs match these filters.</div>
      )}
    </>
  );
}
export function LogView({
  lines,
  status,
}: {
  lines: string[];
  status: string;
}) {
  return (
    <section className="panel-section">
      <div className="panel-section-heading">
        <h2>Live logs</h2>
        <span>{status}</span>
      </div>
      <pre
        className="panel-log"
        role="log"
        aria-label="Readable logs"
        aria-live="off"
      >
        {lines.length ? lines.join("\n") : "Waiting for logs…"}
      </pre>
      <p className="panel-caption">
        Starts within the last 1 MiB (up to 100 lines) and keeps up to 1,000 lines. Use the CLI
        for the full log.
      </p>
    </section>
  );
}
export function splitDiff(patch: string): { name: string; patch: string }[] {
  return patch
    .split(/(?=^diff --git )/m)
    .filter(Boolean)
    .map((part) => ({
      name: part.split("\n")[0].replace(/^diff --git /, ""),
      patch: part,
    }));
}
async function getJSON<T>(path: string, signal: AbortSignal): Promise<T> {
  const response = await panelFetch(path, signal);
  return response.json() as Promise<T>;
}
function CopyCommand({ command }: { command: string }) {
  const [message, setMessage] = useState("Copy");
  return (
    <div className="panel-command">
      <code>{command}</code>
      <button
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(command);
            setMessage("Copied");
          } catch {
            setMessage("Select text to copy");
          }
        }}
      >
        {message}
      </button>
    </div>
  );
}
// Job ids from disk may contain shell syntax. Quote every command argument.
function shellArg(value: string) {
  return "'" + value.replaceAll("'", "'\\''") + "'";
}
function JobDetail({ id }: { id: string }) {
  const [detail, setDetail] = useState<Detail | null>(null);
  const [diff, setDiff] = useState<Diff | null>(null);
  const [error, setError] = useState("");
  const [diffError, setDiffError] = useState("");
  const [lines, setLines] = useState<string[]>([]);
  const [logStatus, setLogStatus] = useState("Connecting");
  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    async function update() {
      try {
        setDetail(
          await getJSON<Detail>(
            `/api/jobs/${encodeURIComponent(id)}`,
            controller.signal,
          ),
        );
        setError("");
      } catch (e) {
        if (!controller.signal.aborted) setError(String((e as Error).message));
      } finally {
        if (!controller.signal.aborted) timer = setTimeout(update, 2000);
      }
    }
    void update();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [id]);
  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    async function update() {
      try {
        setDiff(
          await getJSON<Diff>(
            `/api/jobs/${encodeURIComponent(id)}/diff`,
            controller.signal,
          ),
        );
        setDiffError("");
      } catch (e) {
        if (!controller.signal.aborted) setDiffError((e as Error).message);
      } finally {
        if (!controller.signal.aborted) timer = setTimeout(update, 5000);
      }
    }
    void update();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [id]);
  useEffect(() => {
    const controller = new AbortController();
    void streamLogs(
      `/api/jobs/${encodeURIComponent(id)}/logs`,
      controller.signal,
      (line) => setLines((current) => [...current, line].slice(-1000)),
      setLogStatus,
    ).catch((error: Error) => {
      if (!controller.signal.aborted) setLogStatus(error.message);
    });
    return () => controller.abort();
  }, [id]);
  return (
    <>
      <a className="panel-back" href="#/">
        ← All jobs
      </a>
      {error && (
        <p className="panel-error" role="alert">
          {error}
        </p>
      )}
      {!detail && !error && <p className="panel-empty">Loading job…</p>}
      {detail && (
        <>
          <div className="panel-detail-title">
            <div>
              <div className="panel-eyebrow">
                {detail.job.issue || detail.job.id}
              </div>
              <h1>{detail.job.title || id}</h1>
            </div>
            <Status job={detail.job} />
          </div>
          <Metadata job={detail.job} />
          <p className="panel-repository">
            {detail.job.repo || "No repository recorded"}
          </p>
          <p className="panel-mode">
            {detail.job.worktree
              ? `worktree · ${detail.job.worktree.status}`
              : "workspace"}{" "}
            · {detail.job.access} · {detail.job.configMode || "unknown"} config
          </p>
          {detail.job.worktree && (
            <p className="panel-caption">
              Worktree: {detail.job.worktree.path}
              <br />
              Base: {detail.job.worktree.baseCommit}
            </p>
          )}
          <Attention job={detail.job} />
          <div className="panel-detail-grid">
            <section className="panel-section">
              <h2>Task brief</h2>
              <pre>{detail.brief || "No brief recorded"}</pre>
            </section>
            <section className="panel-section">
              <h2>Result summary</h2>
              {detail.resultError && (
                <p className="panel-error">{detail.resultError}</p>
              )}
              <pre>
                {detail.markdown ||
                  (detail.job.status === "running" ||
                  detail.job.status === "queued"
                    ? "Job in progress. Results are not available yet."
                    : "No results available.")}
              </pre>
            </section>
          </div>
          <LogView lines={lines} status={logStatus} />
          <section className="panel-section">
            <div className="panel-section-heading">
              <h2>Diff</h2>
              <span>{diff?.truncated ? "Truncated" : "Read-only preview"}</span>
            </div>
            {diffError && <p className="panel-error">{diffError}</p>}
            {diff?.note && <p className="panel-caption">{diff.note}</p>}
            {diff?.patch ? (
              splitDiff(diff.patch).map((file, i) => (
                <details className="panel-diff" key={i}>
                  <summary>{file.name}</summary>
                  <pre>{file.patch}</pre>
                </details>
              ))
            ) : diff?.files?.length ? (
              diff.files.map((file) => (
                <details className="panel-diff" key={file.Path}>
                  <summary>
                    {file.Path}{" "}
                    <span>
                      +{file.Added} / −{file.Deleted}
                    </span>
                  </summary>
                  <p>This job recorded a change list without a full patch.</p>
                </details>
              ))
            ) : (
              <p className="panel-caption">
                {diff ? "No changes to display." : "Loading diff…"}
              </p>
            )}
          </section>
          <section className="panel-section">
            <h2>Resume chain</h2>
            <nav className="panel-chain" aria-label="Resume chain">
              {detail.job.resumeOf && (
                <a href={jobLink(detail.job.resumeOf)}>
                  ← {detail.job.resumeOf}
                </a>
              )}
              <span>{id} (current)</span>
              {detail.job.resumedBy.map((child) => (
                <a key={child} href={jobLink(child)}>
                  {child} →
                </a>
              ))}
            </nav>
            {!detail.job.resumeOf && detail.job.resumedBy.length === 0 && (
              <p className="panel-caption">No resume links recorded.</p>
            )}
          </section>
          <section className="panel-section">
            <h2>CLI commands</h2>
            <p className="panel-caption">
              Read-only: apply / discard / cancel must be run in the CLI or
              Claude Code. Use apply / discard for worktree jobs and cancel for
              running jobs.
            </p>
            {["apply", "discard", "cancel"].map((action) => (
              <CopyCommand
                key={action}
                command={`pactify sub ${action} ${shellArg(id)}`}
              />
            ))}
            <CopyCommand
              command={`pactify sub logs ${shellArg(id)} --follow`}
            />
          </section>
        </>
      )}
    </>
  );
}
function selectedJob() {
  const prefix = "#/jobs/";
  try {
    return location.hash.startsWith(prefix)
      ? decodeURIComponent(location.hash.slice(prefix.length))
      : "";
  } catch {
    return "";
  }
}
export function Panel() {
  const [id, setId] = useState(selectedJob);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [error, setError] = useState("");
  const [updated, setUpdated] = useState("");
  const [theme, setTheme] = useState(
    () => localStorage.getItem("pactify-panel-theme") || "system",
  );
  useEffect(() => {
    document.documentElement.dataset.panelTheme = theme;
    localStorage.setItem("pactify-panel-theme", theme);
  }, [theme]);
  useEffect(() => {
    const change = () => setId(selectedJob());
    window.addEventListener("hashchange", change);
    return () => window.removeEventListener("hashchange", change);
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    async function update() {
      try {
        setJobs(await getJSON<Job[]>("/api/jobs?all=1", controller.signal));
        setError("");
        setUpdated(new Date().toLocaleTimeString());
      } catch (e) {
        if (!controller.signal.aborted) setError((e as Error).message);
      } finally {
        if (!controller.signal.aborted) timer = setTimeout(update, 2000);
      }
    }
    void update();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, []);
  return (
    <div className="panel-shell">
      <header className="panel-header">
        <a href="#/" className="panel-brand">
          <span className="panel-brand-mark">p</span>pactify{" "}
          <span className="panel-brand-divider">/</span> <span>sub panel</span>
        </a>
        <label className="panel-theme">
          Theme
          <select
            aria-label="Theme"
            value={theme}
            onChange={(e) => setTheme(e.target.value)}
          >
            <option value="system">System</option>
            <option value="light">Light</option>
            <option value="dark">Dark</option>
          </select>
        </label>
      </header>
      <main>
        <div className="panel-readonly">
          <span className="panel-lock">◈</span>
          <span>
            Read-only: apply / discard / cancel must be run in the CLI or Claude
            Code.
          </span>
          <span className="panel-local">127.0.0.1 · LOCAL</span>
        </div>
        {id ? (
          <JobDetail key={id} id={id} />
        ) : (
          <>
            <div className="panel-page-heading">
              <div>
                <div className="panel-eyebrow">HARNESS WORKSPACE</div>
                <h1>Delegated jobs</h1>
                <p>Track job progress, results and changes.</p>
              </div>
              <span className="panel-updated">
                <span className="panel-live-dot" />
                {updated ? `Updated ${updated}` : "Connecting…"} · Refreshes
                every 2s
              </span>
            </div>
            <div className="panel-stats">
              <div>
                <span>All jobs</span>
                <strong>{jobs.length}</strong>
              </div>
              <div>
                <span>Running / queued</span>
                <strong>{jobs.filter(active).length}</strong>
              </div>
              <div>
                <span>Needs attention</span>
                <strong className="panel-stat-attention">
                  {jobs.filter((j) => j.attention.length > 0).length}
                </strong>
              </div>
            </div>
            {error && (
              <p className="panel-error" role="alert">
                {error}
              </p>
            )}
            <JobList jobs={jobs} />
          </>
        )}
      </main>
      <footer className="panel-footer">
        PACTIFY / LOCAL HARNESS JOBS<span>Job data stays on this device</span>
      </footer>
    </div>
  );
}
