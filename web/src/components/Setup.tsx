import { useEffect, useState } from "react";
import type { SetupBinding } from "../lib/types";
import { getSetupSuggest } from "../lib/api";
import { Badge } from "./ui/Badge";
import { Button } from "./ui/Button";
import { Alert } from "./ui/Alert";
import { EmptyState } from "./ui/EmptyState";
import { Spinner } from "./ui/Spinner";
import { ConfigSection } from "./ui/ConfigSection";
import { Icon, ICON_NAMES } from "../lib/icons";

// Setup (#1) — the entry-point view bridging "I registered my agents" to "this
// project can do work". Reads the proposed seat roster from the machine's
// registered agents, lets the user toggle roles per seat, recomputes role-gap
// warnings live, and generates the exact `init` + `agent add` commands to apply
// the roster. Apply is copy-the-commands (zero side effects) in this cut;
// one-click apply via HTTP (which would mutate .pact) is a deliberate follow-up.
//
// Phase 1 rebuild: composed from the locked Phase 0 elements — per-kind icon
// tiles, role-colored toggle chips (the three-main-color language), and a
// ConfigSection for the apply block.

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

// kindIcon maps a binding kind onto the icon-library concept (kind-*), falling
// back by family then to the generic robot.
function kindIcon(kind: string): string {
  if (ICON_NAMES.includes(`kind-${kind}`)) return `kind-${kind}`;
  if (kind.startsWith("claude")) return "kind-claude-code";
  if (kind.startsWith("gemini")) return "kind-gemini-cli";
  if (kind.startsWith("codex")) return "kind-codex-cli";
  if (kind.startsWith("cursor")) return "kind-cursor-cli";
  return "kind-agent";
}

// validate mirrors internal/wizard.Validate: a runnable roster needs an
// orchestrator, a reviewer, and a worker (a worker can't self-accept, so the
// reviewer can't also be the only one doing the work).
function validate(bindings: SetupBinding[]): string[] {
  const has = (r: Role) => bindings.some((b) => b.roles.includes(r));
  const warns: string[] = [];
  if (!has("orchestrator")) warns.push("缺 orchestrator 座席 — orchestrate 需要一个来驱动合并");
  if (!has("reviewer")) warns.push("缺 reviewer 座席 — 任务无法被 accept（只有 reviewer 能 accept）");
  if (!has("worker")) warns.push("缺 worker 座席 — worker 不能自接受，orchestrator/reviewer 不能既干活又评审；加一个 worker");
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

  const warnings = bindings ? validate(bindings) : [];
  const commands = bindings ? applyCommands(bindings) : "";
  const ready = bindings != null && bindings.length > 0 && warnings.length === 0;

  return (
    <div className="flex-1 overflow-y-auto px-6 py-5 view-enter" data-testid="setup-view">
      <div className="mb-1 flex items-center gap-3">
        <h2 className="text-[15px] font-[650] text-[var(--color-text-1)]">Setup · 把注册的 agent 配进项目</h2>
        {loading && <Spinner size="sm" />}
        {ready && (
          <span className="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10.5px] font-medium"
            style={{ color: "var(--color-success)", background: "color-mix(in srgb, var(--color-success) 12%, transparent)" }}>
            <Icon name="state-accepted" size={12} /> 角色齐备
          </span>
        )}
      </div>
      <p className="mb-4 text-[12px] text-[var(--color-text-3)]">
        从你机器上已注册的 agent 建议座席分工。改好角色后，复制下面的命令在项目根执行。
      </p>

      {error && (
        <Alert tone="danger" title="加载失败" onRetry={load}>
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
          title="还没有注册的 agent"
          hint="先去 Ops 视图扫描并注册 agent（或 CLI: pactify agent register <kind>），再回来配座席。"
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
                {/* kind icon tile — identifies the agent at a glance */}
                <span className="grid h-8 w-8 shrink-0 place-items-center rounded-lg bg-[var(--color-bg-inset)]">
                  <Icon name={kindIcon(b.kind)} size={18} />
                </span>
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
            <ConfigSection
              label="应用命令"
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
                  {copied ? "已复制 ✓" : "复制"}
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
