import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { Badge } from "../ui/Badge";
import { Button } from "../ui/Button";
import type { Job } from "./types";
import type { JobsSource } from "./source";
import { dashboardJobsSource } from "./source";
import { mergeJobChains, type JobChain } from "./board";
import {
  completedDays,
  formatDuration,
  jobCost,
  projectName,
  sectionFor,
  shortNumber,
  tokens,
} from "./statistics";
import { jobsLink, useHash, useResource, validJobs } from "./data";
import { JobActions } from "./JobActions";
import { JobDrawer } from "./JobDrawer";
import { NextSteps, JobsOnboarding, LoadingJobs } from "./NextSteps";
import { JobDetail } from "./JobDetail";
import { JobsKeyboard } from "./JobsKeyboard";
import "./jobs.css";
import { Pipeline } from "./PipelineBoard";
import { type LineGrouping, relativeTime } from "./pipeline";
import { Segmented, StatusPill } from "../ui/Design";
import { storedChoice, saveChoice } from "../../theme";
export type { Job } from "./types";
export { LogView, splitDiff } from "./logs";
function matchesSearch(job: Job, search: string) {
  const query = search.toLowerCase();
  return [job.title, job.id, job.repo, job.modelRequested, job.modelActual]
    .some(value => value?.toLowerCase().includes(query));
}
interface Filters {
  status: string;
  harness: string;
  repo: string;
  search?: string;
}
// Retain the existing exact-status utility for API consumers; UI sections use sectionFor.
export function filterJobs(jobs: Job[], filters: Filters) {
  const priority = (j: Job) =>
    ["running", "retrying"].includes(j.status)
      ? 2
      : j.status === "queued"
        ? 1
        : 0;
  return jobs
    .filter(
      (j) =>
        (!filters.status || j.status === filters.status) &&
        (!filters.harness || j.harness === filters.harness) &&
        (!filters.repo || j.repo === filters.repo) &&
        (!filters.search ||
          matchesSearch(j, filters.search)),
    )
    .sort(
      (a, b) =>
        priority(b) - priority(a) ||
        b.createdAt.localeCompare(a.createdAt) ||
        b.id.localeCompare(a.id),
    );
}
export function JobMetadata({ job }: { job: Job }) {
  const [now, setNow] = useState(Date.now);
  const running = ["running", "retrying"].includes(job.status);
  useEffect(() => {
    if (!running) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [running]);
  const usage = tokens(job);
  return (
    <div className="panel-metadata">
      <span title={job.repo}>{projectName(job.repo)}</span>
      <span>
        {running && Number.isFinite(Date.parse(job.createdAt))
          ? formatDuration((now - Date.parse(job.createdAt)) / 1000)
          : job.duration}
      </span>
      <span
        title={`${usage.input.toLocaleString("en-US")} input / ${usage.output.toLocaleString("en-US")} output tokens`}
      >
        {shortNumber(usage.input)} in / {shortNumber(usage.output)} out
        {(job.usagePartial || job.activity?.usagePartial) && " · partial"}
      </span>
      <span>{jobCost(job)}</span>
    </div>
  );
}
function CompletedRow({ chain }: { chain: JobChain }) {
  const job = chain.latest;
  const title = chain.root.title || chain.root.id;
  const model = `${job.harness} · ${job.modelActual || job.modelRequested || "Harness default"}`;
  const usage = tokens(job);
  const usageTitle = `${usage.input.toLocaleString("en-US")} input / ${usage.output.toLocaleString("en-US")} output tokens`;
  const time = job.finishedAt || job.createdAt;
  return (
    <li>
      <span data-column="status" className="compact-status" title={job.status}><StatusPill status={job.status}/></span>
      <a data-job-link data-column="title" className="compact-title" title={title} href={jobsLink(job.id)}>{title}</a>
      <div className="compact-meta">
        <span data-column="harness" title={model}>{model}</span>
        <span data-column="project" title={job.repo}>{projectName(job.repo)}</span>
        <span data-column="duration" title={job.duration}>{job.duration}</span>
        <span data-column="tokens" title={usageTitle}>
          {shortNumber(usage.input)} in / {shortNumber(usage.output)} out
          {job.usagePartial && " · partial"}
        </span>
        <span data-column="cost" title={jobCost(job)}>{jobCost(job)}</span>
        <time data-column="time" dateTime={time} title={new Date(time).toLocaleString("en-US")}>
          {new Date(time).toLocaleTimeString("en-US", {hour:"2-digit",minute:"2-digit"})}
        </time>
      </div>
    </li>
  );
}
export function JobCard({
  chain,
  selected,
  preview = false,
  source = dashboardJobsSource,
  readOnly = false,
  onChanged = () => {},
  notify = () => {},
}: {
  chain: JobChain;
  selected?: string;
  preview?: boolean;
  source?: JobsSource;
  readOnly?: boolean;
  onChanged?: () => void;
  notify?: (text: string) => void;
}) {
  const job = chain.latest;
  const fallbacks = [
    ...new Set(
      chain.runs
        .filter(
          (run) =>
            run.fallbackFrom ||
            chain.runs.some((parent) => parent.fallbackTo === run.id),
        )
        .map((run) => run.harness),
    ),
  ];
  const failures = [
    ...new Set(chain.runs.map((run) => run.failureClass).filter(Boolean)),
  ];
  const pillStatus = ["running", "retrying", "queued"].includes(job.status)
    ? job.status === "running" && job.retryAttempt ? `retrying (${job.retryAttempt}/${job.retryLimit})` : job.status
    : job.failureClass === "quota" || job.attention.some(a => a.kind === "quota") ? "quota" : job.status === "failed" ? "failed"
    : job.attention.some(a => a.kind === "worktree_pending") ? "ready"
    : job.attention.some(a => a.kind === "warnings") ? "warnings" : job.status;
  const open = () => {
    location.hash = jobsLink(job.id);
  };
  return (
    <article
      inert={preview}
      data-section={sectionFor(job)}
      className={`task-card panel-job ${chain.runs.some((run) => run.id === selected) ? "selected" : ""}`}
      onClick={(e) => {
        if (
          !(e.target as HTMLElement).closest(
            'a,button,input,textarea,select,[role="dialog"]',
          )
        )
          open();
      }}
    >
      <div className="panel-job-top">
        <StatusPill running={["running","retrying"].includes(job.status)} status={pillStatus} />
        <span className="harness-label">
          {job.harness} ·{" "}
          {job.modelActual || job.modelRequested || "Harness default"}
        </span>
      </div>
      <p className="job-id">{job.id} · {job.status === "queued" ? `waiting ${relativeTime(job.createdAt)}` : job.duration}</p>
      <h3>
        <a data-job-link href={jobsLink(job.id)}>{chain.root.title || chain.root.id}</a>
      </h3>
      <JobMetadata job={job} />
      {sectionFor(job) === "attention" && job.changedFiles != null && <p className="change-meta">{job.harness} · {job.changedFiles} files {job.addedLines != null && job.deletedLines != null && <span>+{job.addedLines}/−{job.deletedLines}</span>}</p>}
      <div className="panel-tags">
        {!["running", "retrying", "queued"].includes(job.status) &&
          !!job.retryCount && (
            <Badge color="text-2">retries: {job.retryCount}</Badge>
          )}
        {chain.runs.length > 1 && (
          <Badge color="text-2">{chain.runs.length} runs</Badge>
        )}
        {chain.rework > 0 && (
          <Badge color="text-2">{chain.rework} {chain.rework === 1 ? "rework" : "reworks"}</Badge>
        )}
        {fallbacks.map((h) => (
          <Badge key={h} color="warn">
            fallback → {h}
          </Badge>
        ))}
        {failures.filter(f => f !== pillStatus).map((f) => (
          <Badge key={f} color="warn">
            {f === "harness" ? "Agent error" : f}
          </Badge>
        ))}
        {job.worktree && (
          <Badge color="text-2">worktree · {job.worktree.status}</Badge>
        )}
        {job.access === "read-only" && <Badge color="text-2">read-only</Badge>}
      </div>
      {["running", "retrying"].includes(job.status) && (
        <p className="panel-activity" title={job.activity?.summary}>
          {job.activity?.summary || "Waiting for activity…"}
        </p>
      )}
      {sectionFor(job) === "attention" && (
        <p className="panel-attention" aria-label="Needs attention">
          {job.attention.map((a) => a.message).join(" · ")}
        </p>
      )}
      {readOnly && sectionFor(job) === "attention" && <NextSteps job={job} compact />}
      <div className="card-actions">
        <JobActions
          job={job}
          source={source}
          readOnly={readOnly}
          onChanged={onChanged}
          notify={notify}
        />
        {sectionFor(job) === "attention" && (
          <Button
            size="sm"
            variant="ghost"
            title={readOnly ? "More in details" : undefined}
            onClick={() => {
              location.hash = jobsLink(job.id, "diff");
            }}
          >
            Review
          </Button>
        )}
      </div>
    </article>
  );
}
export function JobList({
  jobs,
  selected,
  loaded = true,
  source,
  readOnly,
  onChanged,
  notify,
  children,
}: {
  jobs: Job[];
  selected?: string;
  loaded?: boolean;
  source?: JobsSource;
  readOnly?: boolean;
  onChanged?: () => void;
  notify?: (text: string) => void;
  children?: ReactNode;
}) {
  const hash = useHash();
  const params = new URLSearchParams(hash.split("?")[1]);
  const filters = {
    repo: params.get("repo") || "",
    harness: params.get("harness") || "",
    status: params.get("status") || "",
    search: params.get("search") || "",
  };
  const [view, setView] = useState(() => storedChoice("pactify-jobs-view", "lines") === "list" ? "list" : "lines");
  const [grouping, setGrouping] = useState<LineGrouping>("project");
  const [filtersOpen, setFiltersOpen] = useState(() => !!(filters.repo || filters.harness || filters.status));
  const filtersId = useId();
  const [compact, setCompact] = useState(false);
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const list = useRef<HTMLDivElement>(null);
  const search = useRef<HTMLInputElement>(null);
  function filter(key: string, value: string) {
    const next = new URLSearchParams(location.hash.split("?")[1]);
    if (value) next.set(key, value);
    else next.delete(key);
    const nextHash = `${location.hash.split("?")[0] || "#/jobs"}${next.size ? "?" + next : ""}`;
    history.replaceState(null, "", nextHash);
    window.dispatchEvent(new HashChangeEvent("hashchange"));
  }
  const chains = mergeJobChains(jobs).filter(
    (chain) =>
      chain.runs.some(
        (job) =>
          (!filters.repo || job.repo === filters.repo) &&
          (!filters.harness || job.harness === filters.harness),
      ) &&
      (!filters.search ||
        chain.runs.some((job) =>
          matchesSearch(job, filters.search),
        )) &&
      (!filters.status ||
        (filters.status === "failed"
          ? chain.latest.status === "failed"
          : sectionFor(chain.latest) === filters.status)),
  );
  const card = (chain: JobChain, preview = false) => (
    <JobCard
      preview={preview}
      key={chain.root.id}
      chain={chain}
      selected={selected}
      source={source}
      readOnly={readOnly}
      onChanged={onChanged}
      notify={notify}
    />
  );
  if (loaded && !jobs.length) return (
    <>{view === "lines" && <Pipeline chains={[]} grouping={grouping} card={card} />}<JobsOnboarding />{children}</>
  );
  if (!loaded) return <>{view === "lines" && <Pipeline chains={[]} grouping={grouping} card={card} />}{children}</>;
  return (
    <>
      <div className={`jobs-toolbar ${view === "lines" ? "lines-toolbar" : "list-toolbar"} ${filtersOpen ? "filters-open" : ""}`}>
        <Segmented label="Jobs view" value={view} options={[["lines", "Lines"], ["list", "List"]]}
          onChange={value => { setView(value); saveChoice("pactify-jobs-view", value); }} />
        {view === "lines" && <div className="line-group-control">
          <span className="section-label">Lines by:</span>
          <Segmented label="Group production lines by" value={grouping}
            options={[["project", "Project"], ["harness", "Harness"], ["one", "One line"]]} onChange={setGrouping} />
        </div>}
        <label className="jobs-search">
          <span className="sr-only">Search jobs</span>
          <input
            ref={search}
            aria-keyshortcuts="/"
            type="search"
            placeholder="Search jobs"
            value={filters.search}
            onChange={(e) => filter("search", e.target.value)}
          />
        </label>
        <JobsKeyboard list={list} search={search} />
        <Button className="filters-toggle" size="sm" variant="ghost" aria-expanded={filtersOpen}
          aria-controls={filtersId} onClick={() => setFiltersOpen(!filtersOpen)}>Filters</Button>
        <div id={filtersId} className="filters-controls" hidden={view === "lines" && !filtersOpen}>
        <Button size="sm" variant="ghost" aria-pressed={filters.status === "attention"}
          onClick={() => filter("status", filters.status === "attention" ? "" : "attention")}>
          Needs attention
        </Button>
        <label title={filters.repo}>
          Project
          <select
            aria-label="Project"
            title={filters.repo}
            value={filters.repo}
            onChange={(e) => filter("repo", e.target.value)}
          >
            <option value="">All projects</option>
            {[...new Set(jobs.map((j) => j.repo))]
              .filter(Boolean)
              .sort()
              .map((repo) => (
                <option key={repo} value={repo} title={repo}>
                  {projectName(repo)}
                </option>
              ))}
          </select>
        </label>
        <label>
          Harness
          <select
            aria-label="Harness"
            value={filters.harness}
            onChange={(e) => filter("harness", e.target.value)}
          >
            <option value="">All harnesses</option>
            {[...new Set(jobs.map((j) => j.harness))].sort().map((h) => (
              <option key={h}>{h}</option>
            ))}
          </select>
        </label>
        <label>
          Status
          <select
            aria-label="Status"
            value={filters.status}
            onChange={(e) => filter("status", e.target.value)}
          >
            {[
              ["", "All"],
              ["progress", "In progress"],
              ["queued", "Queued"],
              ["attention", "Needs attention"],
              ["completed", "Completed"],
              ["failed", "Failed"],
            ].map(([value, label]) => (
              <option value={value} key={value}>
                {label}
              </option>
            ))}
          </select>
        </label>
        </div>
      </div>
      {loaded && (
        <div className="jobs-sections" ref={list}>
          {!chains.length && (
            <p className="panel-empty">No jobs match these filters.</p>
          )}
          {view === "lines" ? <Pipeline chains={chains} grouping={grouping} card={card}/> : <>
          {(
            [
              ["progress", "In progress"],
              ["queued", "Queued"],
              ["attention", "Needs attention"],
            ] as const
          ).map(([key, label]) => {
            const rows = chains.filter((c) => sectionFor(c.latest) === key);
            return rows.length ? (
              <section aria-label={label} key={key}>
                <h2>
                  {label}
                  <span className="section-count">{rows.length}</span>
                </h2>
                <div className="job-grid">
                  {rows.map((chain) => card(chain))}
                </div>
              </section>
            ) : null;
          })}
          {!!chains.filter((c) => sectionFor(c.latest) === "completed")
            .length && (
            <section aria-label="Completed">
              <div className="section-heading">
                <h2>Completed</h2>
                <Button size="sm" variant="ghost" aria-pressed={compact} onClick={() => setCompact(!compact)}>
                  Compact list
                </Button>
              </div>
              {completedDays(
                chains.filter((c) => sectionFor(c.latest) === "completed"),
              ).map(({ day, label, rows }) => (
                <div key={day} className="completed-day">
                  <div className="day-heading">
                    <h3>
                      {label} · {rows.length} completed
                    </h3>
                    {!compact && <Button
                      size="sm"
                      variant="ghost"
                      aria-expanded={!!expanded[day]}
                      aria-controls={`day-${day}`}
                      onClick={() =>
                        setExpanded({ ...expanded, [day]: !expanded[day] })
                      }
                    >
                      {expanded[day] ? "Collapse" : "Show all"}
                    </Button>}
                  </div>
                  {compact ? <ul className="compact-history" aria-label="Completed jobs">
                    {rows.map(chain => <CompletedRow key={chain.root.id} chain={chain} />)}
                  </ul> : <div
                    id={`day-${day}`}
                    className={expanded[day] ? "job-grid" : "job-stack"}
                  >
                    {(expanded[day] ? rows : rows.slice(0, 3)).map(
                      (chain, index) =>
                        card(chain, !expanded[day] && index > 0),
                    )}
                  </div>}
                </div>
              ))}
            </section>
          )}
          </>}
        </div>
      )}
      {children}
    </>
  );
}
export function Jobs({
  source = dashboardJobsSource,
  standalone = false,
  jobs: supplied,
  refresh: suppliedRefresh,
  loaded: suppliedLoaded,
}: {
  source?: JobsSource;
  standalone?: boolean;
  jobs?: Job[];
  refresh?: () => void;
  loaded?: boolean;
}) {
  const resource = useResource<Job[]>(
    source,
    supplied ? undefined : `${source.jobsPath}?all=1`,
    2000,
    validJobs,
  );
  const jobs = supplied || (Array.isArray(resource.data) ? resource.data : []);
  const hash = useHash();
  const match = hash.split("?")[0].match(/^#\/jobs\/([^/]+)$/);
  let id = "";
  try {
    id = match ? decodeURIComponent(match[1]) : "";
  } catch {
    /* Invalid encoded links show the list. */
  }
  const [toast, setToast] = useState("");
  useEffect(() => {
    if (!toast) return;
    const timer = setTimeout(() => setToast(""), 5000);
    return () => clearTimeout(timer);
  }, [toast]);
  const changed = suppliedRefresh || resource.refresh;
  return (
    <div className="jobs-view">
      <div inert={!!id}>
        <JobList
          jobs={jobs}
          selected={id}
          source={source}
          readOnly={standalone}
          onChanged={changed}
          notify={setToast}
          loaded={suppliedLoaded ?? !!resource.data}
        />
        {resource.error && (
          <p role="alert" className="panel-error">
            {resource.error}
          </p>
        )}
        {!supplied && !resource.data && !resource.error && (
          <LoadingJobs />
        )}
      </div>
      {id && (
        <JobDrawer id={id} jobs={jobs}>
          <JobDetail
            key={id}
            id={id}
            jobs={jobs}
            source={source}
            readOnly={standalone}
            onChanged={changed}
            notify={setToast}
            initialTab={
              new URLSearchParams(hash.split("?")[1]).get("tab") || ""
            }
          />
        </JobDrawer>
      )}
      {toast &&
        createPortal(
          <div className="v2-app v2-modal-host">
            <div className="v2-toast" role="status">
              {toast}
            </div>
          </div>,
          document.body,
        )}
    </div>
  );
}
