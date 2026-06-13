import { useEffect, useRef, useState } from "react";
import type { ProjectMeta, State, PactEvent } from "./lib/types";
import { fetchProjects, fetchState, subscribeEvents, getActingSeat } from "./lib/api";
import { TopBar, type View } from "./components/TopBar";
import { Agents } from "./components/Agents";
import { Board } from "./components/Board";
import { Canvas } from "./components/Canvas";
import { LiveOrchestrate } from "./components/LiveOrchestrate";
import { OpsView } from "./components/ops/OpsView";
import { ReplayBar } from "./components/ReplayBar";
import { RightRail } from "./components/RightRail";
import { CommandK } from "./components/CommandK";
import { NoProjects } from "./components/NoProjects";
import { Toasts, diffAwaiting, type Toast } from "./components/Toasts";
import { allTasks } from "./lib/derive";
import { pulseTargets } from "./lib/comms";
import { docTitle } from "./lib/docTitle";
import { readAt, writeAt } from "./lib/replayUrl";
import type { Draft, DraftFeature } from "./lib/canvas";

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
  const [projectsLoaded, setProjectsLoaded] = useState(false);
  // A failed first state fetch must NOT read as "still loading" — it suppresses
  // the skeleton so the honest empty board shows instead. Reset per project.
  const [loadFailed, setLoadFailed] = useState(false);
  const [current, setCurrent] = useState("");
  const [state, setState] = useState<State>(EMPTY);
  const [events, setEvents] = useState<PactEvent[]>([]);
  const [selected, setSelected] = useState("");
  const [live, setLive] = useState(false);
  const [view, setView] = useState<View>("kanban");
  const [author, setAuthor] = useState(false);
  // The acting seat id (empty when observing) — TopBar resolves its roles from
  // state.agents to pick the ant caste for the seat avatar.
  const [seat, setSeat] = useState("");
  // Canvas build-mode drafts live HERE (not inside Canvas) so they survive the
  // Canvas unmount that view switching causes — switching canvas→ops→canvas
  // would otherwise wipe every in-flight draft. Reset on project switch (the
  // [current] effect below), since drafts are scoped to one project's canvas.
  const [drafts, setDrafts] = useState<Draft[]>([]);
  const [draftFeatures, setDraftFeatures] = useState<DraftFeature[]>([]);
  // Replay (M3.3b): replayAt is the scrubber position (null = live). When set,
  // replayState holds the fetched HISTORICAL snapshot, which takes display
  // precedence over the live `state`. Historical snapshots never pass through
  // applyState (so the firstSnapshot toast/pulse guard stays live-only) — they
  // render directly via replayState.
  const [replayAt, setReplayAt] = useState<number | null>(null);
  // null until the first historical snapshot arrives — the display falls back to
  // the live state meanwhile, so entering replay never flashes an empty board.
  const [replayState, setReplayState] = useState<State | null>(null);
  const replaying = replayAt !== null;
  const [toasts, setToasts] = useState<Toast[]>([]);
  const [staleTasks, setStaleTasks] = useState<Set<string>>(new Set());
  // Live pulse (M3.3b C4): task ids whose status changed on the latest applied
  // LIVE snapshot. Canvas/Board apply a transient `pulse` class; the set is
  // cleared after the keyframe duration so the glow plays exactly once. Replay
  // snapshots never reach applyState, so this stays live-only.
  const [pulses, setPulses] = useState<Set<string>>(new Set());
  const pulseTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Monotonic tick bumped on every applied state snapshot; passed to the ops
  // panels so their fetches re-run on SSE updates for the selected project.
  const [refreshTick, setRefreshTick] = useState(0);

  const prevState = useRef<State>(EMPTY);
  const toastId = useRef(0);
  // Mirror of `replayAt` readable inside callbacks that close over stale state
  // (the SSE handler, and the stale-response guard in showReplaySnapshot). While
  // replaying we still append to the event log but do NOT apply snapshots — the
  // historical replayState owns the display. Written SYNCHRONOUSLY in
  // enterReplay/resumeLive (not via an effect) so an SSE event landing between
  // the state update and the next render can't slip a toast through.
  const replayAtRef = useRef<number | null>(null);
  // Mirror of `current` for guarding late async responses after a project switch.
  const currentRef = useRef("");
  // Replay deep link (spec §6.6): the `?at=N` value read ONCE on mount, applied
  // as enterReplay(N) the moment the first project becomes current (ReplayBar
  // clamps N to the timeline bounds). Cleared after it fires so a later project
  // switch doesn't re-trigger it.
  const pendingAt = useRef<number | null>(readAt(window.location.search));
  // Debounce handle for `?at` URL writes during scrubs (see enterReplay).
  const urlTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  // task id → epoch ms when first observed in_progress this session.
  const inProgressSince = useRef<Map<string, number>>(new Map());
  // tick forces stale re-evaluation on an interval even without state changes.
  const [tick, setTick] = useState(0);

  // refreshProjects re-fetches the registry-backed project list, seeding the
  // selection on first load and dropping it if the current project was removed.
  // projectsLoaded gates the empty-registry hero: only a CONFIRMED-empty
  // registry shows it (never the pre-fetch window, never a failed fetch).
  function refreshProjects() {
    fetchProjects().then((ps) => {
      setProjects(ps);
      setProjectsLoaded(true);
      setCurrent((cur) => {
        if (cur && ps.some((p) => p.id === cur)) return cur;
        return ps.length ? ps[0].id : "";
      });
    }).catch(() => {});
  }

  useEffect(() => {
    refreshProjects();
    // A non-empty acting seat means this dashboard can author.
    getActingSeat().then((r) => {
      setAuthor(!!(r?.seat));
      setSeat(r?.seat ?? "");
    }).catch(() => { setAuthor(false); setSeat(""); });
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // Global view shortcuts: 1/2/3 switch kanban/canvas/ops. Ignored while typing
  // (input/textarea/select or contentEditable) or while a modal/dialog is open.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const t = e.target as HTMLElement | null;
      const tag = t?.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || t?.isContentEditable) return;
      if (document.querySelector('[role="dialog"]')) return;
      if (e.key === "1") setView("kanban");
      else if (e.key === "2") setView("canvas");
      else if (e.key === "3") setView("ops");
      else if (e.key === "4") setView("live");
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // Re-evaluate stale tasks on a 60s ticker (and whenever state changes).
  useEffect(() => {
    const t = setInterval(() => setTick((n) => n + 1), 60_000);
    return () => clearInterval(t);
  }, []);

  // pushToast surfaces a one-off message on the shared toast stack (review
  // notifications and palette-action failures ride the same rail). `kind:
  // "error"` renders the danger-tinted variant for failed author actions.
  function pushToast(text: string, kind?: "error") {
    setToasts((prev) => {
      toastId.current += 1;
      const tid = toastId.current;
      setTimeout(() => setToasts((cur) => cur.filter((x) => x.id !== tid)), 5000);
      return [...prev, { id: tid, text, kind }].slice(-3);
    });
  }

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

    // 3) live pulse — task ids whose status changed since the previous live
    // snapshot. Computed BEFORE prevState is updated. The first snapshot must
    // NOT pulse: prev here is the EMPTY sentinel (not null), so pulseTargets
    // would flag every task as "newly appearing" — gate on firstSnapshot.
    if (!firstSnapshot) {
      const { taskIds } = pulseTargets(prevState.current, s);
      if (taskIds.length) {
        setPulses(new Set(taskIds));
        if (pulseTimer.current) clearTimeout(pulseTimer.current);
        // Timeout fallback clears the class after the ~900ms keyframe so the
        // glow plays once (animationend on the node also works in the browser;
        // this guarantees cleanup under jsdom and prefers-reduced-motion).
        pulseTimer.current = setTimeout(() => setPulses(new Set()), 950);
      }
    }

    prevState.current = s;
    setState(s);
    setRefreshTick((n) => n + 1);
  }

  useEffect(() => {
    if (!current) return;
    let alive = true;
    currentRef.current = current;
    setEvents([]);
    prevState.current = EMPTY;
    inProgressSince.current = new Map();
    setStaleTasks(new Set());
    setPulses(new Set());
    // Selection is per-project: a stale id from the previous project would
    // leave the detail panel's listeners half-armed against a missing task.
    setSelected("");
    // Drafts are scoped to one project's canvas — drop them on project switch.
    setDrafts([]);
    setDraftFeatures([]);
    if (pulseTimer.current) clearTimeout(pulseTimer.current);
    // Exit replay on project switch — the new project starts live, UNLESS a
    // pending `?at` deep link is waiting to be applied to the FIRST project that
    // becomes current (consumed once so later switches start live as normal).
    const at = pendingAt.current;
    pendingAt.current = null;
    replayAtRef.current = at;
    setReplayAt(at);
    setReplayState(null);
    // Clear the displayed snapshot IMMEDIATELY: rendering the previous
    // project's state under the new project id both flashes stale data and
    // made Canvas's FitOnEntry frame the OLD graph (then never refit).
    setState(EMPTY);
    setLoadFailed(false);
    fetchState(current)
      .then((s) => { if (alive) applyState(s); })
      .catch(() => { if (alive) { setState(EMPTY); setLoadFailed(true); } });
    const off = subscribeEvents(current, (e) => {
      if (!alive) return;
      setEvents((prev) => [...prev, e]);
      // While replaying, SSE-applied snapshots are IGNORED (the displayed snapshot
      // is the fetched historical one). We keep the subscription open but skip the
      // refetch+apply so toasts/pulse stay live-only.
      if (replayAtRef.current !== null) return;
      fetchState(current).then((s) => { if (alive) applyState(s); }).catch(() => {});
    }, (v) => { if (alive) setLive(v); });
    return () => {
      alive = false;
      off();
      setLive(false);
      if (pulseTimer.current) clearTimeout(pulseTimer.current);
    };
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

  // --- Replay handlers (M3.3b) ----------------------------------------------
  // ReplayBar owns the debounced getStateAt fetch; App just records position
  // (onEnter) and parks the fetched historical snapshot (onSnapshot) which takes
  // display precedence over the live state. Historical snapshots never pass
  // through applyState, so the firstSnapshot toast/pulse guard stays live-only.
  function enterReplay(at: number) {
    replayAtRef.current = at;
    setReplayAt(at);
    // Reflect the scrub position in the URL (spec §6.6) without a history entry,
    // so the view is shareable/reloadable. ReplayBar stays URL-agnostic — App
    // owns the `?at` param. A pending deep-link read is now consumed.
    pendingAt.current = null;
    // DEBOUNCED: a pointer drag calls enterReplay per move; WebKit caps
    // replaceState at ~100 calls / 30s (SecurityError beyond) — write the URL
    // at the same cadence as the snapshot fetch instead of per-move.
    if (urlTimer.current) clearTimeout(urlTimer.current);
    urlTimer.current = setTimeout(() => {
      const next = writeAt(window.location.search, at);
      if (next !== window.location.search) {
        window.history.replaceState(null, "", `${window.location.pathname}${next}${window.location.hash}`);
      }
    }, 150);
  }
  function showReplaySnapshot(at: number, s: State) {
    // Drop stale responses: a slow fetch for an old position must not override
    // the snapshot for where the scrubber is now (or live mode).
    if (at !== replayAtRef.current) return;
    setReplayState(s);
  }

  // Return to live: drop the historical snapshot, refetch fresh live state (do
  // NOT trust the last SSE-applied state), and resume applying SSE.
  function resumeLive() {
    replayAtRef.current = null;
    setReplayAt(null);
    setReplayState(null);
    // Clear the `?at` deep link on return to live (spec §6.6) — immediately,
    // cancelling any debounced scrub write still in flight.
    if (urlTimer.current) clearTimeout(urlTimer.current);
    const next = writeAt(window.location.search, null);
    if (next !== window.location.search) {
      window.history.replaceState(null, "", `${window.location.pathname}${next}${window.location.hash}`);
    }
    const p = current;
    fetchState(p)
      .then((s) => { if (p === currentRef.current) applyState(s); })
      .catch(() => {});
  }

  // Snapshot shown by kanban/canvas: historical while replaying (falling back to
  // the live state until the first replay snapshot lands), else live.
  const shownState = replaying ? (replayState ?? state) : state;

  // First-load skeleton signal (T15): a project is current but its very first
  // live snapshot hasn't been applied yet (state is still the EMPTY sentinel).
  // Not while replaying (replay has its own fallback) and only when there ARE
  // projects (the no-project hero owns the empty-registry case).
  const firstLoad = !!current && state === EMPTY && !replaying && !loadFailed;

  // Dynamic document title (spec §6.5): «project» · N awaiting ●. Driven from
  // the currently displayed (live or replay) state's awaiting count.
  useEffect(() => {
    const name = projects.find((p) => p.id === current)?.name ?? current;
    document.title = docTitle(name, shownState.awaiting_count);
  }, [projects, current, shownState.awaiting_count]);

  return (
    <div data-testid="app-root" className="h-screen flex flex-col">
      <TopBar projects={projects} current={current} onSelect={setCurrent} live={live} replaying={replaying} view={view} onView={setView} author={author} seat={seat} agents={shownState.agents} />
      <Agents author={author} onChanged={refreshProjects} />
      {projectsLoaded && projects.length === 0
        ? <NoProjects onRegistered={refreshProjects} />
        : view === "ops"
        ? <OpsView project={current} author={author} refreshTick={refreshTick} onRegistryChanged={refreshProjects} loading={firstLoad} />
        : view === "live"
        ? <LiveOrchestrate project={current} refreshTick={refreshTick} />
        : (
          <>
            {/* relative so the slide-over detail panel + its scrim position
                within this row, overlaying kanban/canvas (not ops). The board
                and canvas now take the full width — the panel is absolute. */}
            <div className="relative flex flex-1 overflow-hidden">
              {view === "canvas"
                ? <Canvas project={current} state={shownState} author={author && !replaying} replaying={replaying} staleTasks={staleTasks} pulses={replaying ? undefined : pulses} onSelectTask={setSelected} drafts={drafts} setDrafts={setDrafts} draftFeatures={draftFeatures} setDraftFeatures={setDraftFeatures} loading={firstLoad} />
                : <Board state={shownState} selected={selected} onSelect={setSelected} pulses={replaying ? undefined : pulses} staleTasks={staleTasks} loading={firstLoad} />}
              <RightRail state={shownState} events={events} selected={selected} project={current} author={author && !replaying} onSelect={setSelected} />
            </div>
            <ReplayBar project={current} replayAt={replayAt} refreshTick={refreshTick} onEnter={enterReplay} onSnapshot={showReplaySnapshot} onLive={resumeLive} />
          </>
        )}
      <Toasts toasts={toasts} />
      <CommandK
        projects={projects}
        current={current}
        state={shownState}
        view={view}
        setView={setView}
        setSelected={setSelected}
        onSelectProject={setCurrent}
        author={author}
        replaying={replaying}
        notify={(text) => pushToast(text, "error")}
      />
    </div>
  );
}
