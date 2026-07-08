import { useEffect, useRef, useState } from "react";
import type { ProjectMeta, State, PactEvent, RecipeItem } from "./lib/types";
import { fetchEventsLog, getActingSeat, renameRegistry, deleteRegistry, getOrchestrateStatus, getRecipes, getWorktrees } from "./lib/api";
import type { Worktree } from "./lib/api";
import { DataSourceProvider, useDataSource, type DataSource } from "./lib/datasource";
import { isHostedMode, localSource } from "./lib/source";
import { RelayConnect } from "./components/RelayConnect";
import { Toolbar } from "./components/shell/Toolbar";
import { SettingsModal } from "./components/shell/SettingsModal";
import { AddProjectWizard } from "./components/shell/AddProjectWizard";
import { DispatchPanel } from "./components/shell/DispatchPanel";
import { Agents } from "./components/Agents";
import { Board } from "./components/Board";
import { RunRail } from "./components/board/RunRail";
import { EventDrawer } from "./components/board/EventDrawer";
import { RightRail } from "./components/RightRail";
import { TaskDetail } from "./components/TaskDetail";
import { CockpitPanel } from "./components/CockpitPanel";
import { CommandK } from "./components/CommandK";
import { NoProjects } from "./components/NoProjects";
import { Recipes } from "./components/Recipes";
import { Modal } from "./components/ui/Modal";
import { Toasts, diffAwaiting, type Toast } from "./components/Toasts";
import { allTasks } from "./lib/derive";
import { pulseTargets } from "./lib/comms";
import { docTitle } from "./lib/docTitle";

const EMPTY: State = { project: "", agents: [], features: [], awaiting_count: 0 };

// Stale threshold: a task sitting in_progress longer than this (with no further
// state change observed) gets an amber dot. Pragmatic: we time from when this
// dashboard FIRST saw the task in_progress (a per-session timestamp map keyed by
// task id), not from the protocol event ts — good enough to flag a stuck task
// during a live session without parsing the log. Cleared when the task leaves
// in_progress. A 60s ticker re-evaluates the elapsed window.
const STALE_MS = 30 * 60 * 1000;

// Retained-events cap: the SSE stream accumulates for the whole session, so the
// array is trimmed to the most recent EVENTS_CAP after each append (dedup is
// O(1) via the seenEventIds set). Tradeoff: taskRuntimeMs anchors on a task's
// `assign` event, which for an ancient task can fall off the window — such
// long-shipped cards then show runtime 0 (derive's documented no-assign
// fallback), acceptable in exchange for bounded memory and O(1) appends.
const EVENTS_CAP = 2000;

// Consecutive mid-session state-fetch failures before the non-blocking
// "updates interrupted" indicator shows (first-load failures own loadFailed).
const FETCH_FAIL_THRESHOLD = 3;

function AppContent() {
  const src = useDataSource();
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
  // IA v2 shell: ⚙ Settings sheet + AddProjectWizard now owned by App (moved out
  // of the dropped Sidebar). `running` drives the ProjectMenu status light.
  const [settingsOpen, setSettingsOpen] = useState(false);
  // The seat a RosterDock gear was clicked from (null = opened from the toolbar
  // ⚙, no seat focus). Drives SettingsModal's jump-to-project-seats behavior.
  const [settingsSeat, setSettingsSeat] = useState<string | null>(null);
  const openSettings = (seat: string | null) => { setSettingsSeat(seat); setSettingsOpen(true); };
  const [wizardOpen, setWizardOpen] = useState(false);
  const [dispatchOpen, setDispatchOpen] = useState(false);
  // Goal seeded into the DispatchPanel by callers that pre-fill it (none today;
  // the canvas NL dock that used to set this is retired).
  const [dispatchGoal, setDispatchGoal] = useState("");
  // running status per project name → drives the status light on the ProjectMenu
  // trigger AND every row in the dropdown (spec §4.1: each project shows a light).
  const [runningByProject, setRunningByProject] = useState<Record<string, boolean>>({});
  const [author, setAuthor] = useState(false);
  // The acting seat id (empty when observing) — TopBar resolves its roles from
  // state.agents to pick the ant caste for the seat avatar.
  const [seat, setSeat] = useState("");
  // Worktree selection: "" means primary; a non-empty branch name switches the
  // board to that worktree's .pact state (polled, no SSE).
  const [currentWorktree, setCurrentWorktree] = useState("");
  const [worktreesByProject, setWorktreesByProject] = useState<Record<string, Worktree[]>>({});

  const [toasts, setToasts] = useState<Toast[]>([]);
  const [staleTasks, setStaleTasks] = useState<Set<string>>(new Set());
  const [recipes, setRecipes] = useState<RecipeItem[]>([]);
  const [recipeOpen, setRecipeOpen] = useState(false);
  const [cockpitOpen, setCockpitOpen] = useState(false);
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
  // Mirror of `current` for guarding late async responses after a project switch.
  const currentRef = useRef("");
  // task id → epoch ms when first observed in_progress this session.
  const inProgressSince = useRef<Map<string, number>>(new Map());
  // event_id set mirroring `events` appends — makes SSE dedup O(1) instead of
  // an O(n) scan per event. Ids of trimmed events stay in the set (they must:
  // an SSE reconnect can replay an event the cap already evicted).
  const seenEventIds = useRef<Set<string>>(new Set());
  // Mid-session fetchState failure streak (FETCH_FAIL_THRESHOLD consecutive
  // misses → fetchStale indicator; any success clears both).
  const failStreak = useRef(0);
  const [fetchStale, setFetchStale] = useState(false);
  const noteFetchOk = () => { failStreak.current = 0; setFetchStale(false); };
  const noteFetchFail = () => {
    failStreak.current += 1;
    if (failStreak.current >= FETCH_FAIL_THRESHOLD) setFetchStale(true);
  };
  // tick forces stale re-evaluation on an interval even without state changes.
  const [tick, setTick] = useState(0);

  // refreshProjects re-fetches the registry-backed project list, seeding the
  // selection on first load and dropping it if the current project was removed.
  // projectsLoaded gates the empty-registry hero: only a CONFIRMED-empty
  // registry shows it (never the pre-fetch window, never a failed fetch).
  function refreshProjects() {
    src.listProjects().then((ps) => {
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
    // Hosted has no /api/acting-seat (that endpoint is local-serve only); its
    // authority comes from the relay source's capabilities — it drives via the
    // remote-control down-channel as the account, with no per-project seat. So
    // deriving author from getActingSeat() left every hosted dashboard stuck in
    // "observing". Use the capability flag instead.
    if (isHostedMode()) {
      setAuthor(src.capabilities.canWrite);
      setSeat("");
      return;
    }
    // A non-empty acting seat means this local dashboard can author.
    getActingSeat().then((r) => {
      setAuthor(!!(r?.seat));
      setSeat(r?.seat ?? "");
    }).catch(() => { setAuthor(false); setSeat(""); });
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // Fetch recipes once on mount; feed into the ⌘K Templates group.
  useEffect(() => {
    getRecipes().then(setRecipes).catch(() => setRecipes([]));
  }, []);

  // Re-evaluate stale tasks on a 60s ticker (and whenever state changes).
  useEffect(() => {
    const t = setInterval(() => setTick((n) => n + 1), 60_000);
    return () => clearInterval(t);
  }, []);

  // Fetch worktrees for all known projects whenever the project list changes.
  const projectNamesForWt = projects.map((p) => p.name).join(",");
  useEffect(() => {
    const names = projectNamesForWt ? projectNamesForWt.split(",") : [];
    let alive = true;
    Promise.all(names.map((n) => getWorktrees(n).then((w) => [n, w] as const).catch(() => [n, []] as const)))
      .then((entries) => { if (alive) setWorktreesByProject(Object.fromEntries(entries)); });
    return () => { alive = false; };
  }, [projectNamesForWt]);

  // ProjectMenu status light: poll orchestrate status for the current project so
  // the header dot pulses while a run is active (present, not done/escalated).
  const projectNames = projects.map((p) => p.name).join(",");
  useEffect(() => {
    const names = projectNames ? projectNames.split(",") : [];
    if (names.length === 0) { setRunningByProject({}); return; }
    let alive = true;
    const tick = async () => {
      const entries = await Promise.all(
        names.map((name) =>
          getOrchestrateStatus(name)
            .then((s) => [name, Boolean(s.present && s.status && !s.status.done && !s.status.escalated)] as const)
            .catch(() => [name, false] as const),
        ),
      );
      if (alive) setRunningByProject(Object.fromEntries(entries));
    };
    tick();
    const t = setInterval(tick, 4000);
    return () => { alive = false; clearInterval(t); };
  }, [projectNames]);

  // Project rename/delete (moved from Sidebar into the header ProjectMenu).
  const onRenameProject = async (name: string) => {
    const next = window.prompt(`Rename "${name}" to:`, name);
    if (!next || next === name) return;
    try { await renameRegistry(name, next); } catch (e) { pushToast(e instanceof Error ? e.message : String(e), "error"); return; }
    if (current === name) setCurrent(next);
    refreshProjects();
  };
  const onDeleteProject = async (name: string) => {
    if (!window.confirm(`Unregister "${name}"? (.pact files are kept)`)) return;
    try { await deleteRegistry(name); } catch (e) { pushToast(e instanceof Error ? e.message : String(e), "error"); return; }
    refreshProjects();
  };

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
    seenEventIds.current = new Set();
    failStreak.current = 0;
    setFetchStale(false);
    prevState.current = EMPTY;
    inProgressSince.current = new Map();
    setStaleTasks(new Set());
    setPulses(new Set());
    // Selection is per-project: a stale id from the previous project would
    // leave the detail panel's listeners half-armed against a missing task.
    setSelected("");
    if (pulseTimer.current) clearTimeout(pulseTimer.current);
    // Clear the displayed snapshot IMMEDIATELY: rendering the previous
    // project's state under the new project id flashes stale data.
    setState(EMPTY);
    setLoadFailed(false);
    const wt = currentWorktree || undefined;
    src.getState(current, wt)
      .then((s) => { if (alive) { noteFetchOk(); applyState(s); } })
      .catch(() => { if (alive) { noteFetchFail(); setState(EMPTY); setLoadFailed(true); } });
    if (!currentWorktree) {
      const off = src.subscribe(
        current,
        (s) => { if (alive) { noteFetchOk(); applyState(s); } },
        (e) => {
          if (!alive) return;
          // Dedupe by event_id: the SSE backfill replays the log tail, which can
          // overlap a live event that raced in between subscribe and replay.
          // Checked against the seen-set OUTSIDE the updater (O(1), and keeps the
          // updater pure), then trimmed to the EVENTS_CAP most recent.
          if (!seenEventIds.current.has(e.event_id)) {
            seenEventIds.current.add(e.event_id);
            setEvents((prev) => {
              const next = [...prev, e];
              return next.length > EVENTS_CAP ? next.slice(-EVENTS_CAP) : next;
            });
          }
        },
        () => { if (alive) noteFetchFail(); },
        (v) => { if (alive) setLive(v); },
      );
      return () => {
        alive = false;
        off();
        setLive(false);
        if (pulseTimer.current) clearTimeout(pulseTimer.current);
      };
    }
    // non-primary worktree: poll every 3s, no SSE. Events come from the REST
    // log endpoint (same objects as the SSE `pact` frames) so task metrics
    // aren't dead in worktree views; the response is already recency-capped
    // server-side, so it replaces `events` wholesale.
    setLive(false);
    const fetchLog = src.fetchEventsLog?.bind(src) ?? fetchEventsLog;
    const pollEvents = () =>
      fetchLog(current, currentWorktree)
        .then((evs) => { if (alive) setEvents(evs); })
        .catch(() => {});
    pollEvents();
    const poll = setInterval(() => {
      src.getState(current, currentWorktree)
        .then((s) => { if (alive) { noteFetchOk(); applyState(s); } })
        .catch(() => { if (alive) noteFetchFail(); });
      pollEvents();
    }, 3000);
    return () => { alive = false; clearInterval(poll); if (pulseTimer.current) clearTimeout(pulseTimer.current); };
  }, [current, currentWorktree]); // eslint-disable-line react-hooks/exhaustive-deps

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

  const shownState = state;

  // First-load skeleton signal (T15): a project is current but its very first
  // live snapshot hasn't been applied yet (state is still the EMPTY sentinel).
  // Only when there ARE projects (the no-project hero owns the empty-registry case).
  const firstLoad = !!current && state === EMPTY && !loadFailed;

  // Dynamic document title (spec §6.5): «project» · N awaiting ●. Driven from
  // the currently displayed state's awaiting count.
  useEffect(() => {
    const name = projects.find((p) => p.id === current)?.name ?? current;
    document.title = docTitle(name, shownState.awaiting_count);
  }, [projects, current, shownState.awaiting_count]);

  const currentName = projects.find((p) => p.id === current)?.name ?? current;
  const orchestratorSeat = shownState.agents.find((a) => a.roles.includes("orchestrator"))?.id ?? seat ?? "claude";
  return (
    <div data-testid="app-root" className="h-screen flex flex-col">
      <Toolbar
        projectName={currentName}
        live={live}
        author={author}
        seat={seat}
        agents={shownState.agents}
        projects={projects}
        running={!!runningByProject[current]}
        runningByProject={runningByProject}
        onSelectProject={(name) => { setCurrent(name); setCurrentWorktree(""); }}
        onRenameProject={onRenameProject}
        onDeleteProject={onDeleteProject}
        onAddProject={() => setWizardOpen(true)}
        onOpenSettings={() => openSettings(null)}
        onOpenDispatch={() => setDispatchOpen(true)}
        showCockpit={Boolean(current) && src.capabilities.canOrchestrate && !!src.cockpitStreamUrl}
        onToggleCockpit={() => setCockpitOpen((v) => !v)}
        worktreesByProject={worktreesByProject}
        currentWorktree={currentWorktree}
        onSelectWorktree={(name, branch) => { setCurrent(name); setCurrentWorktree(branch); }}
      />
      <div className="relative flex flex-1 overflow-hidden">
        {/* The dark-handoff Board carries its seated cluster in its own context
            header (Board.tsx), so the old floating left dock is gone — the board
            is full-width with no permanent left column. */}
        <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
          <Agents author={author} onChanged={refreshProjects} />
          {projectsLoaded && projects.length === 0
            ? <NoProjects onRegistered={refreshProjects} />
            : (
              <>
                {/* Run rail: the orchestrate banner (renders nothing when the
                    driver is idle) — RunControl strip + driver-touched feature
                    lanes + the five-action ReviewGate on a paused gate. */}
                <RunRail project={current} state={shownState} refreshTick={refreshTick} author={author} events={events} onNotify={(msg, kind) => pushToast(msg, kind)} />
                {/* relative so the slide-over detail panel + its scrim position
                    within this row, overlaying the board. The board takes the
                    full width — the panel is absolute. */}
                <div className="relative flex flex-1 overflow-hidden">
                  <div data-testid="view-board" className="flex flex-1 overflow-hidden"><Board state={shownState} events={events} selected={selected} onSelect={setSelected} pulses={pulses} staleTasks={staleTasks} loading={firstLoad} project={current} author={author} onChanged={() => setRefreshTick((t) => t + 1)} /></div>
                  {src.capabilities.multiMachine
                    ? <TaskDetail project={current} taskId={selected} onClose={() => setSelected("")} />
                    : <RightRail state={shownState} events={events} selected={selected} project={current} author={author} onSelect={setSelected} />}
                  {cockpitOpen && current && src.capabilities.canOrchestrate && src.cockpitStreamUrl && (
                    <CockpitPanel project={current} seat={orchestratorSeat} onClose={() => setCockpitOpen(false)} />
                  )}
                </div>
                {/* Event drawer: collapsed one-line ticker of the pact log —
                    expands to the full colorized terminal + seat presence. */}
                <EventDrawer events={events} agents={shownState.agents} state={shownState} />
              </>
            )}
        </div>
      </div>
      <Toasts toasts={toasts} />
      {/* Persistent (not a 5s toast: it must outlive the outage) non-blocking
          indicator for a mid-session refresh outage; toast-styled, bottom-left
          so it doesn't stack with the toast rail. Cleared on the next success. */}
      {fetchStale && (
        <div
          data-testid="fetch-stale"
          role="status"
          className="pointer-events-none fixed bottom-4 left-4 z-[60] rounded-md border border-[var(--color-border-subtle)] border-l-2 border-l-[var(--color-danger)] bg-[var(--color-bg-raised)] px-3 py-2 text-xs text-[var(--color-text-1)] shadow-[var(--shadow-raised)]"
        >
          Live updates interrupted — retrying…
        </div>
      )}
      {settingsOpen && <SettingsModal project={current} author={author} focusSeat={settingsSeat} onClose={() => setSettingsOpen(false)} />}
      <AddProjectWizard open={wizardOpen} onClose={() => setWizardOpen(false)} onAdded={() => { setWizardOpen(false); refreshProjects(); }} />
      <DispatchPanel
        project={current}
        roster={shownState.agents}
        open={dispatchOpen}
        onClose={() => { setDispatchOpen(false); setDispatchGoal(""); }}
        onGoLive={() => setDispatchOpen(false)}
        initialGoal={dispatchGoal}
      />
      <CommandK
        projects={projects}
        current={current}
        state={shownState}
        setSelected={setSelected}
        onSelectProject={setCurrent}
        author={author}
        replaying={false}
        notify={(text) => pushToast(text, "error")}
        recipes={recipes}
        onRunRecipe={() => setRecipeOpen(true)}
      />
      {recipeOpen && (
        <Modal testId="recipes-modal" title="Generate from template" width="720px" onClose={() => setRecipeOpen(false)}>
          <Recipes />
        </Modal>
      )}
    </div>
  );
}

export default function App() {
  // Local build → the co-located serve source is ready immediately. Hosted build
  // → start with no source and gate on RelayConnect until the user supplies the
  // master secret; then render the same dashboard against the RelaySource.
  const [source, setSource] = useState<DataSource | null>(() =>
    isHostedMode() ? null : localSource(),
  );
  if (!source) {
    return <RelayConnect onConnected={setSource} />;
  }
  return (
    <DataSourceProvider source={source}>
      <AppContent />
    </DataSourceProvider>
  );
}
