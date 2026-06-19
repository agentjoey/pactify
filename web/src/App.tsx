import { useEffect, useRef, useState } from "react";
import type { ProjectMeta, State, PactEvent, RecipeItem } from "./lib/types";
import { fetchProjects, fetchState, subscribeEvents, getActingSeat, renameRegistry, deleteRegistry, getOrchestrateStatus, getRecipes, getWorktrees } from "./lib/api";
import type { Worktree } from "./lib/api";
import { type View } from "./lib/types";
import { Toolbar } from "./components/shell/Toolbar";
import { SettingsModal } from "./components/shell/SettingsModal";
import { AddProjectWizard } from "./components/shell/AddProjectWizard";
import { DispatchPanel } from "./components/shell/DispatchPanel";
import { Agents } from "./components/Agents";
import { Board } from "./components/Board";
import { Canvas } from "./components/Canvas";
import { LiveOrchestrate } from "./components/LiveOrchestrate";
import { RightRail } from "./components/RightRail";
import { CommandK } from "./components/CommandK";
import { NoProjects } from "./components/NoProjects";
import { Recipes } from "./components/Recipes";
import { Modal } from "./components/ui/Modal";
import { Toasts, diffAwaiting, type Toast } from "./components/Toasts";
import { allTasks } from "./lib/derive";
import { pulseTargets } from "./lib/comms";
import { docTitle } from "./lib/docTitle";
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
  const [view, setView] = useState<View>("board");
  // IA v2 shell: ⚙ Settings sheet + AddProjectWizard now owned by App (moved out
  // of the dropped Sidebar). `running` drives the ProjectMenu status light.
  const [settingsOpen, setSettingsOpen] = useState(false);
  // The seat a RosterDock gear was clicked from (null = opened from the toolbar
  // ⚙, no seat focus). Drives SettingsModal's jump-to-project-seats behavior.
  const [settingsSeat, setSettingsSeat] = useState<string | null>(null);
  const openSettings = (seat: string | null) => { setSettingsSeat(seat); setSettingsOpen(true); };
  const [wizardOpen, setWizardOpen] = useState(false);
  const [dispatchOpen, setDispatchOpen] = useState(false);
  // Goal seeded into the DispatchPanel from the canvas NL command dock.
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

  // Canvas build-mode drafts live HERE (not inside Canvas) so they survive the
  // Canvas unmount that view switching causes — switching canvas→ops→canvas
  // would otherwise wipe every in-flight draft. Reset on project switch (the
  // [current] effect below), since drafts are scoped to one project's canvas.
  const [drafts, setDrafts] = useState<Draft[]>([]);
  const [draftFeatures, setDraftFeatures] = useState<DraftFeature[]>([]);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const [staleTasks, setStaleTasks] = useState<Set<string>>(new Set());
  const [recipes, setRecipes] = useState<RecipeItem[]>([]);
  const [recipeOpen, setRecipeOpen] = useState(false);
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

  // Fetch recipes once on mount; feed into the ⌘K Templates group.
  useEffect(() => {
    getRecipes().then(setRecipes).catch(() => setRecipes([]));
  }, []);

  // Global view shortcuts: 1/2/3 switch kanban/canvas/ops. Ignored while typing
  // (input/textarea/select or contentEditable) or while a modal/dialog is open.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const t = e.target as HTMLElement | null;
      const tag = t?.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || t?.isContentEditable) return;
      if (document.querySelector('[role="dialog"]')) return;
      // Three lenses: 1 → board, 2 → canvas, 3 → live.
      if (e.key === "1") setView("board");
      else if (e.key === "2") setView("canvas");
      else if (e.key === "3") setView("live");
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
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
    // Clear the displayed snapshot IMMEDIATELY: rendering the previous
    // project's state under the new project id both flashes stale data and
    // made Canvas's FitOnEntry frame the OLD graph (then never refit).
    setState(EMPTY);
    setLoadFailed(false);
    const wt = currentWorktree || undefined;
    fetchState(current, wt)
      .then((s) => { if (alive) applyState(s); })
      .catch(() => { if (alive) { setState(EMPTY); setLoadFailed(true); } });
    if (!currentWorktree) {
      const off = subscribeEvents(current, (e) => {
        if (!alive) return;
        setEvents((prev) => [...prev, e]);
        fetchState(current).then((s) => { if (alive) applyState(s); }).catch(() => {});
      }, (v) => { if (alive) setLive(v); });
      return () => {
        alive = false;
        off();
        setLive(false);
        if (pulseTimer.current) clearTimeout(pulseTimer.current);
      };
    }
    // non-primary worktree: poll every 3s, no SSE
    setLive(false);
    const poll = setInterval(() => {
      fetchState(current, currentWorktree).then((s) => { if (alive) applyState(s); }).catch(() => {});
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
  return (
    <div data-testid="app-root" className="h-screen flex flex-col">
      <Toolbar projectName={currentName} view={view} onView={setView} live={live} author={author} seat={seat} agents={shownState.agents} projects={projects} running={!!runningByProject[current]} runningByProject={runningByProject} onSelectProject={(name) => { setCurrent(name); setCurrentWorktree(""); }} onRenameProject={onRenameProject} onDeleteProject={onDeleteProject} onAddProject={() => setWizardOpen(true)} onOpenSettings={() => openSettings(null)} onOpenDispatch={() => setDispatchOpen(true)} worktreesByProject={worktreesByProject} currentWorktree={currentWorktree} onSelectWorktree={(name, branch) => { setCurrent(name); setCurrentWorktree(branch); }} />
      <div className="relative flex flex-1 overflow-hidden">
        {/* The dark-handoff Board carries its seated cluster in its own context
            header (Board.tsx), so the old floating left dock is gone — the board
            is full-width with no permanent left column. */}
        <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
          <Agents author={author} onChanged={refreshProjects} />
          {projectsLoaded && projects.length === 0
            ? <NoProjects onRegistered={refreshProjects} />
            : view === "live"
            ? (
              <div data-testid="view-live" className="flex-1 overflow-hidden">
                <LiveOrchestrate project={current} refreshTick={refreshTick} author={author} agents={shownState.agents} events={events} onNotify={(msg, kind) => pushToast(msg, kind)} />
              </div>
            )
            : (
              <>
                {/* relative so the slide-over detail panel + its scrim position
                    within this row, overlaying board/canvas. The board and canvas
                    now take the full width — the panel is absolute. */}
                <div className="relative flex flex-1 overflow-hidden">
                  {view === "canvas"
                    ? <div data-testid="view-canvas" className="flex flex-1 overflow-hidden"><Canvas key={current} project={current} state={shownState} author={author} replaying={false} pulses={pulses} onSelectTask={setSelected} drafts={drafts} setDrafts={setDrafts} draftFeatures={draftFeatures} setDraftFeatures={setDraftFeatures} loading={firstLoad} onRun={(goal) => { setDispatchGoal(goal); setDispatchOpen(true); }} /></div>
                    : <div data-testid="view-board" className="flex flex-1 overflow-hidden"><Board state={shownState} events={events} selected={selected} onSelect={setSelected} pulses={pulses} staleTasks={staleTasks} loading={firstLoad} project={current} author={author} onChanged={() => setRefreshTick((t) => t + 1)} /></div>}
                  <RightRail state={shownState} events={events} selected={selected} project={current} author={author} onSelect={setSelected} />
                </div>
              </>
            )}
        </div>
      </div>
      <Toasts toasts={toasts} />
      {settingsOpen && <SettingsModal project={current} author={author} focusSeat={settingsSeat} onClose={() => setSettingsOpen(false)} />}
      <AddProjectWizard open={wizardOpen} onClose={() => setWizardOpen(false)} onAdded={() => { setWizardOpen(false); refreshProjects(); }} />
      <DispatchPanel
        project={current}
        roster={shownState.agents}
        open={dispatchOpen}
        onClose={() => { setDispatchOpen(false); setDispatchGoal(""); }}
        onGoLive={() => { setView("live"); setDispatchOpen(false); }}
        initialGoal={dispatchGoal}
      />
      <CommandK
        projects={projects}
        current={current}
        state={shownState}
        view={view}
        setView={setView}
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
