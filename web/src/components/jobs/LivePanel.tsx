import { useEffect, useRef, useState } from "react";
import type { Job } from "./types";
import type { JobsSource } from "./source";
import { useResource } from "./data";
import {
  emptyLive,
  frameBatch,
  liveEdges,
  reduceLive,
  selectLiveJobs,
} from "./liveTree";
import { liveStream } from "./liveStreams";
import { formatCost, formatDuration, parseQuotaWindows } from "./statistics";
import "./live.css";

interface RouterSnapshot {
  config?: {
    aliases?: Record<
      string,
      { harness: string; model?: string; effort?: string }
    >;
    fallback?: Record<string, string[]>;
  };
  exhaustedRoutes?: { key: string }[];
}
function LiveJob({
  job,
  source,
  replay,
}: {
  job: Job;
  source: JobsSource;
  replay: boolean;
}) {
  const [state, setState] = useState(emptyLive);
  const [status, setStatus] = useState("Connecting");
  const [now, setNow] = useState(Date.now);
  const log = useRef<HTMLDivElement>(null);
  useEffect(() => {
    let stop: (() => void) | undefined;
    let timer: ReturnType<typeof setInterval> | undefined;
    const batch = frameBatch<unknown>((events) =>
      setState((s) => reduceLive(s, events)),
    );
    const sync = () => {
      stop?.();
      stop = undefined;
      clearInterval(timer);
      batch.dispose();
      if (document.visibilityState === "hidden") {
        setStatus("Paused · page hidden");
        return;
      }
      // Every connection starts from the bounded tail; replace rather than
      // double-count that tail after returning to the tab.
      setState(emptyLive());
      stop = liveStream(source, job.id, (e) => batch.add(e), setStatus);
      timer = setInterval(() => setNow(Date.now()), 1000);
    };
    sync();
    document.addEventListener("visibilitychange", sync);
    return () => {
      stop?.();
      clearInterval(timer);
      batch.dispose();
      document.removeEventListener("visibilitychange", sync);
    };
  }, [source, job.id]);
  useEffect(() => {
    if (log.current) log.current.scrollTop = log.current.scrollHeight;
  }, [state.log]);
  const terminal = !["running", "retrying", "queued"].includes(job.status);
  const phase = terminal
    ? job.status === "succeeded"
      ? "done"
      : job.status
    : state.phase || job.status;
  const input = terminal
    ? job.inputTokens
    : (state.usage?.inputTokens ?? job.activity?.inputTokens);
  const output = terminal
    ? job.outputTokens
    : (state.usage?.outputTokens ?? job.activity?.outputTokens);
  const diff =
    terminal && job.changedFiles !== undefined
      ? {
          files: job.changedFiles,
          added: job.addedLines || 0,
          deleted: job.deletedLines || 0,
        }
      : state.diff;
  const cost = job.costUSD ?? state.costUSD;
  const duration =
    !terminal && job.startedAt
      ? formatDuration(Math.max(0, (now - Date.parse(job.createdAt)) / 1000))
      : job.duration;
  const calls = Object.values(state.tools).reduce((n, t) => n + t.calls, 0);
  return (
    <article className="live-branch" aria-label={`Live job ${job.id}`}>
      <div className="live-node live-harness" data-phase={phase}>
        <div className="live-node-heading">
          <strong>{job.harness}</strong>
          <span className="live-phase">{phase}</span>
        </div>
        <span>
          {job.modelActual || job.modelRequested || "Model unknown"} ·{" "}
          {job.effort || "Effort unknown"}
        </span>
        <small>
          {job.id} · {duration}
        </small>
        <div className="live-metrics">
          <span>
            {input !== undefined && output !== undefined
              ? `${(input + output).toLocaleString("en-US")} tokens`
              : "Tokens unknown"}
          </span>
          <span>
            {calls} observed {calls === 1 ? "call" : "calls"}
          </span>
        </div>
        {input !== undefined && output !== undefined && input + output > 0 && (
          <>
            <meter
              aria-label="Input share of observed tokens"
              min={0}
              max={input + output}
              value={input}
            />
            <small>
              {input.toLocaleString("en-US")} in /{" "}
              {output.toLocaleString("en-US")} out
              {(job.usagePartial || job.activity?.usagePartial || !terminal) &&
                " · partial"}
            </small>
          </>
        )}
        {cost != null && <span>{formatCost(cost)}</span>}
        {diff && (
          <span>
            {diff.files} files · +{diff.added} / −{diff.deleted}
          </span>
        )}
        {parseQuotaWindows(
          state.rateLimit || "",
          new Date(now).toISOString(),
        ).map((w) => (
          <label className="live-quota" key={w.label}>
            {w.label} · {w.remaining}% remaining
            <progress max={100} value={w.remaining} />
          </label>
        ))}
        {!!(job.retryAttempt || job.retryCount) && (
          <div className="live-retry">
            <svg viewBox="0 0 32 28" aria-hidden="true">
              <path d="M24 20a11 11 0 1 1 1-13M19 7h7V1" />
            </svg>
            retry ×{job.retryAttempt || job.retryCount}
          </div>
        )}
        <small className="live-stream-status">{status}</small>
      </div>
      <div className="live-tools" aria-label="Observed tools">
        {Object.entries(state.tools).map(([kind, tool]) => (
          <div className="live-tool-link" key={kind}>
            {!replay && (
              <i key={tool.pulse} className="live-pulse" aria-hidden="true" />
            )}
            <div className="live-node live-tool" data-active={tool.active > 0}>
              <strong>{kind}</strong>
              <span>
                {tool.calls} {tool.calls === 1 ? "call" : "calls"} ·{" "}
                {tool.active} active
              </span>
            </div>
          </div>
        ))}
      </div>
      <h3 className="live-session-heading">Session log</h3>
      <div
        ref={log}
        className="live-session"
        role="log"
        aria-label={`${job.harness} session log`}
        aria-live="off"
      >
        {state.log.length ? (
          state.log.map((line, i) => (
            // log.jsonl rows usually carry no timestamp; omit the column then.
            <div key={i} className={line.time ? undefined : "no-time"}>
              {line.time && (
                <time>{line.time.replace("T", " ").slice(0, 19)}</time>
              )}
              <b>{job.harness}</b>
              <span>{line.text}</span>
            </div>
          ))
        ) : (
          <p>
            {job.activity?.summary ||
              "No observed messages in this log window."}
          </p>
        )}
      </div>
    </article>
  );
}
export function LiveTree({
  source,
  jobs,
  id,
  unavailable = false,
}: {
  source: JobsSource;
  jobs: Job[] | null;
  id?: string;
  unavailable?: boolean;
}) {
  const router = useResource<RouterSnapshot>(source, source.routerPath, 5000);
  const selected = selectLiveJobs(jobs || [], id);
  const ids = new Set(selected.jobs.map((j) => j.id));
  const edges = liveEdges(jobs || []).filter(
    (e) => ids.has(e.from) || ids.has(e.to),
  );
  return (
    <section className="panel-section live-panel" aria-label="Live agent tree">
      <div className="panel-section-heading">
        <h2>Live</h2>
        <span>
          {selected.replay
            ? "Recent run replay · recorded tail"
            : "Observed activity"}
        </span>
      </div>
      {unavailable && <p>Job summary unavailable</p>}
      {!jobs ? (
        <p>Loading activity…</p>
      ) : !selected.jobs.length ? (
        <p>No runs to display.</p>
      ) : (
        <>
          <div className="live-trunk">
            <div className="live-node live-host">
              <strong>Claude Code (host)</strong>
            </div>
            <div className="live-connector" aria-hidden="true" />
            <div className="live-node live-router">
              <strong>Router</strong>
              {router.error ? (
                <span>Router unavailable</span>
              ) : !router.data ? (
                <span>Loading routes…</span>
              ) : (
                <>
                  {Object.entries(router.data.config?.aliases || {}).map(
                    ([alias, r]) => (
                      <span key={alias}>
                        @{alias} → {r.harness}
                        {r.model ? ` · ${r.model}` : ""}
                        {r.effort ? ` · ${r.effort}` : ""}
                      </span>
                    ),
                  )}
                  {Object.entries(router.data.config?.fallback || {}).map(
                    ([h, chain]) => (
                      <span key={h}>
                        {h} fallback → {chain.join(" → ")}
                      </span>
                    ),
                  )}
                  {(router.data.exhaustedRoutes || []).map((r) => (
                    <span className="live-exhausted" key={r.key}>
                      {r.key} · exhausted
                    </span>
                  ))}
                  {!Object.keys(router.data.config?.aliases || {}).length && (
                    <span>No aliases configured</span>
                  )}
                </>
              )}
            </div>
          </div>
          {!!edges.length && (
            <div className="live-relations" aria-label="Job relationships">
              {edges
                .filter((e) => e.kind !== "retry")
                .map((e) => (
                  <div
                    className={`live-relation live-${e.kind}`}
                    key={`${e.kind}:${e.from}:${e.to}`}
                  >
                    <span>{e.from}</span>
                    <svg viewBox="0 0 80 24" aria-hidden="true">
                      <path
                        d={
                          e.kind === "fallback"
                            ? "M0 5H25 M38 5Q48 5 48 17H80"
                            : "M0 5H38Q48 5 48 17H80"
                        }
                      />
                    </svg>
                    <span>
                      {e.kind} → {e.to}
                    </span>
                  </div>
                ))}
            </div>
          )}
          <div className="live-jobs">
            {selected.jobs.map((job) => (
              <LiveJob
                key={job.id}
                job={job}
                source={source}
                replay={selected.replay}
              />
            ))}
          </div>
          <p className="panel-caption">
            Last 1 MiB, up to 100 initial events. Calls and tools reflect this
            observed window. Up to 4 event streams; other jobs use summary
            polling. Replay shows recorded data without timed animation.
          </p>
        </>
      )}
    </section>
  );
}
