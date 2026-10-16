import { useEffect } from "react";
import { Jobs, type Job } from "./components/jobs/Jobs";
import { Dashboard } from "./components/jobs/Dashboard";
import { Settings } from "./components/jobs/Settings";
import { useHash, useResource, validJobs } from "./components/jobs/data";
import { dashboardJobsSource, type JobsSource } from "./components/jobs/source";
import { CableMark } from "./components/shell/CableMark";
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
  const jobs = Array.isArray(resource.data) ? resource.data : null;
  const detailOpen = page === "jobs" && /^#\/jobs\/[^/?]+/.test(hash);
  useEffect(() => {
    document.title = `Pactify · ${page[0].toUpperCase() + page.slice(1)}`;
  }, [page]);
  function navigate(value: string) {
    location.hash = "/" + value;
  }
  return (
    <div className="v2-app">
      <header className="v2-header" inert={detailOpen}>
        <a className="v2-brand" href="#/jobs" aria-label="Pactify Jobs">
          <CableMark />
          <strong>pactify</strong>
          {readOnly && <small>panel</small>}
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
        <span className="v2-updated" role="status">
          {resource.error
            ? "Update unavailable"
            : resource.updated
              ? `Updated ${resource.updated.toLocaleTimeString("en-US", { hour: "numeric", minute: "2-digit" })}`
              : "Updating…"}
        </span>
      </header>
      <main className="v2-main">
        <div className="page-heading" inert={detailOpen}>
          <p className="panel-eyebrow">LOCAL HARNESS ROUTER</p>
          <h1>{page[0].toUpperCase() + page.slice(1)}</h1>
        </div>
        {page === "jobs" && (
          <Jobs
            source={source}
            standalone={readOnly}
            jobs={jobs || []}
            loaded={!!jobs}
            refresh={resource.refresh}
          />
        )}
        {page === "jobs" && resource.error && (
          <p role="alert" className="panel-error">
            {resource.error}
          </p>
        )}
        {page === "dashboard" && (
          <Dashboard
            source={source}
            jobs={jobs}
            jobsUnavailable={!!resource.error}
          />
        )}
        {page === "settings" && (
          <Settings source={source} readOnly={readOnly} />
        )}
      </main>
    </div>
  );
}
export default function App() {
  return <AppContent />;
}
