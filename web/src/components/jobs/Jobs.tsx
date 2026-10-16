import { createContext, useContext, useEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { Badge } from "../ui/Badge";
import { Button } from "../ui/Button";
import { dashboardJobsSource, readJobLogs } from "./source";
import type { JobsSource } from "./source";
import { Routing } from "./Routing";
import { UsageCard } from "./UsageCard";
import { JOB_COLUMNS, jobColumn, mergeJobChains, type JobChain } from "./board";
import { JobDrawer } from "./JobDrawer";
import "./jobs.css";

const SourceContext = createContext<JobsSource>(dashboardJobsSource);

export interface Job {
  id: string;
  title: string;
  harness: string;
  modelRequested: string;
  modelActual?: string;
  repo: string;
  cwd: string;
  access: string;
  configMode: string;
  status: string;
  duration: string;
  retryAttempt?: number;
  retryLimit?: number;
  retryCount?: number;
  failureClass?: string;
  quotaResetAt?: string;
  createdAt: string;
  usagePartial?: boolean;
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
  fallbackFrom?: string;
  fallbackTo?: string;
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
  j.status === "running" || j.status === "retrying" ? 2 : j.status === "queued" ? 1 : 0;
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
    <>
    <Badge
      color={job.status === "failed" ? "danger" : job.status === "queued" ? "warn" : active(job) || job.status === "succeeded" ? "success" : "text-2"}
      className="panel-status"
    >
      {job.status === "running" && job.retryAttempt ? `retrying (${job.retryAttempt}/${job.retryLimit})` : labels[job.status] || job.status}
    </Badge>
    {!active(job) && !!job.retryCount && <Badge color="text-2">retries: {job.retryCount}</Badge>}
    {(job.status === "failed" || job.status === "canceled") && job.failureClass && <Badge color="warn" className="panel-failure-class">{job.failureClass}</Badge>}
    </>
  );
}
function FallbackLinks({ job }: { job: Job }) {
  if (!job.fallbackFrom && !job.fallbackTo) return null;
  return <nav className="panel-chain" aria-label="Fallback chain">
    {job.fallbackFrom && <a href={jobLink(job.fallbackFrom)}>fallback from {job.fallbackFrom}</a>}
    {job.fallbackTo && <a href={jobLink(job.fallbackTo)}>→ fallback to {job.fallbackTo}</a>}
  </nav>;
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
function Metadata({ job, showAccess = false }: { job: Job; showAccess?: boolean }) {
  return (
    <div className="panel-metadata">
      <span>{job.harness}</span>
      <span>{job.modelActual || job.modelRequested || "Default model"}</span>
      {showAccess && <span>{job.access || "unknown access"}</span>}
      <span>{job.duration}</span>
      <Usage job={job} />
    </div>
  );
}
export function JobCard({ chain, selected }: { chain: JobChain; selected?: string }) {
  const job = chain.latest;
  const color = JOB_COLUMNS.find(column => column.key === jobColumn(job))!.color;
  const fallbackHarnesses = [...new Set(chain.runs.filter(run => run.fallbackFrom || chain.runs.some(parent => parent.fallbackTo === run.id)).map(run => run.harness))];
  const failures = [...new Set(chain.runs.map(run => run.failureClass).filter(Boolean))];
  return (
    <article className={`task-card panel-job ${active(job) ? "glow" : ""} ${chain.runs.some(run => run.id === selected) ? "selected" : ""}`} style={{"--st": color} as CSSProperties}>
      <div className="panel-job-top"><Status job={job} /><span className="panel-mono">Run {job.id}</span></div>
      <h2><a href={jobLink(job.id)}>{chain.root.title || chain.root.id}</a></h2>
      <Metadata job={job} />
      <div className="panel-tags">
        {chain.runs.length > 1 && <Badge color="text-2">{chain.runs.length} runs</Badge>}
        {chain.rework > 0 && <Badge color="text-2">rework {chain.rework}</Badge>}
        {fallbackHarnesses.map(harness => <Badge key={harness} color="warn">fallback → {harness}</Badge>)}
        {failures.filter(failure => failure !== job.failureClass || !["failed", "canceled"].includes(job.status)).map(failure => <Badge key={failure} color="warn">{failure}</Badge>)}
        {job.worktree && <Badge color="text-2">worktree · {job.worktree.status}</Badge>}
        {job.access === "read-only" && <Badge color="text-2">read-only</Badge>}
      </div>
      <p className="panel-repository" title={job.repo}>{job.repo || "No repository recorded"}</p>
      {active(job) > 0 && <p className="panel-activity"><span className="panel-live-dot" />{job.activity?.summary || "Waiting for activity…"}</p>}
      <Attention job={job} />
    </article>
  );
}

export function JobList({ jobs, toolbar, showBoard = true, selected, loaded = true, children }: {
  jobs: Job[]; toolbar?: ReactNode; showBoard?: boolean; selected?: string; loaded?: boolean; children?: ReactNode;
}) {
  const [filters, setFilters] = useState({harness: "", repo: ""});
  // Merge before filtering so a harness switch never splits a run chain.
  const chains = mergeJobChains(jobs).filter(chain => chain.runs.some(job =>
    (!filters.harness || job.harness === filters.harness) && (!filters.repo || job.repo === filters.repo)));
  return <>
    <div className="jobs-toolbar flex items-center gap-[14px] border-b border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] px-[18px] py-[11px]">
      {([{key: "repo", label: "Repository", all: "All repositories"}, {key: "harness", label: "Harness", all: "All harnesses"}] as const).map(({key, label, all}) =>
        <label key={key} className={`jobs-filter jobs-filter-${key}`}><span className="sr-only">{label}</span>
          <select aria-label={label} value={filters[key]} onChange={e => setFilters({...filters, [key]: e.target.value})}>
            <option value="">{all}</option>
            {[...new Set(jobs.map(job => job[key]))].filter(Boolean).sort().map(value => <option key={value}>{value}</option>)}
          </select>
        </label>)}
      {toolbar}
    </div>
    {showBoard && <div role={toolbar ? "tabpanel" : undefined} id={toolbar ? "jobs-tab-jobs" : undefined} aria-labelledby={toolbar ? "jobs-tab-button-jobs" : undefined}>
    {loaded && <div className="jobs-board">
      {JOB_COLUMNS.map(column => {
        const rows = chains.filter(chain => jobColumn(chain.latest) === column.key);
        return <section key={column.key} aria-label={column.label} className="jobs-column">
          <div className="flex items-center gap-[8px] px-1 pb-3 pt-0.5">
            <span className={`h-[7px] w-[7px] rounded-full ${column.key === "running" ? "shell-breath" : ""}`} style={{background: column.color}} />
            <h2 className="text-[11px] font-semibold uppercase tracking-[.5px] text-[var(--color-text-2)]">{column.label}</h2>
            <span className="mono ml-auto rounded-full bg-white/5 px-[7px] py-px text-[10px] text-[var(--color-text-3)]" aria-label={`${column.label} count`}>{rows.length}</span>
          </div>
          <div className="flex flex-col gap-[11px]">
            {rows.length ? rows.map(chain => <JobCard key={chain.root.id} chain={chain} selected={selected} />) : <div className="kb-ghost">{column.empty}</div>}
          </div>
        </section>;
      })}
    </div>}
    {children}
    </div>}
  </>;
}
export function LogView({
  lines,
  status,
}: {
  lines: string[];
  status: string;
}) {
  const log = useRef<HTMLPreElement>(null);
  useEffect(() => {
    if (log.current) log.current.scrollTop = log.current.scrollHeight;
  }, [lines]);
  return (
    <section className="panel-section">
      <div className="panel-section-heading">
        <h2>Live logs</h2>
        <span>{status}</span>
      </div>
      <pre
        ref={log}
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
async function getJSON<T>(
  source: JobsSource,
  path: string,
  signal: AbortSignal,
): Promise<T> {
  const response = await source.fetch(path, signal);
  return response.json() as Promise<T>;
}
function CopyCommand({ command }: { command: string }) {
  const [message, setMessage] = useState("Copy");
  return (
    <div className="panel-command">
      <code>{command}</code>
      <Button
        variant="ghost"
        size="sm"
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
      </Button>
    </div>
  );
}
// Job ids from disk may contain shell syntax. Quote every command argument.
function shellArg(value: string) {
  return "'" + value.replaceAll("'", "'\\''") + "'";
}
function JobDetail({ id, jobs }: { id: string; jobs: Job[] }) {
  const source = useContext(SourceContext);
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
            source,
            `${source.jobsPath}/${encodeURIComponent(id)}`,
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
  }, [id, source]);
  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    async function update() {
      try {
        setDiff(
          await getJSON<Diff>(
            source,
            `${source.jobsPath}/${encodeURIComponent(id)}/diff`,
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
  }, [id, source]);
  useEffect(() => {
    const controller = new AbortController();
    void readJobLogs(
      source.fetch,
      `${source.jobsPath}/${encodeURIComponent(id)}/logs`,
      controller.signal,
      (line) => setLines((current) => [...current, line].slice(-1000)),
      setLogStatus,
    ).catch((error: Error) => {
      if (!controller.signal.aborted) setLogStatus(error.message);
    });
    return () => controller.abort();
  }, [id, source]);
  return (
    <>
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
                {detail.job.id}
              </div>
              <h1>{detail.job.title || id}</h1>
            </div>
            <Status job={detail.job} />
          </div>
          <Metadata job={detail.job} showAccess />
          <section className="panel-section">
            <h2>Run timeline</h2>
            <nav className="panel-timeline" aria-label="Run timeline">
              {(mergeJobChains(jobs.some(job => job.id === id) ? jobs : [...jobs, detail.job]).find(chain => chain.runs.some(job => job.id === id))?.runs || [detail.job]).map(run =>
                <a key={run.id} href={jobLink(run.id)} aria-current={run.id === id ? "step" : undefined}>
                  {run.id} · {run.status} · {run.harness} · {run.duration}
                </a>)}
            </nav>
            <FallbackLinks job={detail.job} />
          </section>
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
                <p className="panel-error" role="alert">{detail.resultError}</p>
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
            {diffError && <p className="panel-error" role="alert">{diffError}</p>}
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
export function Jobs({ source = dashboardJobsSource, standalone = false }: {
  source?: JobsSource;
  standalone?: boolean;
}) {
  return (
    <SourceContext.Provider value={source}>
      <JobsContent standalone={standalone} />
    </SourceContext.Provider>
  );
}
function JobsContent({ standalone }: { standalone: boolean }) {
  const source = useContext(SourceContext);
  const [id, setId] = useState(selectedJob);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [error, setError] = useState("");
  const [updated, setUpdated] = useState("");
  const [loaded, setLoaded] = useState(false);
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
        setJobs(await getJSON<Job[]>(source, `${source.jobsPath}?all=1`, controller.signal));
        setLoaded(true);
        setError("");
        setUpdated(new Date().toLocaleTimeString("en-US", {hour: "numeric", minute: "2-digit"}));
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
  }, [source]);
  const [tab, setTab] = useState<"jobs" | "routing">("jobs");
  // Any detail deep link selects Jobs, including hash changes from Routing.
  useEffect(() => { if (id) setTab("jobs"); }, [id]);
  return (
    <div className="jobs-view" data-testid="view-jobs">
      <div className="panel-shell" inert={!!id} aria-hidden={id ? true : undefined}>
        {standalone && <StandaloneHeader />}
        <main>
          <h1 className="sr-only">Delegated jobs</h1>
          <JobList jobs={jobs} selected={id} loaded={loaded} showBoard={tab === "jobs"} toolbar={<>
            <div role="tablist" aria-label="Jobs views" className="flex items-center gap-1 rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-page)] p-0.5">
              {(["jobs", "routing"] as const).filter(value => value === "jobs" || source.routerPath).map(value =>
                <button key={value} type="button" role="tab" aria-selected={tab === value} aria-controls={`jobs-tab-${value}`} id={`jobs-tab-button-${value}`} tabIndex={tab === value ? 0 : -1}
                  onClick={() => setTab(value)} onKeyDown={event => {
                    if (["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key) && source.routerPath) {
                      event.preventDefault();
                      const next = event.key === "Home" ? "jobs" : event.key === "End" ? "routing" : tab === "jobs" ? "routing" : "jobs";
                      setTab(next); document.getElementById(`jobs-tab-button-${next}`)?.focus();
                    }
                  }} className="rounded-md px-2.5 py-1 text-[11px] font-medium transition-colors"
                  style={{color: tab === value ? "var(--color-text-1)" : "var(--color-text-3)", background: tab === value ? "rgba(255,255,255,0.09)" : "transparent"}}>
                  {value === "jobs" ? "Jobs" : "Routing"}
                </button>)}
            </div>
            <div className="jobs-toolbar-status">
              <span className="panel-updated">{updated ? `Updated ${updated}` : "Connecting…"}</span>
              <span className="jobs-readonly" title="Read-only: apply / discard / cancel must be run in the CLI or Claude Code.">Read-only</span>
            </div>
          </>}>
            {error && <p className="panel-error" role="alert">{error}</p>}
            {!loaded && !error && <p className="panel-empty">Loading jobs…</p>}
            {loaded && <div className="jobs-usage"><UsageCard source={source} /></div>}
          </JobList>
          {tab === "routing" && source.routerPath && <div role="tabpanel" id="jobs-tab-routing" aria-labelledby="jobs-tab-button-routing">
            <Routing source={source} readOnly={standalone} expanded />
          </div>}
        </main>
        {standalone && <footer className="panel-footer">
          PACTIFY / LOCAL HARNESS JOBS<span>Job data stays on this device</span>
        </footer>}
      </div>
      {id && <JobDrawer id={id} jobs={jobs}><JobDetail key={id} id={id} jobs={jobs} /></JobDrawer>}
    </div>
  );
}

function StandaloneHeader() {
  const [theme, setTheme] = useState(
    () => localStorage.getItem("pactify-jobs-theme") || "system",
  );
  useEffect(() => {
    const previous = document.documentElement.dataset.theme;
    document.documentElement.dataset.theme = theme;
    localStorage.setItem("pactify-jobs-theme", theme);
    return () => {
      if (previous === undefined) delete document.documentElement.dataset.theme;
      else document.documentElement.dataset.theme = previous;
    };
  }, [theme]);
  return (
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
  );
}
