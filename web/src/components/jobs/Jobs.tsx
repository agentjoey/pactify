import { useEffect, useState, type ReactNode } from "react";
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
  formatCost,
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
import "./jobs.css";
export type { Job } from "./types";
export { LogView, splitDiff } from "./logs";
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
          j.title.toLowerCase().includes(filters.search.toLowerCase())),
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
      {job.costUSD != null && <span>{formatCost(job.costUSD)}</span>}
    </div>
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
  const open = () => {
    location.hash = jobsLink(job.id);
  };
  return (
    <article
      inert={preview}
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
        <Badge
          color={
            job.status === "failed"
              ? "danger"
              : ["running", "succeeded"].includes(job.status)
                ? "success"
                : "warn"
          }
        >
          {job.status === "running" && job.retryAttempt
            ? `retrying (${job.retryAttempt}/${job.retryLimit})`
            : job.status}
        </Badge>
        <span className="harness-label">
          {job.harness} ·{" "}
          {job.modelActual || job.modelRequested || "Harness default"}
        </span>
      </div>
      <h3>
        <a href={jobsLink(job.id)}>{chain.root.title || chain.root.id}</a>
      </h3>
      <JobMetadata job={job} />
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
        {failures.map((f) => (
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
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
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
          job.title.toLowerCase().includes(filters.search.toLowerCase()),
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
  if (loaded && !jobs.length) return <><JobsOnboarding />{children}</>;
  if (!loaded) return <>{children}</>;
  return (
    <>
      <div className="jobs-toolbar">
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
        <label className="jobs-search">
          <span className="sr-only">Search jobs</span>
          <input
            type="search"
            placeholder="Search jobs"
            value={filters.search}
            onChange={(e) => filter("search", e.target.value)}
          />
        </label>
      </div>
      {loaded && (
        <div className="jobs-sections">
          {!chains.length && (
            <p className="panel-empty">No jobs match these filters.</p>
          )}
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
              <h2>Completed</h2>
              {completedDays(
                chains.filter((c) => sectionFor(c.latest) === "completed"),
              ).map(({ day, label, rows }) => (
                <div key={day} className="completed-day">
                  <div className="day-heading">
                    <h3>
                      {label} · {rows.length} completed
                    </h3>
                    <Button
                      size="sm"
                      variant="ghost"
                      aria-expanded={!!expanded[day]}
                      aria-controls={`day-${day}`}
                      onClick={() =>
                        setExpanded({ ...expanded, [day]: !expanded[day] })
                      }
                    >
                      {expanded[day] ? "Collapse" : "Show all"}
                    </Button>
                  </div>
                  <div
                    id={`day-${day}`}
                    className={expanded[day] ? "job-grid" : "job-stack"}
                  >
                    {(expanded[day] ? rows : rows.slice(0, 3)).map(
                      (chain, index) =>
                        card(chain, !expanded[day] && index > 0),
                    )}
                  </div>
                </div>
              ))}
            </section>
          )}
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
