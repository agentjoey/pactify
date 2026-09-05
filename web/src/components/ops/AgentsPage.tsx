import { useCallback, useEffect, useState } from "react";
import {
  getAgents,
  getAgentVersions,
  getAgentConfig,
  testAgent,
  registerAgent,
  unregisterAgent,
  pruneSessions,
  type AgentRow,
  type AgentCheck,
  type AgentConfig as AgentConfigT,
} from "../../lib/api";
import { AgentLogo } from "../../lib/agentLogos";
import { Alert } from "../ui/Alert";
import { Button } from "../ui/Button";
import { EmptyState } from "../ui/EmptyState";
import { Spinner } from "../ui/Spinner";
import { AgentConfigBody } from "./AgentConfig";

// AgentsPage merges what used to be two settings pages — "Registered agents"
// (scan / register / remove) and "Agent configs" (model + posture) — into one.
//
// The split was the source of the confusion this change fixes: Scan and the
// installed/available grouping already lived on the first page, while the pain
// (seven permanently-expanded config rows) lived on the second, and neither
// page could answer "is this agent actually usable right now".
//
// Rows are collapsed by default and MULTIPLE may be open at once: the original
// complaint was page length, which collapsing solves, while an accordion would
// fight anyone comparing two agents' models.

// label 去掉 "cli <kind>: " 前缀；detail 已经以该方面开头时不再重复它
// （否则渲染成 "transport transport: acp available"）。
function label(c: AgentCheck): string {
  const aspect = c.name.replace(/^cli [^:]+:\s*/, "");
  const detail = c.detail.trim();
  return detail.toLowerCase().startsWith(aspect.toLowerCase()) ? detail : `${aspect} ${detail}`;
}

type TestState =
  | { phase: "idle" }
  | { phase: "running" }
  | { phase: "done"; ok: boolean; checks: AgentCheck[] }
  | { phase: "error"; message: string };

export function AgentsPage({ author }: { author?: boolean }) {
  const [rows, setRows] = useState<AgentRow[] | null>(null);
  const [versions, setVersions] = useState<Record<string, string>>({});
  const [configs, setConfigs] = useState<Record<string, AgentConfigT>>({});
  const [error, setError] = useState("");
  const [scanning, setScanning] = useState(false);
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const [tests, setTests] = useState<Record<string, TestState>>({});
  const [busy, setBusy] = useState<string | null>(null);
  const [actionErr, setActionErr] = useState("");

  const load = useCallback((rescan = false) => {
    if (rescan) setScanning(true);
    getAgents()
      .then((r) => {
        setRows(r);
        setError("");
        // Configs feed the collapsed row's model chip. Fetched per registered
        // kind and handed to the expanded body as `initial`, so opening a row
        // does not re-request what we already have.
        for (const a of r.filter((x) => x.registered)) {
          getAgentConfig(a.kind)
            .then((c) => setConfigs((m) => ({ ...m, [a.kind]: c })))
            .catch(() => {});
        }
      })
      .catch(() => setError("Failed to load agents"))
      .finally(() => setScanning(false));
    // Versions are a separate, slower probe (~572ms for the slowest CLI). They
    // arrive after the list and must never gate it.
    getAgentVersions()
      .then((v) => setVersions(v.versions ?? {}))
      .catch(() => {});
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const runTest = (kind: string) => {
    setTests((t) => ({ ...t, [kind]: { phase: "running" } }));
    testAgent(kind)
      .then((r) => setTests((t) => ({ ...t, [kind]: { phase: "done", ok: r.ok, checks: r.checks } })))
      .catch((e) =>
        setTests((t) => ({
          ...t,
          [kind]: { phase: "error", message: e instanceof Error ? e.message : "test failed" },
        })),
      );
  };

  const toggleRegister = async (kind: string, registered: boolean) => {
    setBusy(kind);
    setActionErr("");
    try {
      if (registered) await unregisterAgent(kind);
      else await registerAgent(kind);
      load();
    } catch (e) {
      setActionErr(e instanceof Error ? e.message : "action failed");
    } finally {
      setBusy(null);
    }
  };

  const prune = async (kind: string) => {
    setBusy(kind);
    setActionErr("");
    try {
      await pruneSessions(kind);
    } catch (e) {
      setActionErr(e instanceof Error ? e.message : "prune failed");
    } finally {
      setBusy(null);
    }
  };

  if (error) {
    return (
      <div data-testid="agents-error">
        <Alert tone="danger" onRetry={() => load()}>
          {error}
        </Alert>
      </div>
    );
  }
  if (!rows) return <Spinner size="xs" />;

  const installed = rows.filter((r) => r.installed);
  const available = rows.filter((r) => !r.installed);
  const registeredCount = installed.filter((r) => r.registered).length;

  const row = (a: AgentRow) => (
    <AgentRowCard
      key={a.kind}
      row={a}
      version={versions[a.kind]}
      config={configs[a.kind]}
      open={!!open[a.kind]}
      author={author}
      busy={busy === a.kind}
      test={tests[a.kind] ?? { phase: "idle" }}
      onToggleOpen={() => setOpen((o) => ({ ...o, [a.kind]: !o[a.kind] }))}
      onTest={() => runTest(a.kind)}
      onRegister={() => toggleRegister(a.kind, a.registered)}
      onPrune={() => prune(a.kind)}
    />
  );

  return (
    <section className="mb-4">
      {actionErr && (
        <div className="mb-2">
          <Alert tone="danger">{actionErr}</Alert>
        </div>
      )}

      <div className="mb-2 flex items-center gap-3">
        <h2 className="font-mono text-[10.5px] uppercase tracking-[0.12em] text-[var(--color-text-2)]">
          Installed CLIs
        </h2>
        <span className="font-mono text-[11px] text-[var(--color-text-2)]">
          {installed.length} · {registeredCount} registered
        </span>
        <span className="ml-auto" />
        <Button size="sm" variant="ghost" data-testid="agents-rescan" loading={scanning} onClick={() => load(true)}>
          {scanning ? "Rescanning…" : "↻ Rescan"}
        </Button>
      </div>

      <div data-testid="agents-installed" className="flex flex-col gap-2">
        {installed.length === 0 ? (
          <EmptyState
            title="No supported agents detected"
            hint="Install an agent CLI (claude, opencode, gemini, codex …), then Rescan."
          />
        ) : (
          installed.map(row)
        )}
      </div>

      <div className="mb-2 mt-6 flex items-center gap-3">
        <h2 className="font-mono text-[10.5px] uppercase tracking-[0.12em] text-[var(--color-text-2)]">
          Available CLIs
        </h2>
        <span className="font-mono text-[11px] text-[var(--color-text-2)]">
          {available.length} not detected on this machine
        </span>
      </div>
      <div data-testid="agents-available" className="flex flex-col gap-2">
        {available.length === 0 ? (
          <p className="font-mono text-[11px] text-[var(--color-text-2)]">
            every supported CLI is installed here
          </p>
        ) : (
          available.map(row)
        )}
      </div>
    </section>
  );
}

function AgentRowCard({
  row,
  version,
  config,
  open,
  author,
  busy,
  test,
  onToggleOpen,
  onTest,
  onRegister,
  onPrune,
}: {
  row: AgentRow;
  version?: string;
  config?: AgentConfigT;
  open: boolean;
  author?: boolean;
  busy: boolean;
  test: TestState;
  onToggleOpen: () => void;
  onTest: () => void;
  onRegister: () => void;
  onPrune: () => void;
}) {
  const kind = row.kind;
  const expandable = row.installed && row.registered;
  const bodyId = `agent-body-${kind}`;

  return (
    <div
      data-testid={`agent-row-${kind}`}
      className="overflow-hidden rounded-xl border border-[rgba(255,255,255,0.08)] bg-[var(--color-bg-surface)]"
    >
      <div className="flex flex-wrap items-center gap-3 px-3.5 py-2.5">
        {/* Disclosure is its own button wrapping chevron+name; the action
            buttons are siblings, never nested inside it. Whole-row click was
            rejected: it is not keyboard reachable and it puts Register one
            stray click away. */}
        {expandable ? (
          <button
            type="button"
            data-testid={`agent-disclosure-${kind}`}
            aria-expanded={open}
            aria-controls={bodyId}
            onClick={onToggleOpen}
            className="press flex items-center gap-3 rounded-md text-left"
          >
            <span aria-hidden className="w-2.5 font-mono text-[10px] text-[var(--color-text-2)]">
              {open ? "▾" : "▸"}
            </span>
            <AgentLogo kind={kind} size={22} />
            <span className="font-mono text-[12.5px] font-semibold text-[var(--color-text-1)]">{kind}</span>
          </button>
        ) : (
          <span className="flex items-center gap-3">
            <span className="w-2.5" />
            <AgentLogo kind={kind} size={22} />
            <span className="font-mono text-[12.5px] font-semibold text-[var(--color-text-2)]">{kind}</span>
          </span>
        )}

        {version && <span className="font-mono text-[11px] text-[var(--color-text-2)]">{version}</span>}
        {config?.model && (
          <span className="rounded border border-[rgba(255,255,255,0.07)] bg-[var(--bg-input)] px-1.5 py-0.5 font-mono text-[11px] text-[var(--color-text-2)]">
            {config.model}
          </span>
        )}
        {!row.installed && (
          <span className="font-mono text-[11px] text-[var(--color-text-2)]">{row.detail}</span>
        )}
        {row.installed && !row.registered && (
          <span className="font-mono text-[11px] text-[var(--color-text-2)]">
            installed · not registered
          </span>
        )}

        <span className="flex flex-wrap items-center gap-2 sm:ml-auto">
          {row.installed && (
            <Button size="sm" variant="ghost" data-testid={`agent-test-${kind}`} onClick={onTest}>
              {test.phase === "running" ? "Testing…" : "Test"}
            </Button>
          )}
          {author && row.installed && (
            <Button
              size="sm"
              variant="ghost"
              data-testid={row.registered ? `agent-remove-${kind}` : `agent-register-${kind}`}
              loading={busy}
              onClick={onRegister}
            >
              {row.registered ? "Remove…" : "Register"}
            </Button>
          )}
        </span>
      </div>

      {(test.phase === "done" || test.phase === "error") && (
        <div
          data-testid={`agent-test-result-${kind}`}
          role="status"
          aria-live="polite"
          className="flex flex-col gap-1.5 border-t border-[rgba(255,255,255,0.07)] bg-[var(--bg-code)] px-3.5 py-2 sm:flex-row sm:flex-wrap sm:gap-4"
        >
          {test.phase === "error" ? (
            <span className="font-mono text-[11px] text-[var(--color-danger)]">
              ✕ {test.message}
            </span>
          ) : (
            test.checks.map((c) => (
              <span key={c.name} className="font-mono text-[11px] text-[var(--color-text-2)]">
                {/* Mark carries the meaning; colour only reinforces it, so a
                    failed layer is identifiable without colour vision. */}
                <span className={c.ok ? "text-[var(--color-success)]" : "text-[var(--color-danger)]"}>
                  {c.ok ? "✓" : "✕"}
                </span>{" "}
                {label(c)}
              </span>
            ))
          )}
        </div>
      )}

      {open && expandable && (
        <div id={bodyId} className="border-t border-[rgba(255,255,255,0.07)]">
          <AgentConfigBody kind={kind} initial={config} />
          {author && (
            <div className="flex items-center gap-2 border-t border-[rgba(255,255,255,0.07)] px-3.5 py-2">
              <span className="font-mono text-[10px] uppercase tracking-[0.08em] text-[var(--color-text-2)]">
                registration on this machine
              </span>
              <span className="ml-auto" />
              <Button size="sm" variant="ghost" data-testid={`agent-prune-${kind}`} loading={busy} onClick={onPrune}>
                Prune sessions…
              </Button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
