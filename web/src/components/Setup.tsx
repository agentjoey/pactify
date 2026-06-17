import { useEffect, useState } from "react";
import type { SetupBinding } from "../lib/types";
import { getSetupSuggest, setupApply, type SetupApplySeat, type SetupApplyResponse } from "../lib/api";
import { Badge } from "./ui/Badge";
import { Button } from "./ui/Button";
import { Alert } from "./ui/Alert";
import { EmptyState } from "./ui/EmptyState";
import { Spinner } from "./ui/Spinner";
import { ConfigSection, Inset } from "./ui/ConfigSection";
import { Icon } from "../lib/icons";
import { AgentLogo } from "../lib/agentLogos";

// Setup (#1) — the entry-point view bridging "I registered my agents" to "this
// project can do work". Reads the proposed seat roster from the machine's
// registered agents, lets the user toggle roles per seat, recomputes role-gap
// warnings live, and either generates the exact CLI commands to apply the
// roster or runs one-click setup via POST /api/setup/apply.

const ALL_ROLES = ["orchestrator", "reviewer", "worker"] as const;
type Role = (typeof ALL_ROLES)[number];

// Each role tints with its own brand color when active — the three-main-color
// language (orchestrator gold · reviewer blue · worker green).
const ROLE_COLOR: Record<Role, { main: string; ink: string }> = {
  orchestrator: { main: "var(--color-role-product)", ink: "var(--color-role-product-ink)" },
  reviewer: { main: "var(--color-role-design)", ink: "var(--color-role-design-ink)" },
  worker: { main: "var(--color-role-dev)", ink: "var(--color-role-dev-ink)" },
};

// entryFor mirrors the CLI's kind→entry-file convention for the init command.
function entryFor(kind: string): string {
  if (kind.startsWith("claude")) return "CLAUDE.md";
  if (kind.startsWith("gemini")) return "GEMINI.md";
  return "AGENTS.md";
}

// basename mirrors path.basename for the project-name default.
function basename(p: string): string {
  const normal = p.replace(/\\/g, "/").replace(/\/$/, "");
  return normal.split("/").filter(Boolean).pop() ?? "";
}

// validate mirrors internal/wizard.Validate: a runnable roster needs an
// orchestrator, a reviewer, and a worker (a worker can't self-accept, so the
// reviewer can't also be the only one doing the work).
function validate(bindings: SetupBinding[]): string[] {
  const has = (r: Role) => bindings.some((b) => b.roles.includes(r));
  const warns: string[] = [];
  if (!has("orchestrator")) warns.push("No orchestrator seat — orchestrate needs one to drive merges");
  if (!has("reviewer")) warns.push("No reviewer seat — tasks can't be accepted (only a reviewer can accept)");
  if (!has("worker")) warns.push("No worker seat — a worker can't self-accept, and an orchestrator/reviewer can't both build and review. Add a worker.");
  return warns;
}

function applyCommands(bindings: SetupBinding[]): string {
  const seats = bindings
    .map((b) => `--seat "${b.seat}:${b.roles.join(",")}:${entryFor(b.kind)}:${b.kind}"`)
    .join(" ");
  const lines = [`pactify init <project> ${seats}`];
  for (const b of bindings) {
    lines.push(`pactify agent add ${b.kind} --id ${b.seat} --roles ${b.roles.join(",")}`);
  }
  return lines.join("\n");
}

export function Setup() {
  const [bindings, setBindings] = useState<SetupBinding[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [copied, setCopied] = useState(false);

  const [path, setPath] = useState("");
  const [project, setProject] = useState("");
  const [applying, setApplying] = useState(false);
  const [result, setResult] = useState<SetupApplyResponse | null>(null);
  const [applyError, setApplyError] = useState("");

  function load() {
    setLoading(true);
    getSetupSuggest()
      .then((r) => {
        setBindings(r.bindings);
        setError("");
      })
      .catch(() => setError("Failed to load setup suggestion"))
      .finally(() => setLoading(false));
  }

  useEffect(() => {
    load();
  }, []);

  function toggleRole(seat: string, role: Role) {
    setBindings((bs) =>
      (bs ?? []).map((b) => {
        if (b.seat !== seat) return b;
        const roles = b.roles.includes(role)
          ? b.roles.filter((r) => r !== role)
          : [...b.roles, role];
        return { ...b, roles };
      }),
    );
  }

  function handlePathChange(value: string) {
    setPath(value);
    setProject(basename(value));
  }

  async function handleApply() {
    if (!bindings || !path || !project) return;
    setApplying(true);
    setApplyError("");
    setResult(null);
    const seats: SetupApplySeat[] = bindings.map((b) => ({
      id: b.seat,
      roles: b.roles,
      entry: entryFor(b.kind),
      kind: b.kind,
    }));
    try {
      const r = await setupApply({ path, project, seats });
      setResult(r);
    } catch (e) {
      setApplyError(e instanceof Error ? e.message : String(e));
    } finally {
      setApplying(false);
    }
  }

  const warnings = bindings ? validate(bindings) : [];
  const commands = bindings ? applyCommands(bindings) : "";
  const ready = bindings != null && bindings.length > 0 && warnings.length === 0;
  const canApply = ready;

  return (
    <div className="flex-1 overflow-y-auto px-6 py-5 view-enter" data-testid="setup-view">
      <div className="mb-1 flex items-center gap-3">
        <h2 className="text-[15px] font-[650] text-[var(--color-text-1)]">Setup · wire your registered agents into the project</h2>
        {loading && <Spinner size="sm" />}
        {ready && (
          <span className="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10.5px] font-medium"
            style={{ color: "var(--color-success)", background: "color-mix(in srgb, var(--color-success) 12%, transparent)" }}>
            <Icon name="state-accepted" size={12} /> Roles complete
          </span>
        )}
      </div>
      <p className="mb-4 text-[12px] text-[var(--color-text-3)]">
        Suggested seat assignments from the agents registered on this machine. Adjust the roles, pick a project path, then apply in one click or copy the commands below.
      </p>

      {error && (
        <Alert tone="danger" title="Failed to load" onRetry={load}>
          {error}
        </Alert>
      )}

      {!error && !bindings && loading && (
        <div className="flex flex-col gap-2">
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i} className="skeleton" style={{ height: 52, borderRadius: 8 }} />
          ))}
        </div>
      )}

      {!error && bindings && bindings.length === 0 && (
        <EmptyState
          title="No agents registered yet"
          hint="Scan and register agents in the Ops view (or via CLI: pactify agent register <kind>), then come back to assign seats."
        />
      )}

      {!error && bindings && bindings.length > 0 && (
        <div className="fade-rise">
          <div className="flex flex-col gap-2">
            {bindings.map((b, i) => (
              <div
                key={b.seat}
                data-testid="setup-row"
                style={{ animationDelay: `${i * 40}ms` }}
                className="flex flex-wrap items-center gap-3 rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-3 py-2.5 shadow-[var(--shadow-card)] hover-lift fade-rise"
              >
                {/* agent logo — identifies the agent kind at a glance */}
                <AgentLogo kind={b.kind} size={32} />
                <div className="flex min-w-0 flex-col">
                  <span className="mono text-[12px] font-[650] text-[var(--color-text-1)] leading-tight">{b.seat}</span>
                  <span className="text-[10.5px] text-[var(--color-text-3)] leading-tight">{b.kind}</span>
                </div>
                <Badge color={b.drivable ? "role-dev" : "role-design"}>
                  {b.drivable ? "drivable" : "manual"}
                </Badge>
                <div className="ml-auto flex items-center gap-1.5">
                  {ALL_ROLES.map((r) => {
                    const on = b.roles.includes(r);
                    const c = ROLE_COLOR[r];
                    return (
                      <button
                        key={r}
                        type="button"
                        data-testid={`role-${b.seat}-${r}`}
                        aria-pressed={on}
                        onClick={() => toggleRole(b.seat, r)}
                        className="press inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10.5px] font-medium transition-colors duration-[var(--motion-micro)] outline-none focus-visible:ring-2"
                        style={
                          on
                            ? { color: c.ink, background: `color-mix(in srgb, ${c.main} 16%, transparent)`, boxShadow: `inset 0 0 0 1px color-mix(in srgb, ${c.main} 32%, transparent)` }
                            : { color: "var(--color-text-3)", background: "var(--color-bg-inset)" }
                        }
                      >
                        <span aria-hidden style={{ width: 5, height: 5, borderRadius: 999, background: on ? c.main : "var(--color-text-3)", opacity: on ? 1 : 0.5 }} />
                        {r}
                      </button>
                    );
                  })}
                </div>
              </div>
            ))}
          </div>

          {warnings.length > 0 && (
            <div className="mt-3 flex flex-col gap-1.5">
              {warnings.map((wn) => (
                <Alert key={wn} tone="warn">
                  {wn}
                </Alert>
              ))}
            </div>
          )}

          <div className="mt-5">
            <ConfigSection label="Project location" required>
              <Inset className="flex flex-col gap-2">
                <label className="flex flex-col gap-0.5">
                  <span className="text-[10.5px] font-medium text-[var(--color-text-3)]">Project path</span>
                  <input
                    data-testid="setup-path"
                    type="text"
                    value={path}
                    onChange={(e) => handlePathChange(e.target.value)}
                    placeholder="/path/to/new-project"
                    className="w-full rounded border border-[var(--color-border-subtle)] bg-[var(--color-bg-page)] px-2 py-1.5 text-[12px] text-[var(--color-text-1)] outline-none focus-visible:border-[var(--color-role-design)]"
                  />
                </label>
                <label className="flex flex-col gap-0.5">
                  <span className="text-[10.5px] font-medium text-[var(--color-text-3)]">Project name</span>
                  <input
                    data-testid="setup-project"
                    type="text"
                    value={project}
                    onChange={(e) => setProject(e.target.value)}
                    placeholder="new-project"
                    className="w-full rounded border border-[var(--color-border-subtle)] bg-[var(--color-bg-page)] px-2 py-1.5 text-[12px] text-[var(--color-text-1)] outline-none focus-visible:border-[var(--color-role-design)]"
                  />
                </label>
              </Inset>
            </ConfigSection>
          </div>

          <div className="mt-4 flex items-center gap-3">
            <Button
              data-testid="setup-apply-btn"
              onClick={handleApply}
              disabled={!canApply}
              loading={applying}
            >
              Apply
            </Button>
            {!ready && (
              <span className="text-[11px] text-[var(--color-text-3)]">
                Complete all three roles before applying.
              </span>
            )}
          </div>

          {(applyError || result) && (
            <div className="mt-4" data-testid="setup-result">
              {applyError && (
                <Alert tone="danger" title="Apply failed">
                  {applyError}
                </Alert>
              )}
              {result?.inited && !applyError && (
                <Alert tone="success" title="Project initialized">
                  {result.wired.length === 0
                    ? "No agent wiring was required."
                    : `${result.wired.filter((w) => w.wrote && !w.docOnly).length} agent file(s) written.`}
                </Alert>
              )}
              {result && result.wired.length > 0 && (
                <div className="mt-3 flex flex-col gap-2">
                  {result.wired.map((w) => (
                    <div
                      key={`${w.kind}-${w.seat}`}
                      data-testid="setup-wired"
                      className="rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-3 py-2.5"
                    >
                      <div className="mb-1 flex items-center gap-2">
                        <span className="text-[12px] font-[650] text-[var(--color-text-1)]">{w.kind}</span>
                        <Badge color={w.docOnly ? "role-design" : w.wrote ? "role-dev" : "role-design"}>
                          {w.docOnly ? "doc-only" : w.wrote ? "wired" : "skipped"}
                        </Badge>
                        <span className="ml-auto text-[11px] text-[var(--color-text-3)]">{w.path}</span>
                      </div>
                      {w.docOnly && w.snippet && (
                        <div className="mt-2">
                          <div className="mb-1 flex items-center justify-between">
                            <span className="text-[10.5px] text-[var(--color-text-3)]">Copy snippet</span>
                            <Button
                              size="sm"
                              variant="ghost"
                              onClick={() => {
                                void navigator.clipboard?.writeText(w.snippet ?? "");
                                setCopied(true);
                                setTimeout(() => setCopied(false), 1500);
                              }}
                            >
                              {copied ? "Copied ✓" : "Copy"}
                            </Button>
                          </div>
                          <pre className="whitespace-pre-wrap rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] px-3 py-2 mono text-[11px] text-[var(--color-text-2)]">
                            {w.snippet}
                          </pre>
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              )}
              {result && result.notes.length > 0 && (
                <div className="mt-3 flex flex-col gap-1.5">
                  {result.notes.map((note, i) => (
                    <Alert key={i} tone="warn">
                      {note}
                    </Alert>
                  ))}
                </div>
              )}
            </div>
          )}

          <div className="mt-5">
            <ConfigSection
              label="Apply commands"
              action={
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => {
                    void navigator.clipboard?.writeText(commands);
                    setCopied(true);
                    setTimeout(() => setCopied(false), 1500);
                  }}
                >
                  {copied ? "Copied ✓" : "Copy"}
                </Button>
              }
            >
              <pre
                data-testid="setup-commands"
                className="whitespace-pre-wrap rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] px-3 py-2.5 mono text-[11px] leading-[1.7] text-[var(--color-text-2)] [overflow-wrap:anywhere]"
              >
                {commands}
              </pre>
            </ConfigSection>
          </div>
        </div>
      )}
    </div>
  );
}
