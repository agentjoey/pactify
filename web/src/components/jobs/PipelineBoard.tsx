import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import type { JobChain } from "./board";
import type { Job } from "./types";
import {
  endedToday, groupLines, relativeTime, stations, stationFor, stationStat,
  stationTodayCount, stationLeft, type LineGrouping,
} from "./pipeline";
import { jobsLink } from "./data";
import { SectionLabel } from "../ui/Design";
import { Button } from "../ui/Button";

type Line = ReturnType<typeof groupLines>[number];
type Station = typeof stations[number];

function LineHeading({ line, grouping, index }: { line: Line; grouping: LineGrouping; index: number }) {
  const running = line.jobs.filter(job => stationFor(job) === "progress").length;
  const shipped = line.jobs.filter(job => stationFor(job) === "completed" && endedToday(job)).length;
  const harnesses = [...new Set(line.jobs.map(job => job.harness))];
  return (
    <div className={`line-heading line-color-${index % 3}`}>
      <h3><i aria-hidden="true" />{line.name}</h3>
      <p>{grouping === "harness" ? `${new Set(line.jobs.map(job => job.repo)).size} projects` : harnesses.join(" · ")}</p>
      <div className="line-load" role="meter" aria-label={`${line.name} running share`}
        aria-valuemin={0} aria-valuemax={line.jobs.length} aria-valuenow={running}>
        <span style={{ width: `${100 * running / Math.max(1, line.jobs.length)}%` }} />
      </div>
      <small>{running || shipped ? `${running} running · ${shipped} shipped today` : "idle"}</small>
    </div>
  );
}

function ShippedRow({ chain, now }: { chain: JobChain; now: number }) {
  const { latest: job } = chain;
  const ok = (job.status === "succeeded" || job.worktree?.status === "applied") && job.worktree?.status !== "discarded";
  return (
    <article className="shipped-row" onClick={event => {
      if (!(event.target as HTMLElement).closest("a,button")) location.hash = jobsLink(job.id);
    }}>
      <span className={ok ? "tone-ok" : "tone-muted"} aria-label={job.worktree?.status || job.status}>{ok ? "✓" : "×"}</span>
      <a data-job-link href={jobsLink(job.id)}>{chain.root.title || chain.root.id}</a>
      <time dateTime={job.finishedAt || job.createdAt}>{relativeTime(job.finishedAt || job.createdAt, now)} ago</time>
    </article>
  );
}

function StationCell({ station, line, rows, now, card }: {
  station: Station;
  line: Line;
  rows: JobChain[];
  now: number;
  card: (chain: JobChain) => ReactNode;
}) {
  const [expanded, setExpanded] = useState(false);
  const shipped = station.key === "completed";
  const shown = shipped && !expanded ? rows.slice(0, 3) : rows;
  const moving = line.jobs.some(job => stationFor(job) === "progress");
  return (
    <div className={`line-cell ${moving ? "belt-moving" : ""}`}>
      <h3 className="phone-line-label">{line.name}</h3>
      {shown.length ? shown.map((chain, index) => (
        <div className="belt-anchor" key={chain.root.id}>
          {index === 0 && <svg className="belt" aria-hidden="true"><line x1="0" y1="50%" x2="100%" y2="50%" /></svg>}
          {shipped ? <ShippedRow chain={chain} now={now} /> : card(chain)}
        </div>
      )) : <div className="belt-anchor">
        <svg className="belt" aria-hidden="true"><line x1="0" y1="50%" x2="100%" y2="50%" /></svg>
        <p className="station-empty">— {station.key === "progress" ? "idle" : station.key === "attention" ? "nothing waiting" : "empty"}</p>
      </div>}
      {shipped && rows.length > 3 && (
        <Button size="sm" variant="ghost" aria-expanded={expanded} onClick={() => setExpanded(!expanded)}>
          {expanded ? "Show less" : `+${rows.length - 3} more`}
        </Button>
      )}
    </div>
  );
}

export function Pipeline({ chains, grouping, card, showAll = false }: {
  chains: JobChain[];
  grouping: LineGrouping;
  card: (chain: JobChain) => ReactNode;
  showAll?: boolean;
}) {
  const [now, setNow] = useState(Date.now);
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
  const lines = groupLines(chains.map(chain => chain.latest), grouping, now);
  const quiet = showAll ? [] : lines.filter(line => !line.active);
  const allExpanded = quiet.length > 0 && quiet.every(line => expanded.has(`${grouping}:${line.name}`));
  useEffect(() => {
    const timer = setInterval(() => { if (!document.hidden) setNow(Date.now()); }, 1000);
    return () => clearInterval(timer);
  }, []);
  return <>
    <LineBoard chains={chains} grouping={grouping} card={card} now={now}
      lines={showAll ? lines : lines.filter(line => line.active)} />
    {quiet.length > 0 && <section className="quiet-lines" aria-label="Quiet lines">
      <div className="quiet-lines-heading">
        <div><h2>Quiet lines · {quiet.length}</h2>
          <p>Nothing running or waiting, last activity 3+ days ago</p></div>
        <Button size="sm" variant="ghost" aria-expanded={allExpanded} onClick={() => {
          setExpanded(allExpanded ? new Set() : new Set(quiet.map(line => `${grouping}:${line.name}`)));
        }}>{allExpanded ? "Hide quiet lines" : "Show quiet lines"}</Button>
      </div>
      {quiet.map(line => {
        const key = `${grouping}:${line.name}`;
        const open = expanded.has(key);
        return <div className="quiet-line" key={key}>
          <Button className="quiet-line-chip" size="sm" variant="ghost" aria-expanded={open}
            onClick={() => setExpanded(previous => {
              const next = new Set(previous);
              if (next.has(key)) next.delete(key); else next.add(key);
              return next;
            })}>
            {line.name} · {relativeTime(line.latestActivity ? new Date(line.latestActivity).toISOString() : "", now)} ago
          </Button>
          {open && <LineBoard chains={chains} grouping={grouping} card={card} now={now} lines={[line]} quiet />}
        </div>;
      })}
    </section>}
  </>;
}

function LineBoard({ chains, grouping, card, lines, now, quiet = false }: {
  chains: JobChain[];
  grouping: LineGrouping;
  card: (chain: JobChain) => ReactNode;
  lines: Line[];
  now: number;
  quiet?: boolean;
}) {
  const scroll = useRef<HTMLDivElement>(null);
  const [active, setActive] = useState(quiet ? 3 : 1);
  const current = useRef(quiet ? 3 : 1);
  const jobs: Job[] = lines.flatMap(line => line.jobs);
  const byId = new Map(chains.map(chain => [chain.latest.id, chain]));

  function go(index: number) {
    current.current = index;
    const board = scroll.current;
    const station = board?.querySelectorAll<HTMLElement>("[data-station]")[index];
    if (board && station) board.scrollTo?.({ left: stationLeft(board, station), behavior: "instant" });
  }
  useLayoutEffect(() => {
    const board = scroll.current;
    if (!board) return;
    const elements = [...board.querySelectorAll<HTMLElement>("[data-station]")];
    const paged = () => getComputedStyle(board.querySelector(".line-rail")!).display === "none";
    let settleUntil = 0;
    const align = () => {
      if (elements[current.current]) board.scrollTo?.({ left: stationLeft(board, elements[current.current]), behavior: "instant" });
    };
    if (paged()) align();
    const update = () => {
      // A viewport resize (rotation, a collapsing address bar) can shift scrollLeft
      // shortly afterwards without the reader moving; keep the paged station.
      if (paged() && performance.now() < settleUntil) { align(); return; }
      const distances = elements.map(station => Math.abs(stationLeft(board, station) - board.scrollLeft));
      current.current = distances.indexOf(Math.min(...distances));
      setActive(current.current);
    };
    const resized = () => {
      settleUntil = performance.now() + 500;
      if (paged()) align();
    };
    board.addEventListener("scroll", update, { passive: true });
    addEventListener("resize", resized);
    // Only width changes move stations; height changes come from job content.
    let width = board.clientWidth;
    const resize = new ResizeObserver(() => {
      if (board.clientWidth !== width) { width = board.clientWidth; resized(); }
    });
    resize.observe(board);
    update();
    return () => {
      board.removeEventListener("scroll", update);
      removeEventListener("resize", resized);
      resize.disconnect();
    };
    // Initialize once; filtering and polling keep the reader's station.
  }, []);

  return (
    <div className="pipeline">
      <div className="station-stepper" aria-label="Pipeline stations">
        {stations.map((station, index) => (
          <button type="button" key={station.key} aria-label={`Go to ${station.name}`}
            aria-current={active === index ? "step" : undefined} className={`tone-${station.tone}`} onClick={() => go(index)}>
            <span>{jobs.filter(job => stationFor(job) === station.key).length}</span>{station.name}
          </button>
        ))}
      </div>
      <div className="pipeline-scroll" ref={scroll} tabIndex={0} aria-label="Production lines, scroll horizontally">
        <div className="pipeline-grid" style={{ "--line-rows": lines.length + 2 } as CSSProperties}>
          <aside className="line-rail">
            <div className="rail-heading"><SectionLabel>Production line</SectionLabel></div>
            {lines.map((line, index) => <LineHeading key={line.name} line={line} grouping={grouping} index={index} />)}
            <div className="throughput-label"><SectionLabel>Throughput · today</SectionLabel></div>
          </aside>
          {stations.map(station => (
            <section className={`pipeline-station tone-${station.tone}`} key={station.key}
              data-station={station.key} tabIndex={-1} aria-label={station.name}>
              <header className="station-heading">
                <h2><span className="station-number">{station.number}</span><i className="status-dot" aria-hidden="true" />
                  {station.name}<span className="count-pill">{jobs.filter(job => stationFor(job) === station.key).length}</span>
                </h2>
                <p>{stationStat(station.key, jobs, now)}</p>
              </header>
              {lines.map(line => {
                const rows = line.jobs.filter(job => stationFor(job) === station.key).map(job => byId.get(job.id)!)
                  .sort((a, b) => (b.latest.finishedAt || b.latest.createdAt).localeCompare(a.latest.finishedAt || a.latest.createdAt));
                return <StationCell key={`${grouping}:${line.name}`} station={station} line={line} rows={rows} now={now} card={card} />;
              })}
              <footer className="station-throughput">
                <span className="phone-throughput-label"><SectionLabel>Throughput · today</SectionLabel></span>
                {stationTodayCount(station.key, jobs, new Date(now))} {station.name.toLowerCase()}
              </footer>
            </section>
          ))}
        </div>
      </div>
      <div className="pager-dots" aria-label="Station pages">
        {stations.map((station, index) => (
          <button type="button" className="pager-dot" key={station.key} aria-label={`${station.name} page`}
            aria-current={active === index ? "step" : undefined} onClick={() => go(index)} />
        ))}
      </div>
      {!quiet && <p className="pipeline-hint"><SectionLabel>⇆ stations · ↕ lines</SectionLabel>
        <span>j/k next job · h/l next station · Enter details</span><span>Belt moves while jobs run</span>
      </p>}
    </div>
  );
}
