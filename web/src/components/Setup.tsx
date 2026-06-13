import { useEffect, useState } from "react";
import type { SetupBinding } from "../lib/types";
import { getSetupSuggest } from "../lib/api";
import { Badge } from "./ui/Badge";
import { Button } from "./ui/Button";
import { Alert } from "./ui/Alert";
import { EmptyState } from "./ui/EmptyState";
import { Spinner } from "./ui/Spinner";

// Setup (#1) — the entry-point view bridging "I registered my agents" to "this
// project can do work". Reads the proposed seat roster from the machine's
// registered agents, lets the user toggle roles per seat, recomputes role-gap
// warnings live, and generates the exact `init` + `agent add` commands to apply
// the roster. Apply is copy-the-commands (zero side effects) in this cut;
// one-click apply via HTTP (which would mutate .pact) is a deliberate follow-up.

const ALL_ROLES = ["orchestrator", "reviewer", "worker"] as const;
type Role = (typeof ALL_ROLES)[number];

// entryFor mirrors the CLI's kind→entry-file convention for the init command.
function entryFor(kind: string): string {
  if (kind.startsWith("claude")) return "CLAUDE.md";
  if (kind.startsWith("gemini")) return "GEMINI.md";
  return "AGENTS.md";
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

  return (
    <div className="flex-1 overflow-y-auto px-6 py-5" data-testid="setup-view">
      <div className="mb-1 flex items-center gap-3">
        <h2 className="text-[15px] font-[650] text-[var(--color-text-1)]">Setup · 把注册的 agent 配进项目</h2>
        {loading && <Spinner size="sm" />}
      </div>
      <p className="mb-4 text-[12px] text-[var(--color-text-3)]">
        从你机器上已注册的 agent 建议座席分工。改好角色后，复制下面的命令在项目根执行。
      </p>

      {error && (
        <Alert tone="danger" title="加载失败" onRetry={load}>
          {error}
        </Alert>
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
            {bindings.map((b) => (
              <div
                key={b.seat}
                data-testid="setup-row"
                className="flex flex-wrap items-center gap-3 rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-3 py-2.5 hover-lift"
              >
                <span className="mono text-[12px] font-medium text-[var(--color-text-1)] w-24">{b.seat}</span>
                <span className="text-[11px] text-[var(--color-text-2)] w-28">{b.kind}</span>
                <Badge color={b.drivable ? "role-dev" : "role-design"}>
                  {b.drivable ? "drivable" : "manual"}
                </Badge>
                <div className="flex items-center gap-1.5">
                  {ALL_ROLES.map((r) => {
                    const on = b.roles.includes(r);
                    return (
                      <button
                        key={r}
                        type="button"
                        data-testid={`role-${b.seat}-${r}`}
                        aria-pressed={on}
                        onClick={() => toggleRole(b.seat, r)}
                        className={[
                          "rounded px-2 py-0.5 text-[10.5px] transition-colors duration-[var(--motion-micro)] outline-none focus-visible:ring-2",
                          on
                            ? "bg-[color-mix(in_srgb,var(--color-role-design)_22%,transparent)] text-[var(--color-text-1)]"
                            : "bg-[var(--color-bg-raised)] text-[var(--color-text-3)] hover:text-[var(--color-text-2)]",
                        ].join(" ")}
                      >
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

          <div className="mt-4">
            <div className="mb-1.5 flex items-center gap-2">
              <span className="text-[11px] uppercase tracking-[.5px] text-[var(--color-text-3)]">应用命令</span>
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
            </div>
            <pre
              data-testid="setup-commands"
              className="whitespace-pre-wrap rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-raised)] px-3 py-2.5 mono text-[11px] text-[var(--color-text-2)] [overflow-wrap:anywhere]"
            >
              {commands}
            </pre>
          </div>
        </div>
      )}
    </div>
  );
}
