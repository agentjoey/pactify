import { useEffect, useState } from "react";
import type { SetupBinding } from "../lib/types";
import { getSetupSuggest, setupApply, type SetupApplySeat, type SetupApplyResponse } from "../lib/api";
import { Badge } from "./ui/Badge";
import { Button } from "./ui/Button";
import { Alert } from "./ui/Alert";
import { EmptyState } from "./ui/EmptyState";
import { ConfigSection, Inset } from "./ui/ConfigSection";
import { Icon } from "../lib/icons";
import { CableMark } from "./shell/CableMark";

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

const JOURNEY_STEPS = [
  { index: 0, label: "Setup" },
  { index: 1, label: "Plan" },
  { index: 2, label: "Run" },
  { index: 3, label: "Review" },
  { index: 4, label: "Ship" },
] as const;

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

// agentGradient gives each registered agent its brand tile (claude = gold,
// opencode/gemini = green), matching the design-handoff avatar spec.
function agentGradient(kind: string): { from: string; to: string } {
  if (kind.startsWith("claude")) return { from: "#ffd479", to: "#e0a93a" };
  return { from: "#6ee7a0", to: "#39b97a" };
}

function initials(seat: string): string {
  return seat.slice(0, 2).toLowerCase();
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
    // eslint-disable-next-line react-hooks/set-state-in-effect
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
    <div className="flex h-screen flex-col" data-testid="setup-view">
      {/* Slim toolbar: breadcrumb replaces the centered lens on Setup. */}
      <div className="relative z-50 flex min-h-[46px] shrink-0 items-center gap-3 border-b border-[var(--color-border-subtle)] bg-[color-mix(in_srgb,var(--color-bg-surface)_82%,var(--color-bg-page))] px-3.5 py-2.5 backdrop-blur-[12px]">
        <div className="flex items-center gap-2">
          <CableMark />
          <span className="text-[13px] font-[680] text-[var(--color-text-1)]">pactify</span>
          <span className="text-[var(--color-text-3)]">·</span>
          <span className="text-[12px] font-medium text-[var(--color-text-2)]">
            {project ? `${project} › Setup` : "Setup"}
          </span>
        </div>
        <div className="ml-auto flex items-center gap-2.5">
          <button
            type="button"
            aria-label="command palette"
            onClick={() => window.dispatchEvent(new CustomEvent("pactify:cmdk"))}
            className="rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] px-1.5 py-1 text-[11px] text-[var(--color-text-3)] transition-colors hover:text-[var(--color-text-1)]"
          >
            ⌘K
          </button>
          <span
            className="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[10.5px] font-medium"
            style={{ color: "var(--color-warn)", background: "color-mix(in srgb, var(--color-warn) 12%, transparent)" }}
          >
            setting up
          </span>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto" style={{ background: "radial-gradient(820px 460px at 50% -8%, color-mix(in srgb, var(--color-role-product) 6%, transparent), transparent 62%), var(--color-bg-page)" }}>
        <div className="mx-auto max-w-[780px] px-7 pb-20 pt-[34px]">
          {/* Journey stepper */}
          <div className="mb-[34px] flex flex-wrap items-center gap-0" data-testid="journey-stepper">
            {JOURNEY_STEPS.map((step, i) => {
              const active = step.index === 0;
              const dimColor = "rgba(234,238,245,0.5)";
              return (
                <div key={step.label} className="flex items-center">
                  <div
                    className="inline-flex items-center gap-[7px] rounded-full px-3 py-1.5"
                    style={
                      active
                        ? { background: "color-mix(in srgb, var(--color-role-product) 10%, transparent)", border: "1px solid color-mix(in srgb, var(--color-role-product) 35%, transparent)" }
                        : { padding: "6px 12px" }
                    }
                  >
                    <span
                      className="grid h-[18px] w-[18px] place-items-center rounded-full text-[10px] font-semibold"
                      style={
                        active
                          ? { background: "var(--color-role-product)", color: "#0a0e14" }
                          : { border: "1px solid rgba(255,255,255,0.18)", color: dimColor }
                      }
                    >
                      {step.index}
                    </span>
                    <span
                      className="text-[11.5px] font-medium"
                      style={active ? { color: "var(--color-role-product)" } : { color: dimColor }}
                    >
                      {step.label}
                    </span>
                  </div>
                  {i < JOURNEY_STEPS.length - 1 && (
                    <span
                      className="mx-1 inline-block w-[30px]"
                      style={{ height: 2, background: i === 0 ? "rgba(255,255,255,0.12)" : "rgba(255,255,255,0.08)" }}
                    />
                  )}
                </div>
              );
            })}
          </div>

          {/* Heading */}
          <div className="mb-2 flex flex-wrap items-center gap-3">
            <h1 className="text-[24px] font-semibold leading-[1.2] tracking-[-0.02em] text-[var(--color-text-1)]">
              Wire your agents into the project
            </h1>
            {project && (
              <span
                data-testid="scope-chip"
                className="inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-[10px] font-medium"
                style={{ color: "var(--color-role-product)", background: "color-mix(in srgb, var(--color-role-product) 12%, transparent)", borderColor: "color-mix(in srgb, var(--color-role-product) 28%, transparent)" }}
              >
                <span className="h-[5px] w-[5px] rounded-sm" style={{ background: "var(--color-role-product)" }} />
                PROJECT · {project}
              </span>
            )}
            {ready && (
              <span
                data-testid="roles-complete-chip"
                className="inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-[10.5px] font-medium"
                style={{ color: "var(--color-success)", background: "color-mix(in srgb, var(--color-success) 12%, transparent)", borderColor: "color-mix(in srgb, var(--color-success) 30%, transparent)" }}
              >
                <Icon name="state-accepted" size={12} />
                Roles complete
              </span>
            )}
          </div>

          {/* Intro note */}
          <p className="mb-[26px] max-w-[600px] text-[13.5px] leading-[1.6] text-[var(--color-text-2)]">
            Suggested seat assignments from the agents registered on this machine. Adjust the roles, set a path, then apply in one click or copy the commands below.
            <br />
            <span className="text-[var(--color-text-3)]">
              Agents themselves are registered once on your machine — manage models & permissions in{" "}
              <span className="text-[var(--color-success)]">Settings · Machine</span>.
            </span>
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
              {/* Seat rows */}
              <div className="mb-[18px] flex flex-col gap-2.5">
                {bindings.map((b, i) => {
                  const pad = agentGradient(b.kind);
                  return (
                    <div
                      key={b.seat}
                      data-testid="setup-row"
                      style={{ animationDelay: `${i * 40}ms` }}
                      className="flex flex-wrap items-center gap-[13px] rounded-xl border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-[15px] py-[13px] shadow-[var(--shadow-card)] hover-lift fade-rise"
                    >
                      <span
                        className="grid h-[34px] w-[34px] shrink-0 place-items-center rounded-[10px] font-mono text-[12px] font-semibold"
                        style={{ background: `linear-gradient(135deg, ${pad.from}, ${pad.to})`, color: "#0a0e14" }}
                      >
                        {initials(b.seat)}
                      </span>
                      <div className="flex min-w-0 flex-col">
                        <span className="font-mono text-[12.5px] font-[650] leading-tight text-[var(--color-text-1)]">{b.seat}</span>
                        <span className="text-[10px] leading-tight text-[var(--color-text-3)]">{b.kind}</span>
                      </div>
                      <Badge color={b.drivable ? "role-dev" : "role-design"}>
                        {b.drivable ? "drivable" : "manual"}
                      </Badge>
                      <div className="ml-auto flex items-center gap-[9px]">
                        <span className="font-mono text-[8.5px] font-semibold uppercase tracking-[0.14em] text-[var(--color-text-3)]">
                          ROLES
                        </span>
                        <div className="flex gap-[7px]">
                          {ALL_ROLES.map((r) => {
                            const on = b.roles.includes(r);
                            const c = ROLE_COLOR[r];
                            return (
                              <span key={r} data-testid="role-toggle" data-role={r}>
                                <button
                                  type="button"
                                  data-testid={`role-${b.seat}-${r}`}
                                  aria-pressed={on}
                                  onClick={() => toggleRole(b.seat, r)}
                                  className="press inline-flex items-center gap-[5px] rounded-full px-[11px] py-1 text-[10.5px] font-medium transition-colors duration-[var(--motion-micro)] outline-none focus-visible:ring-2"
                                  style={
                                    on
                                      ? {
                                          color: c.main,
                                          background: `color-mix(in srgb, ${c.main} 16%, transparent)`,
                                          boxShadow: `inset 0 0 0 1px color-mix(in srgb, ${c.main} 32%, transparent)`,
                                        }
                                      : {
                                          color: "rgba(234,238,245,0.5)",
                                          background: "var(--color-bg-page)",
                                          border: "1px solid rgba(255,255,255,0.08)",
                                        }
                                  }
                                >
                                  <span
                                    aria-hidden
                                    style={{
                                      width: 5,
                                      height: 5,
                                      borderRadius: 999,
                                      background: on ? c.main : "rgba(234,238,245,0.4)",
                                    }}
                                  />
                                  {r}
                                </button>
                              </span>
                            );
                          })}
                        </div>
                      </div>
                    </div>
                  );
                })}
              </div>

              {/* Role-gap warnings / success note */}
              {ready ? (
                <div
                  className="mb-6 flex items-start gap-[9px] rounded-[10px] border px-[13px] py-[11px]"
                  style={{
                    background: "color-mix(in srgb, var(--color-success) 7%, transparent)",
                    borderColor: "color-mix(in srgb, var(--color-success) 26%, transparent)",
                  }}
                >
                  <span className="font-mono text-[12px] font-semibold leading-[1.5] text-[var(--color-success)]">✓</span>
                  <span className="text-[12px] leading-[1.55] text-[var(--color-text-2)]">
                    Separation of duties holds — orchestrator, reviewer and worker are all staffed, and no seat is both building and accepting its own work.
                  </span>
                </div>
              ) : (
                <div className="mb-6 flex flex-col gap-1.5">
                  {warnings.map((wn) => (
                    <Alert key={wn} tone="warn">
                      {wn}
                    </Alert>
                  ))}
                </div>
              )}

              {/* Project location */}
              <div className="mb-[22px]">
                <ConfigSection label="Project location" required>
                  <Inset className="flex flex-col gap-3 rounded-[11px] bg-[var(--color-bg-page)] px-[14px] py-[14px]">
                    <label className="flex flex-col gap-[5px]">
                      <span className="text-[10px] font-medium text-[var(--color-text-3)]">Project path</span>
                      <input
                        data-testid="setup-path"
                        type="text"
                        value={path}
                        onChange={(e) => handlePathChange(e.target.value)}
                        placeholder="/path/to/new-project"
                        className="w-full rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-[11px] py-[9px] font-mono text-[12px] text-[var(--color-text-1)] outline-none transition-colors focus:border-[color-mix(in_srgb,var(--color-role-design)_45%,transparent)] focus:shadow-[0_0_0_3px_color-mix(in_srgb,var(--color-role-design)_10%,transparent)]"
                        style={{ caretColor: "var(--color-role-design)" }}
                      />
                    </label>
                    <label className="flex flex-col gap-[5px]">
                      <span className="text-[10px] font-medium text-[var(--color-text-3)]">Project name</span>
                      <input
                        data-testid="setup-project"
                        type="text"
                        value={project}
                        onChange={(e) => setProject(e.target.value)}
                        placeholder="new-project"
                        className="w-full rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-[11px] py-[9px] font-mono text-[12px] text-[var(--color-text-1)] outline-none transition-colors focus:border-[color-mix(in_srgb,var(--color-role-design)_45%,transparent)] focus:shadow-[0_0_0_3px_color-mix(in_srgb,var(--color-role-design)_10%,transparent)]"
                      />
                    </label>
                  </Inset>
                </ConfigSection>
              </div>

              {/* Apply */}
              <div className="mb-[26px] flex items-center gap-3">
                <Button
                  data-testid="setup-apply-btn"
                  onClick={handleApply}
                  disabled={!canApply}
                  loading={applying}
                  className="bg-[var(--color-role-design)] shadow-[0_6px_18px_-6px_color-mix(in_srgb,var(--color-role-design)_50%,transparent)]"
                >
                  Apply · init + wire →
                </Button>
                {!ready && (
                  <span className="text-[11px] text-[var(--color-text-3)]">
                    Complete all three roles before applying.
                  </span>
                )}
              </div>

              {/* Apply result */}
              {(applyError || result) && (
                <div className="mb-4" data-testid="setup-result">
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
                              <pre className="mono whitespace-pre-wrap rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] px-3 py-2 text-[11px] text-[var(--color-text-2)]">
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

              {/* Commands */}
              <div>
                <div className="mb-2 flex items-center justify-between">
                  <span className="font-mono text-[10px] font-semibold uppercase tracking-[1px] text-[var(--color-text-3)]">
                    Or copy the commands
                  </span>
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
                </div>
                <pre
                  data-testid="setup-commands"
                  className="mono whitespace-pre-wrap rounded-[11px] border border-[var(--color-border-subtle)] bg-[#07090d] px-[16px] py-[14px] text-[11.5px] leading-[1.85] text-[var(--color-text-2)] [overflow-wrap:anywhere]"
                >
                  {commands.split("\n").map((line, idx) => (
                    <span key={idx} className="block">
                      <span className="text-[var(--color-success)]">$</span> {line}
                    </span>
                  ))}
                </pre>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
