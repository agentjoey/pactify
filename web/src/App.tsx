import { useEffect, useState } from "react";
import type { ProjectMeta, State, PactEvent } from "./lib/types";
import { fetchProjects, fetchState, subscribeEvents, getActingSeat } from "./lib/api";
import { TopBar, type View } from "./components/TopBar";
import { Agents } from "./components/Agents";
import { Board } from "./components/Board";
import { Canvas } from "./components/Canvas";
import { RightRail } from "./components/RightRail";

const EMPTY: State = { project: "", agents: [], features: [], awaiting_count: 0 };

export default function App() {
  const [projects, setProjects] = useState<ProjectMeta[]>([]);
  const [current, setCurrent] = useState("");
  const [state, setState] = useState<State>(EMPTY);
  const [events, setEvents] = useState<PactEvent[]>([]);
  const [selected, setSelected] = useState("");
  const [live, setLive] = useState(false);
  const [view, setView] = useState<View>("kanban");
  const [author, setAuthor] = useState(false);

  useEffect(() => {
    fetchProjects().then((ps) => {
      setProjects(ps);
      if (ps.length && !current) setCurrent(ps[0].id);
    }).catch(() => {});
    // A non-empty acting seat means this dashboard can author (gates UI; full
    // author interactions land in Q7/Q8).
    getActingSeat().then((r) => setAuthor(!!(r?.seat))).catch(() => setAuthor(false));
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!current) return;
    let alive = true;
    setEvents([]);
    fetchState(current).then((s) => { if (alive) setState(s); }).catch(() => { if (alive) setState(EMPTY); });
    const off = subscribeEvents(current, (e) => {
      if (!alive) return;
      setEvents((prev) => [...prev, e]);
      fetchState(current).then((s) => { if (alive) setState(s); }).catch(() => {});
    }, (v) => { if (alive) setLive(v); });
    return () => { alive = false; off(); setLive(false); };
  }, [current]);

  return (
    <div data-testid="app-root" className="h-screen flex flex-col">
      <TopBar projects={projects} current={current} onSelect={setCurrent} live={live} view={view} onView={setView} />
      <Agents state={state} events={events} onPick={() => {}} />
      <div className="flex flex-1 overflow-hidden">
        {view === "canvas"
          ? <Canvas project={current} state={state} author={author} />
          : <Board state={state} selected={selected} onSelect={setSelected} />}
        <RightRail state={state} events={events} selected={selected} />
      </div>
    </div>
  );
}
