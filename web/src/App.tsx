import { useEffect, useRef, useState } from "react";
import type { ProjectMeta, State, PactEvent } from "./lib/types";
import { fetchProjects, fetchState, subscribeEvents, getActingSeat } from "./lib/api";
import { TopBar, type View } from "./components/TopBar";
import { Agents } from "./components/Agents";
import { Board } from "./components/Board";
import { Canvas } from "./components/Canvas";
import { RightRail } from "./components/RightRail";
import { Toasts, diffAwaiting, type Toast } from "./components/Toasts";
import { allTasks } from "./lib/derive";

const EMPTY: State = { project: "", agents: [], features: [], awaiting_count: 0 };

// Stale threshold: a task sitting in_progress longer than this (with no further
// state change observed) gets an amber dot. Pragmatic: we time from when this
// dashboard FIRST saw the task in_progress (a per-session timestamp map keyed by
// task id), not from the protocol event ts — good enough to flag a stuck task
// during a live session without parsing the log. Cleared when the task leaves
// in_progress. A 60s ticker re-evaluates the elapsed window.
const STALE_MS = 30 * 60 * 1000;

export default function App() {
  const [projects, setProjects] = useState<ProjectMeta[]>([]);
  const [current, setCurrent] = useState("");
  const [state, setState] = useState<State>(EMPTY);
  const [events, setEvents] = useState<PactEvent[]>([]);
  const [selected, setSelected] = useState("");
  const [live, setLive] = useState(false);
  const [view, setView] = useState<View>("kanban");
  const [author, setAuthor] = useState(false);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const [staleTasks, setStaleTasks] = useState<Set<string>>(new Set());

  const prevState = useRef<State>(EMPTY);
  const toastId = useRef(0);
  // task id → epoch ms when first observed in_progress this session.
  const inProgressSince = useRef<Map<string, number>>(new Map());
  // tick forces stale re-evaluation on an interval even without state changes.
  const [tick, setTick] = useState(0);

  useEffect(() => {
    fetchProjects().then((ps) => {
      setProjects(ps);
      if (ps.length && !current) setCurrent(ps[0].id);
    }).catch(() => {});
    // A non-empty acting seat means this dashboard can author.
    getActingSeat().then((r) => setAuthor(!!(r?.seat))).catch(() => setAuthor(false));
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // Re-evaluate stale tasks on a 60s ticker (and whenever state changes).
  useEffect(() => {
    const t = setInterval(() => setTick((n) => n + 1), 60_000);
    return () => clearInterval(t);
  }, []);

  // applyState centralizes a fresh snapshot: diff for review toasts, maintain
  // the in_progress timestamp map, then commit the new state.
  function applyState(s: State) {
    // 1) review toasts — tasks newly entering awaiting_review.
    // First snapshot after load/project-switch seeds the baseline silently:
    // tasks ALREADY awaiting on arrival must not toast-spam.
    const firstSnapshot = prevState.current === EMPTY;
    const ready = firstSnapshot ? [] : diffAwaiting(prevState.current, s);
    if (ready.length) {
      setToasts((prev) => {
        const next = [...prev];
        for (const id of ready) {
          toastId.current += 1;
          const tid = toastId.current;
          next.push({ id: tid, text: `${id} ready for review` });
          setTimeout(() => setToasts((cur) => cur.filter((x) => x.id !== tid)), 5000);
        }
        return next.slice(-3); // cap at 3
      });
    }
    // 2) in_progress timestamp map: stamp on entry, clear on exit.
    const now = Date.now();
    const m = inProgressSince.current;
    const liveInProgress = new Set<string>();
    for (const b of allTasks(s)) {
      if (b.task.status === "in_progress") {
        liveInProgress.add(b.task.id);
        if (!m.has(b.task.id)) m.set(b.task.id, now);
      }
    }
    for (const id of [...m.keys()]) if (!liveInProgress.has(id)) m.delete(id);

    prevState.current = s;
    setState(s);
  }

  useEffect(() => {
    if (!current) return;
    let alive = true;
    setEvents([]);
    prevState.current = EMPTY;
    inProgressSince.current = new Map();
    setStaleTasks(new Set());
    fetchState(current).then((s) => { if (alive) applyState(s); }).catch(() => { if (alive) setState(EMPTY); });
    const off = subscribeEvents(current, (e) => {
      if (!alive) return;
      setEvents((prev) => [...prev, e]);
      fetchState(current).then((s) => { if (alive) applyState(s); }).catch(() => {});
    }, (v) => { if (alive) setLive(v); });
    return () => { alive = false; off(); setLive(false); };
  }, [current]); // eslint-disable-line react-hooks/exhaustive-deps

  // Recompute the stale set from the timestamp map whenever state or tick moves.
  useEffect(() => {
    const now = Date.now();
    const next = new Set<string>();
    for (const [id, since] of inProgressSince.current) {
      if (now - since >= STALE_MS) next.add(id);
    }
    setStaleTasks((prev) => {
      if (prev.size === next.size && [...next].every((x) => prev.has(x))) return prev;
      return next;
    });
  }, [state, tick]);

  return (
    <div data-testid="app-root" className="h-screen flex flex-col">
      <TopBar projects={projects} current={current} onSelect={setCurrent} live={live} view={view} onView={setView} />
      <Agents state={state} events={events} onPick={() => {}} />
      <div className="flex flex-1 overflow-hidden">
        {view === "canvas"
          ? <Canvas project={current} state={state} author={author} staleTasks={staleTasks} />
          : <Board state={state} selected={selected} onSelect={setSelected} />}
        <RightRail state={state} events={events} selected={selected} project={current} author={author} />
      </div>
      <Toasts toasts={toasts} />
    </div>
  );
}
