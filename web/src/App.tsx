import { useEffect, useState } from "react";
import { Jobs, type Job } from "./components/jobs/Jobs";
import { Dashboard } from "./components/jobs/Dashboard";
import { Settings } from "./components/jobs/Settings";
import { useHash, useResource, validJobs } from "./components/jobs/data";
import { dashboardJobsSource, type JobsSource } from "./components/jobs/source";
import { LoadingJobs } from "./components/jobs/NextSteps";
import { CableMark } from "./components/shell/CableMark";
import { mergeJobChains } from "./components/jobs/board";
import { sectionFor } from "./components/jobs/statistics";
export type Lens = "dashboard" | "jobs" | "settings";
export function AppContent({
  source = dashboardJobsSource,
  readOnly = false,
}: {
  source?: JobsSource;
  readOnly?: boolean;
}) {
  const hash = useHash();
  const route = hash.split("?")[0].split("/")[1];
  const page: Lens =
    route === "dashboard" || route === "settings" ? route : "jobs";
  const resource = useResource<Job[]>(
    source,
    `${source.jobsPath}?all=1`,
    2000,
    validJobs,
  );
  const info = useResource<{ version: string }>(
    source,
    source.jobsPath.replace(/\/jobs$/, "/info"),
  );
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const timer = setInterval(() => {
      if (!document.hidden) setNow(Date.now());
    }, 1000);
    return () => clearInterval(timer);
  }, []);
  const jobs = Array.isArray(resource.data) ? resource.data : null;
  const detailOpen = page === "jobs" && /^#\/jobs\/[^/?]+/.test(hash);
  const attention = mergeJobChains(jobs || []).filter(
    chain => sectionFor(chain.latest) === "attention",
  ).length;
  useEffect(() => {
    document.title = `${attention ? `(${attention}) ` : ""}${readOnly ? "pactify panel" : `Pactify · ${page[0].toUpperCase() + page.slice(1)}`}`;
  }, [page, readOnly, attention]);
  function navigate(value: string) {
    location.hash = "/" + value;
  }
  return (
    <div className="v2-app">
      <header className="v2-header" inert={detailOpen}>
        <a className="v2-brand" href="#/jobs" aria-label="Pactify Jobs">
          <CableMark />
          <strong>pactify</strong>
          {readOnly && <small title="This panel is read-only. Make changes with the CLI or pactify serve.">panel</small>}
        </a>
        <nav aria-label="Main navigation" className="v2-navigation">
          {(["dashboard", "jobs", "settings"] as const).map((value) => (
            <a
              href={`#/${value}`}
              key={value}
              aria-current={page === value ? "page" : undefined}
            >
              {value[0].toUpperCase() + value.slice(1)}
            </a>
          ))}
        </nav>
        <select
          className="v2-mobile-nav"
          aria-label="Page"
          value={page}
          onChange={(e) => navigate(e.target.value)}
        >
          {["Dashboard", "Jobs", "Settings"].map((label) => (
            <option key={label} value={label.toLowerCase()}>
              {label}
            </option>
          ))}
        </select>
        <div className="header-links">
          {info.data?.version && <span aria-label="Pactify version">{info.data.version}</span>}
          <a href="https://github.com/agentjoey/pactify#readme" target="_blank" rel="noopener noreferrer">Docs</a>
        </div>
        <span className="v2-updated" role="status">
          {resource.error
            ? "Update unavailable"
            : resource.updated
              ? `Updated ${Math.max(0, Math.floor((now - +resource.updated) / 1000))}s ago`
              : "Updating…"}
        </span>
      </header>
      <main className="v2-main">
        <div className="page-heading" inert={detailOpen}>
          <p className="panel-eyebrow">Local jobs</p>
          <h1>{page[0].toUpperCase() + page.slice(1)}</h1>
        </div>
        {!jobs && !resource.error ? <LoadingJobs /> : page === "jobs" && (
          <Jobs
            source={source}
            standalone={readOnly}
            jobs={jobs || []}
            loaded={!!jobs}
            refresh={resource.refresh}
          />
        )}
        {page !== "dashboard" && resource.error && (
          <p role="alert" className="panel-error">
            {resource.error}
          </p>
        )}
        {page === "dashboard" && (!!jobs || !!resource.error) && (
          <Dashboard
            source={source}
            jobs={jobs}
            jobsUnavailable={!!resource.error}
            jobsError={resource.error}
          />
        )}
        {page === "settings" && (!!jobs || !!resource.error) && (
          <Settings source={source} readOnly={readOnly} />
        )}
      </main>
    </div>
  );
}
export default function App() {
  return <AppContent />;
}
