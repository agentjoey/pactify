import { useEffect, useMemo, useState } from "react";
import { Modal } from "../ui/Modal";
import { Button } from "../ui/Button";
import { Input } from "../ui/Input";
import { FolderPicker } from "./FolderPicker";
import {
  getSetupSuggest,
  postRegister,
  setupApply,
  type SetupApplySeat,
  type SetupApplyResponse,
} from "../../lib/api";
import type { FsBrowseEntry, SetupBinding } from "../../lib/types";
import { humanizeError } from "../../lib/protocolErrors";

// AddProjectWizard — two-step modal for registering or initializing one or more
// repos into a dashboard project group.
//
// Step 1 picks a group name and a set of folders via FolderPicker.
// Step 2 edits the seat roster for folders that lack .pact/; folders that already
// have .pact/ are registered without init. Submission is per-folder, results are
// summarized, and any docOnly wiring snippets are surfaced for copy-paste.

const ALL_ROLES = ["orchestrator", "reviewer", "worker"] as const;
type Role = (typeof ALL_ROLES)[number];

const ROLE_COLOR: Record<Role, { main: string; ink: string }> = {
  orchestrator: { main: "var(--color-role-product)", ink: "var(--color-role-product-ink)" },
  reviewer: { main: "var(--color-role-design)", ink: "var(--color-role-design-ink)" },
  worker: { main: "var(--color-role-dev)", ink: "var(--color-role-dev-ink)" },
};

function basename(p: string): string {
  const normal = p.replace(/\\/g, "/").replace(/\/$/, "");
  return normal.split("/").filter(Boolean).pop() ?? "";
}

function entryFor(kind: string): string {
  if (kind.startsWith("claude")) return "CLAUDE.md";
  if (kind.startsWith("gemini")) return "GEMINI.md";
  return "AGENTS.md";
}

function bindingToSeat(b: SetupBinding): SetupApplySeat {
  return {
    id: b.seat,
    roles: b.roles,
    entry: entryFor(b.kind),
    kind: b.kind,
  };
}

interface FolderResult {
  path: string;
  ok: boolean;
  name?: string;
  error?: string;
  wired?: SetupApplyResponse["wired"];
  notes?: string[];
}

export function AddProjectWizard({
  open,
  onClose,
  onAdded,
}: {
  open: boolean;
  onClose: () => void;
  onAdded: () => void;
}) {
  const [step, setStep] = useState<1 | 2>(1);
  const [group, setGroup] = useState("");
  const [selected, setSelected] = useState<string[]>([]);
  const [entries, setEntries] = useState<Map<string, FsBrowseEntry>>(new Map());
  const [rosters, setRosters] = useState<Map<string, SetupApplySeat[]>>(new Map());
  const [lastRoster, setLastRoster] = useState<SetupApplySeat[] | null>(null);
  const [loadingSuggest, setLoadingSuggest] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [results, setResults] = useState<FolderResult[] | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) {
      setStep(1);
      setGroup("");
      setSelected([]);
      setEntries(new Map());
      setRosters(new Map());
      setResults(null);
      setError("");
    }
  }, [open]);

  const selectedEntries = useMemo(
    () => selected.map((p) => entries.get(p)).filter(Boolean) as FsBrowseEntry[],
    [selected, entries],
  );

  function handleSelectionChange(paths: string[], selectedEntries: FsBrowseEntry[]) {
    setSelected(paths);
    setEntries((prev) => {
      const next = new Map(prev);
      for (const e of selectedEntries) next.set(e.path, e);
      return next;
    });
  }

  async function toStep2() {
    if (selected.length === 0) return;
    setLoadingSuggest(true);
    setError("");
    try {
      const suggest = await getSetupSuggest().catch(() => ({ bindings: [], warnings: [] }));
      const baseRoster = suggest.bindings.map(bindingToSeat);
      setRosters((prev) => {
        const next = new Map(prev);
        for (const e of selectedEntries) {
          if (!e.hasPact && !next.has(e.path)) {
            next.set(e.path, baseRoster.map((s) => ({ ...s })));
          }
        }
        return next;
      });
      setStep(2);
    } catch (e) {
      setError(humanizeError(e instanceof Error ? e.message : String(e)));
    } finally {
      setLoadingSuggest(false);
    }
  }

  function applyLastRoster(path: string) {
    if (!lastRoster) return;
    setRosters((prev) => new Map(prev).set(path, lastRoster.map((s) => ({ ...s }))));
  }

  function toggleRole(path: string, seatId: string, role: Role) {
    setRosters((prev) => {
      const next = new Map(prev);
      const seats = next.get(path)?.map((s) => {
        if (s.id !== seatId) return s;
        const roles = s.roles.includes(role)
          ? s.roles.filter((r) => r !== role)
          : [...s.roles, role];
        return { ...s, roles };
      });
      if (seats) next.set(path, seats);
      return next;
    });
  }

  async function submit() {
    setSubmitting(true);
    setError("");
    setResults(null);
    const groupValue = group.trim() || undefined;
    const out: FolderResult[] = [];
    try {
      for (const entry of selectedEntries) {
        if (entry.hasPact) {
          try {
            const r = await postRegister(entry.path, { group: groupValue });
            out.push({ path: entry.path, ok: true, name: r.name });
          } catch (e) {
            out.push({ path: entry.path, ok: false, error: humanizeError(e instanceof Error ? e.message : String(e)) });
          }
        } else {
          // Only agents the user gave at least one role join the project; agents
          // left with no roles are simply not added (not an error).
          const seats = (rosters.get(entry.path) ?? []).filter((s) => s.roles.length > 0);
          try {
            const r = await setupApply({
              path: entry.path,
              project: basename(entry.path),
              seats,
              group: groupValue,
            });
            out.push({ path: entry.path, ok: true, name: basename(entry.path), wired: r.wired, notes: r.notes });
            if (seats.length) setLastRoster(seats);
          } catch (e) {
            out.push({ path: entry.path, ok: false, error: humanizeError(e instanceof Error ? e.message : String(e)) });
          }
        }
      }
      setResults(out);
    } finally {
      setSubmitting(false);
    }
  }

  // rosterGaps returns which required roles a folder's *joined* agents (those with
  // ≥1 role) don't cover — a new project needs orchestrator + reviewer + worker to
  // be runnable. Empty = ready.
  function rosterGaps(path: string): Role[] {
    const seats = (rosters.get(path) ?? []).filter((s) => s.roles.length > 0);
    const have = new Set(seats.flatMap((s) => s.roles));
    return (["orchestrator", "reviewer", "worker"] as Role[]).filter((r) => !have.has(r));
  }

  function canSubmitStep2(): boolean {
    for (const entry of selectedEntries) {
      if (!entry.hasPact && rosterGaps(entry.path).length > 0) return false;
    }
    return true;
  }

  if (!open) return null;

  return (
    <Modal
      testId="add-project-wizard"
      title="Add projects"
      onClose={onClose}
      width="640px"
      footer={
        <>
          {results ? (
            <>
              <Button variant="ghost" size="sm" onClick={onClose} disabled={!results.every((r) => r.ok)}>Close</Button>
              <Button size="sm" disabled={!results.every((r) => r.ok)} onClick={() => { onAdded(); onClose(); }}>
                {results.every((r) => r.ok) ? "Done" : "Fix errors to continue"}
              </Button>
            </>
          ) : step === 1 ? (
            <>
              <Button variant="ghost" size="sm" onClick={onClose}>Cancel</Button>
              <Button size="sm" disabled={selected.length === 0 || loadingSuggest} loading={loadingSuggest} onClick={toStep2}>
                Next
              </Button>
            </>
          ) : (
            <>
              <Button variant="ghost" size="sm" disabled={submitting} onClick={() => setStep(1)}>Back</Button>
              <Button size="sm" disabled={!canSubmitStep2() || submitting} loading={submitting} onClick={submit}>
                Submit
              </Button>
            </>
          )}
        </>
      }
    >
      <div className="flex flex-col gap-4">
        {step === 1 && (
          <>
            <label className="flex flex-col gap-1">
              <span className="text-[11px] font-medium text-[var(--color-text-3)]">Group (optional)</span>
              <Input
                data-testid="wizard-group"
                value={group}
                onChange={(e) => setGroup(e.target.value)}
                placeholder="e.g. backend team"
              />
            </label>
            <FolderPicker selected={selected} onChange={handleSelectionChange} />
          </>
        )}

        {step === 2 && (
          <div className="flex max-h-[420px] flex-col gap-3 overflow-y-auto pr-1" data-testid="wizard-step2">
            {selectedEntries.map((entry) => (
              <div
                key={entry.path}
                data-testid={`wizard-folder-${entry.name}`}
                className="rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] p-3"
              >
                <div className="mb-2 flex items-center justify-between">
                  <span className="text-[12px] font-[650] text-[var(--color-text-1)]">{entry.name}</span>
                  {entry.hasPact ? (
                    <span
                      className="rounded px-1.5 py-0.5 text-[9px] font-medium uppercase"
                      style={{
                        color: "var(--color-role-design-ink)",
                        background: "color-mix(in srgb, var(--color-role-design) 14%, transparent)",
                      }}
                    >
                      existing project
                    </span>
                  ) : (
                    <span
                      className="rounded px-1.5 py-0.5 text-[9px] font-medium uppercase"
                      style={{
                        color: "var(--color-role-dev-ink)",
                        background: "color-mix(in srgb, var(--color-role-dev) 14%, transparent)",
                      }}
                    >
                      new project
                    </span>
                  )}
                </div>
                {entry.hasPact ? (
                  <p className="text-[11px] text-[var(--color-text-3)]">
                    This folder already has a .pact/ directory. It will be registered in the dashboard.
                  </p>
                ) : (
                  <>
                    {lastRoster && (
                      <button
                        type="button"
                        data-testid={`wizard-apply-last-${entry.name}`}
                        onClick={() => applyLastRoster(entry.path)}
                        className="mb-2 text-[11px] text-[var(--color-role-design)] hover:underline"
                      >
                        Apply last roster
                      </button>
                    )}
                    <RosterEditor seats={rosters.get(entry.path) ?? []} onToggle={(id, role) => toggleRole(entry.path, id, role)} />
                  </>
                )}
              </div>
            ))}
          </div>
        )}

        {error && <p className="text-[11px] text-[var(--color-danger)]">{error}</p>}

        {results && (
          <div data-testid="wizard-results" className="flex flex-col gap-2 rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] p-3">
            {results.map((r) => (
              <div key={r.path} data-testid={`wizard-result-${basename(r.path)}`}>
                <div className="flex items-center gap-2 text-[12px]">
                  <span className={r.ok ? "text-[var(--color-success)]" : "text-[var(--color-danger)]"}>{r.ok ? "✓" : "✕"}</span>
                  <span className="font-medium text-[var(--color-text-1)]">{basename(r.path)}</span>
                  {r.name && <span className="text-[var(--color-text-3)]">→ {r.name}</span>}
                </div>
                {r.error && <p className="ml-5 text-[11px] text-[var(--color-danger)]">{r.error}</p>}
                {r.ok && r.wired && r.wired.length > 0 && (
                  <div className="ml-5 mt-1 flex flex-col gap-1">
                    {r.wired.map((w) => (
                      <div key={`${w.kind}-${w.seat}`} className="text-[11px] text-[var(--color-text-2)]">
                        {w.kind} {w.docOnly ? "(doc-only)" : w.wrote ? "(wired)" : "(skipped)"}
                        {w.docOnly && w.snippet && (
                          <pre className="mt-1 whitespace-pre-wrap rounded border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] p-2 mono text-[10px]">
                            {w.snippet}
                          </pre>
                        )}
                      </div>
                    ))}
                  </div>
                )}
                {r.ok && r.notes && r.notes.length > 0 && (
                  <ul className="ml-5 mt-1 list-disc text-[11px] text-[var(--color-warn)]">
                    {r.notes.map((n, i) => <li key={i}>{n}</li>)}
                  </ul>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </Modal>
  );
}

function RosterEditor({
  seats,
  onToggle,
}: {
  seats: SetupApplySeat[];
  onToggle: (seatId: string, role: Role) => void;
}) {
  if (seats.length === 0) {
    return <p className="text-[11px] text-[var(--color-text-3)]">No agents registered on this machine.</p>;
  }
  return (
    <div className="flex flex-col gap-1.5">
      {seats.map((s) => (
        <div key={s.id} className="flex items-center justify-between gap-2">
          <div className="flex flex-col" style={s.roles.length === 0 ? { opacity: 0.5 } : undefined}>
            <span className="text-[11px] font-medium text-[var(--color-text-1)]">{s.id}</span>
            <span className="text-[10px] text-[var(--color-text-3)]">
              {s.kind}
              {s.roles.length === 0 && <span className="ml-1 italic" data-testid={`not-added-${s.id}`}>· not added</span>}
            </span>
          </div>
          <div className="flex items-center gap-1">
            {ALL_ROLES.map((role) => {
              const on = s.roles.includes(role);
              const c = ROLE_COLOR[role];
              return (
                <button
                  key={role}
                  type="button"
                  data-testid={`role-${s.id}-${role}`}
                  aria-pressed={on}
                  onClick={() => onToggle(s.id, role)}
                  className="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-medium transition-colors"
                  style={
                    on
                      ? { color: c.ink, background: `color-mix(in srgb, ${c.main} 16%, transparent)`, boxShadow: `inset 0 0 0 1px color-mix(in srgb, ${c.main} 32%, transparent)` }
                      : { color: "var(--color-text-3)", background: "var(--color-bg-inset)" }
                  }
                >
                  {role}
                </button>
              );
            })}
          </div>
        </div>
      ))}
    </div>
  );
}
