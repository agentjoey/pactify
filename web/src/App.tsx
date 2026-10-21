import { useEffect, useState } from "react";
import { Jobs, type Job } from "./components/jobs/Jobs";
import { Dashboard } from "./components/jobs/Dashboard";
import { Settings } from "./components/jobs/Settings";
import { useHash, useResource, validJobs } from "./components/jobs/data";
import { dashboardJobsSource, type JobsSource } from "./components/jobs/source";
import { LoadingJobs } from "./components/jobs/NextSteps";
import { CableMark } from "./components/shell/CableMark";
import { mergeJobChains } from "./components/jobs/board";
import { LiveIndicator, ThemeControl } from "./components/ui/Design";
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
  const chains = mergeJobChains(jobs || []);
  const attention = chains.filter(
    chain => sectionFor(chain.latest) === "attention",
  ).length;
  const running = chains.some(chain => sectionFor(chain.latest) === "progress");
  useEffect(() => {
    let icon = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
    if (!icon) {
      icon = document.createElement("link");
      icon.rel = "icon";
      document.head.append(icon);
    }
    icon.type = "image/svg+xml";
    icon.href = `/favicon-${attention ? "attention" : running ? "running" : "neutral"}.svg`;
  }, [attention, running]);
  useEffect(() => {
    document.title = `${attention ? `(${attention}) ` : ""}${readOnly ? "pactify panel" : `Pactify · ${page[0].toUpperCase() + page.slice(1)}`}`;
  }, [page, readOnly, attention]);
  function focusSearch() {
    const input = document.querySelector<HTMLInputElement>(".jobs-search input");
    if (page === "jobs" && input) { input.focus(); return; }
    if (page !== "jobs") location.hash = "/jobs";
    requestAnimationFrame(() => requestAnimationFrame(() => document.querySelector<HTMLInputElement>('.jobs-search input')?.focus()));
  }
  useEffect(() => {
    const keydown = (event: KeyboardEvent) => {
      if (document.querySelector('[aria-modal="true"]')) return;
      const commandSearch = (event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k";
      const slashSearch = event.key === "/"
        && !(event.target as HTMLElement).closest?.('input,textarea,select,[contenteditable="true"]');
      if (commandSearch || slashSearch) {
        event.preventDefault();
        focusSearch();
      }
    };
    window.addEventListener("keydown", keydown);
    return () => window.removeEventListener("keydown", keydown);
  }, [page]);
  return (
    <div className="v2-app">
      <header className="v2-header" inert={detailOpen}>
        <a className="v2-brand" href="#/jobs" aria-label="Pactify Jobs">
          <CableMark />
          <strong>pactify</strong>
          {readOnly && <small title="This panel is read-only. Make changes with the CLI or pactify serve.">read-only</small>}
        </a>
        <nav aria-label="Main navigation" className="v2-navigation">
          {(["dashboard", "jobs", "settings"] as const).map((value) => (
            <a
              href={`#/${value}`}
              aria-label={value[0].toUpperCase() + value.slice(1)}
              key={value}
              aria-current={page === value ? "page" : undefined}
            >
              {value[0].toUpperCase() + value.slice(1)}
              {value === "jobs" && attention > 0 && (
                <span className="nav-count" aria-label={`${attention} needing attention`}>{attention}</span>
              )}
            </a>
          ))}
        </nav>
        <div className="header-tools">
          <button type="button" className="header-search icon-control" aria-label="Search jobs (⌘K or /)" onClick={focusSearch}>⌕ <kbd>⌘K</kbd></button>
          <LiveIndicator error={resource.error} updated={resource.updated} now={now} />
          <ThemeControl />
          <a href="#/settings" className="icon-control" aria-label="Open Settings">⚙</a>
          <div className="header-links">
            {info.data?.version && <span aria-label="Pactify version">{info.data.version}</span>}
            <a href="https://github.com/agentjoey/pactify#readme" target="_blank" rel="noopener noreferrer">Docs</a>
          </div>
        </div>
      </header>
      <main className="v2-main">
        <div className="page-heading" inert={detailOpen}>
          <p className="panel-eyebrow">{page === "settings" ? "Machine · this computer" : "Local jobs · this computer"}</p>
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
            readOnly={readOnly}
            onChanged={resource.refresh}
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
