import { useEffect, useRef, useState } from "react";
import type { ProjectMeta, State, PactEvent, RecipeItem } from "./lib/types";
import { fetchEventsLog, getActingSeat, renameRegistry, deleteRegistry, getOrchestrateStatus, getRecipes, getWorktrees } from "./lib/api";
import type { Worktree } from "./lib/api";
import { DataSourceProvider, useDataSource, type DataSource } from "./lib/datasource";
import { isHostedMode, localSource } from "./lib/source";
import { IdentityGate } from "./components/IdentityGate";
import { UnlockPanel } from "./components/UnlockPanel";
import { Toolbar } from "./components/shell/Toolbar";
import { fetchMe, type MeResponse } from "./lib/identity";
import { SettingsModal } from "./components/shell/SettingsModal";
import { AddProjectWizard } from "./components/shell/AddProjectWizard";
import { DispatchPanel } from "./components/shell/DispatchPanel";
import { Agents } from "./components/Agents";
import { Board } from "./components/Board";
import { FlowView } from "./components/flow/FlowView";
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
import { STALE_MS, EVENTS_CAP, FETCH_FAIL_THRESHOLD, COCKPIT_STATUS_POLL_MS } from "./lib/constants";

const EMPTY: State = { project: "", agents: [], features: [], awaiting_count: 0 };
type BoardMode = "board" | "flow";
export type Lens = "dashboard" | "board" | "cockpit" | "settings";

const LENS_STORAGE_KEY = "pactify:lens";
const BOARD_MODE_STORAGE_KEY = "pactify:boardMode";
const LAST_PROJECT_STORAGE_KEY = "pactify:lastProject";

function readSavedLens(): Lens {
  if (typeof localStorage === "undefined") return "board";
  const raw = localStorage.getItem(LENS_STORAGE_KEY);
  if (raw === "dashboard" || raw === "board" || raw === "cockpit" || raw === "settings") return raw;
  return "board";
}

// pickInitialProject resolves the default project id from the loaded list and
// the user's last selection. Preference order: 1) stored id still present,
// 2) first project whose backend `project` field is not "unknown",
// 3) fall back to the first entry (or "" when the list is empty).
export function pickInitialProject(ps: ProjectMeta[], stored?: string | null): string {
  if (stored && ps.some((p) => p.id === stored)) return stored;
  const alive = ps.find((p) => p.project !== "unknown");
  return alive?.id ?? (ps.length ? ps[0].id : "");
}

function AppContent({ onSource, onLogout }: { onSource: (s: DataSource) => void; onLogout: () => void }) {
  const src = useDataSource();
  const locked = Boolean(src.locked);
  const [email, setEmail] = useState("");
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
  // IA v2 shell: lens-based routing. Settings is a peer view, not a modal.
  const [lens, setLens] = useState<Lens>(() => readSavedLens());
  const previousLens = useRef<Lens>(lens);
  const [settingsSeat] = useState<string | null>(null); // future: seat-gear deep link into Settings
  const [wizardOpen, setWizardOpen] = useState(false);
  const [dispatchOpen, setDispatchOpen] = useState(false);
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
  // Board | Flow main-view toggle, persisted per browser.
  const [boardMode, setBoardMode] = useState<BoardMode>(() => {
    const saved = typeof localStorage !== "undefined" ? localStorage.getItem(BOARD_MODE_STORAGE_KEY) : null;
    return (saved as BoardMode) === "flow" ? "flow" : "board";
  });

  const [toasts, setToasts] = useState<Toast[]>([]);
  const [staleTasks, setStaleTasks] = useState<Set<string>>(new Set());
  const [recipes, setRecipes] = useState<RecipeItem[]>([]);
  const [recipeOpen, setRecipeOpen] = useState(false);
  // Cockpit is now a full-page lens. The selected seat is tracked so the view
  // can survive re-renders and be changed from inside CockpitPanel.
  const [cockpitSeat, setCockpitSeat] = useState("");
  const openCockpit = (s: string) => { setCockpitSeat(s); setLens("cockpit"); };
  // Live count of cockpit pending approvals surfaced on the Cockpit lens badge.
  const [cockpitPending, setCockpitPending] = useState(0);
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
        return pickInitialProject(ps, localStorage.getItem(LAST_PROJECT_STORAGE_KEY));
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
      // Display the signed-in email in the toolbar.
      fetchMe().then((m: MeResponse) => setEmail(m.user.email)).catch(() => setEmail(""));
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

  // Cockpit pending-approval count: poll the orchestrator seat's status so the
  // toolbar Cockpit badge shows the live queue length. Lifted from CockpitPanel
  // (ui2-shell spec §3) so the badge can live in the shared shell.
  const orchestratorSeat = state.agents.find((a) => a.roles.includes("orchestrator"))?.id ?? seat ?? "claude";
  const cockpitSeatTarget = cockpitSeat || orchestratorSeat;
  useEffect(() => {
    // Capture the optional method so TS keeps the narrowing inside the closure.
    const statusFn = src.cockpitStatus?.bind(src);
    if (!current || !statusFn || !src.capabilities.cockpit || locked) {
      setCockpitPending(0);
      return;
    }
    let alive = true;
    const poll = async () => {
      try {
        const st = await statusFn(current, cockpitSeatTarget);
        if (alive) setCockpitPending(st.pending?.length ?? 0);
      } catch {
        if (alive) setCockpitPending(0);
      }
    };
    poll();
    const t = setInterval(poll, COCKPIT_STATUS_POLL_MS);
    return () => { alive = false; clearInterval(t); };
  }, [current, cockpitSeatTarget, src.cockpitStatus, src.capabilities.cockpit, locked]);

  // Persist lens + remember previous non-settings lens so Escape from Settings
  // returns there instead of hard-coding Board.
  useEffect(() => {
    localStorage.setItem(LENS_STORAGE_KEY, lens);
    if (lens !== "settings") previousLens.current = lens;
  }, [lens]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && lens === "settings") {
        e.stopPropagation();
        setLens(previousLens.current === "settings" ? "board" : previousLens.current);
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [lens]);

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
    if (locked) {
      // A locked (bearer-only) source cannot decrypt state or events. Keep the
      // project selector live but skip all decryption-dependent fetches.
      setState(EMPTY);
      setEvents([]);
      setLoadFailed(false);
      setLive(false);
      return;
    }
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
      // Shared event-append with event_id dedupe: the local SSE backfill can
      // overlap a live event that raced in between subscribe and replay, and
      // the hosted REST backfill (below) can overlap the socket stream.
      const appendEvent = (e: PactEvent) => {
        if (seenEventIds.current.has(e.event_id)) return;
        seenEventIds.current.add(e.event_id);
        setEvents((prev) => {
          const next = [...prev, e];
          return next.length > EVENTS_CAP ? next.slice(-EVENTS_CAP) : next;
        });
      };
      const off = src.subscribe(
        current,
        (s) => { if (alive) { noteFetchOk(); applyState(s); } },
        (e) => { if (alive) appendEvent(e); },
        () => { if (alive) noteFetchFail(); },
        (v) => { if (alive) setLive(v); },
      );
      // Backfill history via the REST log. The LOCAL SSE stream replays the
      // log tail on connect, but the HOSTED (relay) subscription is live-only
      // — without this, Flow/EventDrawer start empty on hosted until new
      // activity arrives. Sort merged history by ts so a live event that
      // raced in ahead of the backfill can't scramble the timeline.
      const fetchLog = src.fetchEventsLog?.bind(src) ?? fetchEventsLog;
      fetchLog(current, undefined, EVENTS_CAP)
        .then((evs) => {
          if (!alive) return;
          evs.forEach(appendEvent);
          setEvents((prev) =>
            [...prev].sort((a, b) => (a.ts < b.ts ? -1 : a.ts > b.ts ? 1 : 0)),
          );
        })
        .catch(() => { /* backfill is best-effort; live stream still works */ });
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

  // Remember the selected project id across reloads.
  useEffect(() => {
    if (current) localStorage.setItem(LAST_PROJECT_STORAGE_KEY, current);
  }, [current]);

  // Persist the Board | Flow view preference.
  useEffect(() => {
    localStorage.setItem(BOARD_MODE_STORAGE_KEY, boardMode);
  }, [boardMode]);

  const currentName = projects.find((p) => p.id === current)?.name ?? current;

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
        onOpenDispatch={() => setDispatchOpen(true)}
        lens={lens}
        onLensChange={setLens}
        cockpitPending={cockpitPending}
        profileEmail={email}
        worktreesByProject={worktreesByProject}
        currentWorktree={currentWorktree}
        onSelectWorktree={(name, branch) => { setCurrent(name); setCurrentWorktree(branch); }}
      />
      <div className="relative flex flex-1 overflow-hidden">
        {projectsLoaded && projects.length === 0
          ? <NoProjects onRegistered={refreshProjects} />
          : locked
            ? <UnlockPanel onUnlock={onSource} />
            : renderLens()}
      </div>
      <Toasts toasts={toasts} />
      {fetchStale && (
        <div
          data-testid="fetch-stale"
          role="status"
          className="pointer-events-none fixed bottom-4 left-4 z-[60] rounded-md border border-[var(--color-border-subtle)] border-l-2 border-l-[var(--color-danger)] bg-[var(--color-bg-raised)] px-3 py-2 text-xs text-[var(--color-text-1)] shadow-[var(--shadow-raised)]"
        >
          Live updates interrupted — retrying…
        </div>
      )}
      <AddProjectWizard open={wizardOpen} onClose={() => setWizardOpen(false)} onAdded={() => { setWizardOpen(false); refreshProjects(); }} />
      <DispatchPanel
        project={current}
        roster={shownState.agents}
        open={dispatchOpen}
        onClose={() => setDispatchOpen(false)}
        onGoLive={() => setDispatchOpen(false)}
        initialGoal=""
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

  function renderLens() {
    switch (lens) {
      case "dashboard":
        return <DashboardPlaceholder />;
      case "settings":
        return (
          <SettingsModal
            project={current}
            author={author}
            focusSeat={settingsSeat}
            onClose={() => setLens(previousLens.current === "settings" ? "board" : previousLens.current)}
            onLogout={onLogout}
            viewMode
          />
        );
      case "cockpit":
        if (!current || !src.capabilities.cockpit) {
          return (
            <div className="flex flex-1 items-center justify-center text-sm text-[var(--color-text-3)]">
              Cockpit is not available for this project or data source.
            </div>
          );
        }
        return (
          <CockpitPanel
            project={current}
            seat={cockpitSeatTarget}
            agents={shownState.agents}
            onSeatChange={setCockpitSeat}
            onNotify={pushToast}
            viewMode
          />
        );
      case "board":
      default:
        return (
          <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
            <Agents author={author} onChanged={refreshProjects} />
            <RunRail project={current} state={shownState} refreshTick={refreshTick} author={author} events={events} onNotify={(msg, kind) => pushToast(msg, kind)} onOpenCockpit={openCockpit} />
            <div className="flex items-center gap-2 border-b border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] px-[18px] py-[9px]">
              <div className="flex items-center gap-1 rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-page)] p-0.5">
                {(["board", "flow"] as BoardMode[]).map((m) => (
                  <button
                    key={m}
                    type="button"
                    data-testid={`board-mode-${m}`}
                    onClick={() => setBoardMode(m)}
                    className="rounded-md px-2.5 py-1 text-[11px] font-medium transition-colors"
                    style={{
                      color: boardMode === m ? "var(--color-text-1)" : "var(--color-text-3)",
                      background: boardMode === m ? "rgba(255,255,255,0.09)" : "transparent",
                    }}
                  >
                    {m === "board" ? "Board" : "Flow"}
                  </button>
                ))}
              </div>
            </div>
            <div className="relative flex flex-1 overflow-hidden">
              {boardMode === "board" ? (
                <div data-testid="view-board" className="flex flex-1 overflow-hidden"><Board state={shownState} events={events} selected={selected} onSelect={setSelected} pulses={pulses} staleTasks={staleTasks} loading={firstLoad} project={current} author={author} onChanged={() => setRefreshTick((t) => t + 1)} onOpenCockpit={openCockpit} /></div>
              ) : (
                <div data-testid="view-flow" className="flex flex-1 overflow-hidden"><FlowView state={shownState} events={events} project={current} selected={selected} onSelect={setSelected} /></div>
              )}
              {src.capabilities.multiMachine
                ? <TaskDetail project={current} taskId={selected} state={shownState} onClose={() => setSelected("")} onOpenCockpit={openCockpit} />
                : <RightRail state={shownState} events={events} selected={selected} project={current} author={author} onSelect={setSelected} />}
            </div>
            <EventDrawer events={events} agents={shownState.agents} state={shownState} />
          </div>
        );
    }
  }
}

function DashboardPlaceholder() {
  return (
    <div data-testid="view-dashboard" className="flex flex-1 flex-col items-center justify-center gap-3 text-[var(--color-text-2)]">
      <div className="text-sm">Dashboard</div>
      <div className="text-xs text-[var(--color-text-3)]">Coming in the next slice.</div>
    </div>
  );
}

export default function App() {
  // Local build → the co-located serve source is ready immediately. Hosted build
  // → start with no source and gate on IdentityGate until an SSO session is
  // established (or the user opts into the legacy master-secret paste flow).
  const [source, setSource] = useState<DataSource | null>(() =>
    isHostedMode() ? null : localSource(),
  );
  if (!source) {
    return <IdentityGate onSource={setSource} />;
  }
  return (
    <DataSourceProvider source={source}>
      <AppContent onSource={setSource} onLogout={() => setSource(null)} />
    </DataSourceProvider>
  );
}
