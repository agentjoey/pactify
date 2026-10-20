import { useEffect, useRef, useState } from "react";
import type { Job } from "./types";
import type { JobsSource } from "./source";
import { readJobLogs } from "./source";
import { jobsLink, useResource } from "./data";
import { mergeJobChains } from "./board";
import { JobMetadata } from "./Jobs";
import { JobActions } from "./JobActions";
import { LogView } from "./logs";
import { DiffView, type Diff } from "./DiffView";
import { NextSteps } from "./NextSteps";
import { Button } from "../ui/Button";
import { LiveTree } from "./LivePanel";
interface Detail {
  job: Job;
  brief: string;
  markdown: string;
  resultError?: string;
  result?: { finalText?: string; warnings?: string[]; costUSD?: number };
}
const tabs = ["Summary", "Live", "Logs", "Diff", "Brief"];
export function JobDetail({
  id,
  jobs,
  source,
  readOnly,
  onChanged,
  notify,
  initialTab,
}: {
  id: string;
  jobs: Job[];
  source: JobsSource;
  readOnly: boolean;
  onChanged: () => void;
  notify: (message: string) => void;
  initialTab: string;
}) {
  const [selectedTab, setTab] = useState(
    tabs.find(t => t.toLowerCase() === initialTab),
  );
  const heading = useRef<HTMLHeadingElement>(null);
  const commands = useRef<HTMLDivElement>(null);
  const [narrow, setNarrow] = useState(() => window.matchMedia?.("(max-width: 700px)").matches ?? false);
  const [commandsOpen, setCommandsOpen] = useState(false);
  useEffect(() => {
    const media = window.matchMedia?.("(max-width: 700px)");
    if (!media) return;
    const update = () => setNarrow(media.matches);
    update();
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);
  useEffect(() => {
    if (narrow && commandsOpen) {
      commands.current?.scrollIntoView({block: "start"});
      commands.current?.focus({preventScroll: true});
    }
  }, [narrow, commandsOpen]);
  const path = `${source.jobsPath}/${encodeURIComponent(id)}`;
  const detail = useResource<Detail>(source, path, 2000);
  const job = detail.data?.job || jobs.find(job => job.id === id);
  const tab = selectedTab || (
    job && ["running", "retrying"].includes(job.status) ? "Live" : "Summary"
  );
  // Once data arrives, preserve the initial tab even when the job finishes.
  useEffect(() => {
    // A click can arrive between this render and its passive effect. Read the
    // latest selection so the default never overwrites that user action.
    if (detail.data && !selectedTab) setTab(current => current || tab);
  }, [detail.data, selectedTab, tab]);
  const diff = useResource<Diff>(
    source,
    tab === "Diff" ? path + "/diff" : undefined,
    5000,
  );
  const [lines, setLines] = useState<string[]>([]);
  const [logStatus, setLogStatus] = useState("Connecting");
  useEffect(() => {
    if (tab !== "Logs") return;
    setLines([]);
    setLogStatus("Connecting");
    const controller = new AbortController();
    void readJobLogs(
      source.fetch,
      path + "/logs",
      controller.signal,
      (line) => setLines((current) => [...current, line].slice(-1000)),
      setLogStatus,
    ).catch((e) => {
      if (!controller.signal.aborted) setLogStatus((e as Error).message);
    });
    return () => controller.abort();
  }, [source, path, tab]);
  const data = detail.data;
  const chain = mergeJobChains(
    jobs.some(j => j.id === id) ? jobs : data ? [...jobs, data.job] : jobs,
  ).find(c => c.runs.some(j => j.id === id));
  const loaded = !!data;
  useEffect(() => {
    // Only focus on initial load, not every polling update or run switch.
    const active = document.activeElement;
    if (
      loaded &&
      active?.closest('[role="dialog"]') === heading.current?.closest('[role="dialog"]') &&
      active?.getAttribute("data-detail-focus") === "pending"
    ) heading.current?.focus();
  }, [loaded]);
  return (
    <>
      {detail.error && (
        <p role="alert" className="panel-error">
          {detail.error}
        </p>
      )}
      {!data ? (
        !detail.error && <p>Loading job…</p>
      ) : (
        <>
          <div
            className="detail-scroll"
            role="region"
            aria-label="Job detail content"
            tabIndex={0}
          >
            <div className="panel-eyebrow">
              {data.job.id} · {data.job.status}
            </div>
            <h1 ref={heading} tabIndex={-1}>
              {chain ? chain.root.title || chain.root.id : data.job.title || id}
            </h1>
            {chain && chain.runs.length > 1 && (
              <p className="panel-caption">
                Run {chain.runs.findIndex(j => j.id === id) + 1} of {chain.runs.length}
              </p>
            )}
            <JobMetadata job={data.job} />
            <div
              role="tablist"
              aria-label="Job detail tabs"
              className="detail-tabs"
            >
              {tabs.map((name, index) => (
                <button
                  key={name}
                  id={`detail-tab-${name}`}
                  role="tab"
                  aria-controls="detail-tabpanel"
                  aria-selected={tab === name}
                  tabIndex={tab === name ? 0 : -1}
                  onClick={() => setTab(name)}
                  onKeyDown={(e) => {
                    if (
                      !["ArrowLeft", "ArrowRight", "Home", "End"].includes(e.key)
                    )
                      return;
                    e.preventDefault();
                    const next =
                      e.key === "Home"
                        ? tabs[0]
                        : e.key === "End"
                          ? tabs[tabs.length - 1]
                          : tabs[
                              (index +
                                (e.key === "ArrowRight" ? 1 : tabs.length - 1)) %
                                tabs.length
                            ];
                    setTab(next);
                    document.getElementById(`detail-tab-${next}`)?.focus();
                  }}
                >
                  {name}
                </button>
              ))}
            </div>
            <div
              className="detail-body"
              role="tabpanel"
              id="detail-tabpanel"
              aria-labelledby={`detail-tab-${tab}`}
            >
              {tab === "Summary" && (
                <>
                  <section className="panel-section">
                    <h2>Result summary</h2>
                    <pre>
                      {data.markdown || data.result?.finalText || "No result yet"}
                    </pre>
                    {data.resultError && <p role="alert">{data.resultError}</p>}
                  </section>
                  {!!data.result?.warnings?.length && (
                    <section className="panel-section">
                      <h2>Warnings</h2>
                      {data.result.warnings.map((warning, i) => (
                        <p key={i}>{warning}</p>
                      ))}
                    </section>
                  )}
                  {data.job.failureClass && (
                    <p>Error type: {data.job.failureClass === "harness" ? "Agent error" : data.job.failureClass}</p>
                  )}
                  {data.job.attention.map((a, i) => (
                    <p key={i} className="panel-attention">
                      {a.message}
                    </p>
                  ))}
                  <section className="panel-section">
                    <h2>Run timeline</h2>
                    <nav className="panel-timeline" aria-label="Run timeline">
                      {(chain?.runs || [data.job]).map((run) => (
                        <a
                          key={run.id}
                          href={jobsLink(run.id)}
                          aria-current={run.id === id ? "step" : undefined}
                        >
                          {run.id} · {run.status} · {run.harness} · {run.duration}
                        </a>
                      ))}
                    </nav>
                    {(data.job.fallbackFrom || data.job.fallbackTo) && (
                      <nav className="panel-timeline" aria-label="Fallback chain">
                        {data.job.fallbackFrom && (
                          <a href={jobsLink(data.job.fallbackFrom)}>
                            fallback from {data.job.fallbackFrom}
                          </a>
                        )}
                        {data.job.fallbackTo && (
                          <a href={jobsLink(data.job.fallbackTo)}>
                            → fallback to {data.job.fallbackTo}
                          </a>
                        )}
                      </nav>
                    )}
                  </section>
                  <section className="panel-section">
                    <h2>Workspace</h2>
                    <p>{data.job.repo}</p>
                    <p title={`Configuration: ${data.job.configMode || "unknown"}`}>
                      {data.job.access === "write" ? "Write access" : "Read-only access"}
                    </p>
                    {data.job.worktree && (
                      <>
                        <p>
                          Worktree: {data.job.worktree.path} ·{" "}
                          {data.job.worktree.status}
                        </p>
                        <p>Base: {data.job.worktree.baseCommit}</p>
                      </>
                    )}
                  </section>
                  <section className="panel-section">
                    <h2>Usage</h2>
                    <JobMetadata
                      job={{ ...data.job, costUSD: data.job.costUSD ?? data.result?.costUSD }}
                    />
                    <p>
                      {data.job.harness} ·{" "}
                      {data.job.modelActual ||
                        data.job.modelRequested ||
                        "Harness default"}
                    </p>
                  </section>
                </>
              )}
              {tab === "Live" && (
                <LiveTree
                  source={source}
                  jobs={[...jobs.filter((j) => j.id !== id), data.job]}
                  id={id}
                />
              )}
              {tab === "Logs" && <LogView lines={lines} status={logStatus} />}
              {tab === "Diff" && (
                <section className="panel-section">
                  <h2>Diff</h2>
                  {diff.error ? (
                    <p role="alert">{diff.error}</p>
                  ) : !diff.data ? (
                    <p>Loading diff…</p>
                  ) : (
                    <DiffView diff={diff.data} job={data.job} />
                  )}
                </section>
              )}
              {tab === "Brief" && (
                <section className="panel-section">
                  <h2>Task brief</h2>
                  <pre>{data.brief}</pre>
                </section>
              )}
            </div>
            {readOnly && narrow && commandsOpen && (
              <div id="detail-terminal-commands" className="detail-commands" ref={commands} tabIndex={-1}>
                <NextSteps job={data.job} title="Terminal commands" />
              </div>
            )}
          </div>
          <footer className="detail-footer">
            {readOnly ? (narrow ? (
              <div className="detail-next-steps">
                <h2>Next steps</h2>
                <Button
                  size="sm"
                  variant="ghost"
                  aria-expanded={commandsOpen}
                  aria-controls="detail-terminal-commands"
                  onClick={() => setCommandsOpen(!commandsOpen)}
                >
                  {commandsOpen ? "Hide commands" : "Show commands"}
                </Button>
              </div>
            ) : <NextSteps job={data.job} />) : (
              <JobActions
                job={data.job}
                source={source}
                onChanged={() => {
                  detail.refresh();
                  diff.refresh();
                  onChanged();
                }}
                notify={notify}
              />
            )}
          </footer>
        </>
      )}
    </>
  );
}
