import { useCallback, useEffect, useMemo, useState } from "react";
import { Command } from "cmdk";
import type { ProjectMeta, State } from "../lib/types";
import type { View } from "./TopBar";
import { allTasks } from "../lib/derive";
import { postVerb } from "../lib/api";
import { humanizeError } from "../lib/protocolErrors";
import { Kbd } from "./ui/Kbd";
import { Modal } from "./ui/Modal";

// CommandK — ⌘K command palette (plan T13, mockup board4 §②). Self-contained:
// installs its own global listeners (⌘K/Ctrl+K + the `pactify:cmdk` CustomEvent
// the TopBar hint dispatches) to open, and a typing-guarded `?` to open the
// shortcut cheat sheet. Receives the data + callbacks it needs as props so App
// stays the single source of truth for view/selection/project.
//
// Groups (per board4 §② + spec §5):
//   Tasks    — every task; ↵ → setView("canvas") + setSelected(id) (the detail
//              panel opens over the canvas). See the canvas-focus note below.
//   Actions  — Accept / Request changes for awaiting_review tasks (capped at 5);
//              hidden entirely when observing OR replaying (write ops are unsafe
//              there). "Replay to this task's last event" is DEFERRED to T14
//              (timeline jump needs ?at plumbing that does not exist yet).
//   Navigate — switch view (Kbd 1/2/3) + switch project (one entry per project).
//
// CANVAS-FOCUS DECISION: the board4 hint says "↵ 聚焦" (focus). Wiring React
// Flow's fitView-to-a-specific-node down through App is invasive (App does not
// hold the Canvas's useReactFlow instance). The honest, non-invasive path is to
// switch to the canvas view and select the task: the detail panel slides over
// the canvas, which IS the "focus on this task" affordance. A literal
// pan/zoom-to-node animation is left as a TODO for a later Canvas-internal pass.

// "Item value" cmdk uses for filtering/selection. cmdk filters on the value +
// keywords; we feed it an id-forward value so typing a task id surfaces it.
type ActionKind = "accept" | "changes";

function statusIcon(status: string): string {
  switch (status) {
    case "assigned": return "◇";
    case "in_progress": return "⚡";
    case "awaiting_review": return "◉";
    case "changes_requested": return "↺";
    case "accepted": return "✓";
    default: return "◦";
  }
}

export function CommandK({
  projects,
  current,
  state,
  view,
  setView,
  setSelected,
  onSelectProject,
  author,
  replaying,
  notify,
}: {
  projects: ProjectMeta[];
  current: string;
  state: State;
  view: View;
  setView: (v: View) => void;
  setSelected: (id: string) => void;
  onSelectProject: (id: string) => void;
  // author => this dashboard can act (has an acting seat). Write actions are
  // hidden when !author (observe mode) or while replaying.
  author: boolean;
  replaying: boolean;
  // Surfaces palette-action failures on the app toast stack (a failed accept
  // produces no SSE event, so nothing else would ever report it).
  notify?: (text: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [cheat, setCheat] = useState(false);

  // Focus restore (WAI-ARIA dialog): the cmdk input autofocuses on open; on
  // close, give focus back to whatever had it (TopBar hint, canvas, …).
  useEffect(() => {
    if (!open) return;
    const prev = document.activeElement as HTMLElement | null;
    return () => prev?.focus();
  }, [open]);

  const tasks = useMemo(() => allTasks(state), [state]);
  // Awaiting-review tasks are the ones Accept / Request-changes can target.
  // Capped at 5 to keep the palette tight (the detail panel handles the rest).
  const awaiting = useMemo(
    () => tasks.filter((b) => b.task.status === "awaiting_review").slice(0, 5),
    [tasks],
  );
  const showActions = author && !replaying;

  // Global open: ⌘K / Ctrl+K from anywhere (the convention — works even inside
  // inputs), EXCEPT when another modal/dialog is open (don't stack over the
  // detail panel / a ui Modal — the cheat-sheet Modal included). Only the
  // palette's OWN dialog is exempt, via its [data-cmdk-root] attribute, so ⌘K
  // still toggle-closes an open palette.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && (e.key === "k" || e.key === "K")) {
        // A foreign dialog open (detail panel, ui Modal) — let it keep focus.
        const dialog = document.querySelector('[role="dialog"]');
        if (dialog && !dialog.hasAttribute("data-cmdk-root")) return;
        e.preventDefault();
        setOpen((o) => !o);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // TopBar ⌘K hint dispatches this CustomEvent.
  useEffect(() => {
    const onEvt = () => setOpen(true);
    window.addEventListener("pactify:cmdk", onEvt as EventListener);
    return () => window.removeEventListener("pactify:cmdk", onEvt as EventListener);
  }, []);

  // `?` opens the cheat sheet — typing-guarded (never while typing in a field)
  // and not while a dialog/palette is already open.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "?") return;
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const t = e.target as HTMLElement | null;
      const tag = t?.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || t?.isContentEditable) return;
      if (document.querySelector('[role="dialog"]')) return;
      e.preventDefault();
      setCheat(true);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const close = useCallback(() => {
    setOpen(false);
    setSearch("");
  }, []);

  // ↵ on a task: switch to canvas + select it (detail panel opens). See the
  // canvas-focus decision note at the top of this file.
  const focusTask = useCallback((id: string) => {
    setView("canvas");
    setSelected(id);
    close();
  }, [setView, setSelected, close]);

  const runAction = useCallback(async (kind: ActionKind, taskId: string) => {
    close();
    if (kind === "accept") {
      try {
        await postVerb(current, "accept", { task: taskId });
      } catch (e) {
        // A failed accept emits no event — nothing downstream would report it.
        // Route the raw engine error through humanizeError so the toast carries
        // actionable guidance (e.g. self-accept → use a reviewer seat).
        const raw = e instanceof Error ? e.message : String(e);
        notify?.(`accept ${taskId}: ${humanizeError(raw)}`);
      }
    } else {
      // Request changes needs a reason — the detail panel owns that flow. Open
      // the task (canvas + select); the panel's "Changes…" affordance collects
      // the note. We don't POST a reasonless `changes` here.
      setView("canvas");
      setSelected(taskId);
    }
  }, [close, current, setView, setSelected, notify]);

  const switchProject = useCallback((id: string) => {
    onSelectProject(id);
    close();
  }, [onSelectProject, close]);

  return (
    <>
      {open && (
        <div
          data-testid="cmdk-overlay"
          className="fixed inset-0 z-[60] flex items-start justify-center bg-black/55 backdrop-blur-sm pt-[14vh]"
          onClick={close}
        >
          <Command
            label="Command palette"
            aria-label="Command palette"
            data-testid="cmdk"
            data-cmdk-root=""
            role="dialog"
            aria-modal="true"
            className="w-[560px] max-w-[92vw] overflow-hidden rounded-[14px] border border-[var(--color-border-strong)] bg-[var(--color-bg-overlay)] shadow-[var(--shadow-overlay)]"
            onClick={(e) => e.stopPropagation()}
            onKeyDown={(e) => {
              // Esc closes the palette and must NOT leak to the canvas Esc chains
              // behind it.
              if (e.key === "Escape") {
                e.stopPropagation();
                close();
              }
              // Focus containment: the input is the palette's only focusable
              // element (options are aria-selected, not tabbable), so trapping
              // is simply refusing to Tab out of the dialog.
              if (e.key === "Tab") e.preventDefault();
            }}
          >
            {/* input row */}
            <div className="flex items-center gap-2.5 border-b border-white/[0.08] px-4 py-[13px]">
              <span className="text-[var(--color-text-3)]">⌕</span>
              <Command.Input
                value={search}
                onValueChange={setSearch}
                autoFocus
                placeholder="Jump to a task, action, view, or project…"
                className="flex-1 bg-transparent text-sm text-[var(--color-text-1)] outline-none placeholder:text-[var(--color-text-3)]"
              />
              <Kbd>esc</Kbd>
            </div>

            <Command.List className="max-h-[360px] overflow-y-auto py-1.5">
              <Command.Empty className="px-4 py-6 text-center text-[12px] text-[var(--color-text-3)]">
                No results.
              </Command.Empty>

              {/* Tasks */}
              <Command.Group
                heading="Tasks"
                className="[&_[cmdk-group-heading]]:px-3.5 [&_[cmdk-group-heading]]:pb-0.5 [&_[cmdk-group-heading]]:pt-1.5 [&_[cmdk-group-heading]]:text-[9.5px] [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-[.6px] [&_[cmdk-group-heading]]:text-[var(--color-text-3)]"
              >
                {tasks.map((b) => (
                  <Command.Item
                    key={b.task.id}
                    value={`task ${b.task.id}`}
                    keywords={[b.task.id, b.feature, b.task.status]}
                    onSelect={() => focusTask(b.task.id)}
                    className="mx-1.5 flex cursor-pointer items-center gap-2.5 rounded-lg px-3 py-2 text-[13px] text-[var(--color-text-1)] data-[selected=true]:bg-[linear-gradient(180deg,rgba(147,180,242,.18),rgba(147,180,242,.09))]"
                  >
                    <span className="inline-flex h-[22px] w-[22px] items-center justify-center rounded-md bg-white/[0.06] text-[11px]">
                      {statusIcon(b.task.status)}
                    </span>
                    <span className="mono text-[12px]">{b.task.id}</span>
                    <span className="ml-auto text-[10.5px] text-[var(--color-text-3)]">
                      {b.task.status.replace(/_/g, " ")} · {b.feature}
                    </span>
                    <Kbd className="ml-2">↵</Kbd>
                  </Command.Item>
                ))}
              </Command.Group>

              {/* Actions — hidden in observe / replay */}
              {showActions && awaiting.length > 0 && (
                <Command.Group
                  heading="Actions"
                  className="[&_[cmdk-group-heading]]:px-3.5 [&_[cmdk-group-heading]]:pb-0.5 [&_[cmdk-group-heading]]:pt-1.5 [&_[cmdk-group-heading]]:text-[9.5px] [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-[.6px] [&_[cmdk-group-heading]]:text-[var(--color-text-3)]"
                >
                  {awaiting.map((b) => (
                    <Command.Item
                      key={`accept-${b.task.id}`}
                      value={`accept ${b.task.id}`}
                      keywords={[b.task.id, "accept"]}
                      onSelect={() => runAction("accept", b.task.id)}
                      className="mx-1.5 flex cursor-pointer items-center gap-2.5 rounded-lg px-3 py-2 text-[13px] text-[var(--color-text-1)] data-[selected=true]:bg-[linear-gradient(180deg,rgba(147,180,242,.18),rgba(147,180,242,.09))]"
                    >
                      <span className="inline-flex h-[22px] w-[22px] items-center justify-center rounded-md bg-white/[0.06] text-[11px]">✓</span>
                      <span>Accept <span className="mono text-[11px]">{b.task.id}</span></span>
                      <Kbd className="ml-auto">↵</Kbd>
                    </Command.Item>
                  ))}
                  {awaiting.map((b) => (
                    <Command.Item
                      key={`changes-${b.task.id}`}
                      value={`request changes ${b.task.id}`}
                      keywords={[b.task.id, "changes", "request"]}
                      onSelect={() => runAction("changes", b.task.id)}
                      className="mx-1.5 flex cursor-pointer items-center gap-2.5 rounded-lg px-3 py-2 text-[13px] text-[var(--color-text-1)] data-[selected=true]:bg-[linear-gradient(180deg,rgba(147,180,242,.18),rgba(147,180,242,.09))]"
                    >
                      <span className="inline-flex h-[22px] w-[22px] items-center justify-center rounded-md bg-white/[0.06] text-[11px]">↺</span>
                      <span>Request changes… <span className="mono text-[11px]">{b.task.id}</span></span>
                    </Command.Item>
                  ))}
                </Command.Group>
              )}

              {/* Navigate */}
              <Command.Group
                heading="Navigate"
                className="[&_[cmdk-group-heading]]:px-3.5 [&_[cmdk-group-heading]]:pb-0.5 [&_[cmdk-group-heading]]:pt-1.5 [&_[cmdk-group-heading]]:text-[9.5px] [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-[.6px] [&_[cmdk-group-heading]]:text-[var(--color-text-3)]"
              >
                {([
                  { v: "board" as View, label: "Board", key: "1", ico: "▤" },
                  { v: "canvas" as View, label: "Canvas", key: "2", ico: "▦" },
                  { v: "live" as View, label: "Live", key: "3", ico: "◉" },
                ]).map((o) => (
                  <Command.Item
                    key={o.v}
                    value={`switch view ${o.label}`}
                    keywords={["view", o.label]}
                    onSelect={() => { setView(o.v); close(); }}
                    className="mx-1.5 flex cursor-pointer items-center gap-2.5 rounded-lg px-3 py-2 text-[13px] text-[var(--color-text-1)] data-[selected=true]:bg-[linear-gradient(180deg,rgba(147,180,242,.18),rgba(147,180,242,.09))]"
                  >
                    <span className="inline-flex h-[22px] w-[22px] items-center justify-center rounded-md bg-white/[0.06] text-[11px]">{o.ico}</span>
                    Switch view: {o.label}
                    {view === o.v && <span className="text-[10px] text-[var(--color-text-3)]">·</span>}
                    <Kbd className="ml-auto">{o.key}</Kbd>
                  </Command.Item>
                ))}
                {projects.map((p) => (
                  <Command.Item
                    key={`proj-${p.id}`}
                    value={`switch project ${p.name} ${p.id}`}
                    keywords={["project", p.name, p.id]}
                    onSelect={() => switchProject(p.id)}
                    className="mx-1.5 flex cursor-pointer items-center gap-2.5 rounded-lg px-3 py-2 text-[13px] text-[var(--color-text-1)] data-[selected=true]:bg-[linear-gradient(180deg,rgba(147,180,242,.18),rgba(147,180,242,.09))]"
                  >
                    <span className="inline-flex h-[22px] w-[22px] items-center justify-center rounded-md bg-white/[0.06] text-[11px]">⌂</span>
                    <span>Switch project: <span className="mono text-[12px]">{p.name}</span></span>
                    {p.id === current && <span className="ml-auto text-[10px] text-[var(--color-success)]">●</span>}
                  </Command.Item>
                ))}
              </Command.Group>
            </Command.List>

            {/* footer hints */}
            <div className="flex gap-3.5 border-t border-white/[0.07] px-4 py-[9px] text-[9.5px] text-[var(--color-text-3)]">
              <span>↑↓ Navigate</span>
              <span>↵ Run</span>
              <span>Write actions hidden in observe mode</span>
            </div>
          </Command>
        </div>
      )}

      {cheat && <CheatSheet onClose={() => setCheat(false)} />}
    </>
  );
}

// CheatSheet — static shortcut reference (plan T13.4). A ui/Modal so it inherits
// the overlay token, Esc-closes and focus trap.
const SHORTCUTS: ReadonlyArray<{ keys: string[]; what: string }> = [
  { keys: ["1"], what: "Board view" },
  { keys: ["2"], what: "Canvas view" },
  { keys: ["3"], what: "Live view" },
  { keys: ["⌘", "K"], what: "Command palette" },
  { keys: ["Shift", "drag"], what: "Marquee select (canvas)" },
  { keys: ["Del"], what: "Delete selected draft (canvas)" },
  { keys: ["Esc"], what: "Close panel / menu / cancel draft" },
  { keys: ["←", "→"], what: "Step replay (one event)" },
  { keys: ["?"], what: "This cheat sheet" },
];

function CheatSheet({ onClose }: { onClose: () => void }) {
  return (
    <Modal title="Keyboard shortcuts" onClose={onClose} testId="cheat-sheet" width="380px">
      <div className="flex flex-col gap-2">
        {SHORTCUTS.map((s) => (
          <div key={s.what} className="flex items-center justify-between text-[12px] text-[var(--color-text-2)]">
            <span>{s.what}</span>
            <span className="flex items-center gap-1">
              {s.keys.map((k, i) => (
                <Kbd key={i}>{k}</Kbd>
              ))}
            </span>
          </div>
        ))}
      </div>
    </Modal>
  );
}
