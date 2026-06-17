import { useEffect, useState } from "react";
import { getAgents, registerAgent, unregisterAgent, pruneSessions, type AgentRow } from "../../lib/api";
import { Button } from "../ui/Button";
import { Badge } from "../ui/Badge";
import { Alert } from "../ui/Alert";
import { EmptyState } from "../ui/EmptyState";
import { AgentLogo } from "../../lib/agentLogos";

// AgentRoster — the Settings agent-management panel. Three jobs (backlog
// 2026-06-15):
//   1. one-click auto-scan — GET /api/agents re-probes installed binaries on
//      every request, so "Scan" is a refetch that surfaces what's on the machine;
//   2. one-click register for any detected-but-unregistered agent;
//   3. manual add — register a SUPPORTED kind that wasn't auto-detected (to
//      pre-seat a project, or for a non-standard install). Custom binary paths
//      are a follow-up (the register endpoint doesn't take a path yet).
// Author-gated for the mutating actions; Scan itself is read-only.
export function AgentRoster({
  author,
  refreshKey,
  onChanged,
}: {
  author: boolean;
  refreshKey?: number;
  onChanged?: () => void;
}) {
  const [rows, setRows] = useState<AgentRow[] | null>(null);
  const [error, setError] = useState("");
  const [scanning, setScanning] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const [pruneBusy, setPruneBusy] = useState<string | null>(null);
  const [pruneResult, setPruneResult] = useState<{ kind: string; skipped: boolean; output: string } | null>(null);
  const [showManual, setShowManual] = useState(false);

  function load(scan = false) {
    if (scan) setScanning(true);
    return getAgents()
      .then((r) => {
        setRows(r);
        setError("");
      })
      .catch(() => setError("Failed to scan agents"))
      .finally(() => setScanning(false));
  }

  useEffect(() => {
    load();
  }, [refreshKey]); // eslint-disable-line react-hooks/exhaustive-deps

  async function toggle(kind: string, registered: boolean) {
    if (!author) return;
    setBusy(kind);
    setRows((prev) => (prev ?? []).map((a) => (a.kind === kind ? { ...a, registered: !registered } : a)));
    try {
      if (registered) {
        await unregisterAgent(kind);
      } else {
        await registerAgent(kind);
      }
      onChanged?.();
    } catch {
      await load();
    } finally {
      setBusy(null);
    }
  }

  async function handlePrune(kind: string) {
    if (!author) return;
    setPruneBusy(kind);
    setPruneResult(null);
    try {
      const r = await pruneSessions(kind);
      setPruneResult(r);
    } catch {
      setPruneResult({ kind, skipped: false, output: "Failed to prune sessions" });
    } finally {
      setPruneBusy(null);
    }
  }

  const installed = (rows ?? []).filter((a) => a.installed);
  const manual = (rows ?? []).filter((a) => !a.installed && !a.registered);

  return (
    <section data-testid="ops-agent-roster" className="mb-4">
      <div className="mb-2 flex items-center gap-2">
        <h2 className="text-[10px] font-semibold uppercase text-[var(--color-text-3)]">
          Agents · scan &amp; register
        </h2>
        <Button size="sm" variant="ghost" data-testid="agent-scan" loading={scanning} onClick={() => load(true)}>
          {scanning ? "Scanning…" : "Scan"}
        </Button>
        {rows && (
          <span className="text-[10.5px] text-[var(--color-text-3)]">
            {installed.length} installed on this machine
          </span>
        )}
      </div>

      {error && (
        <Alert tone="danger" onRetry={() => load()}>
          {error}
        </Alert>
      )}

      {!error && rows && (
        <div className="flex flex-col gap-2">
          {installed.length === 0 && (
            <EmptyState
              title="No supported agents detected"
              hint="Install a supported agent CLI (e.g. claude, opencode, gemini, codex), then Scan — or add one manually below."
            />
          )}

          {installed.map((a) => (
            <RosterRow
              key={a.kind}
              row={a}
              author={author}
              busy={busy === a.kind}
              pruneBusy={pruneBusy === a.kind}
              pruneResult={pruneResult?.kind === a.kind ? pruneResult : null}
              onToggle={() => toggle(a.kind, a.registered)}
              onPrune={() => handlePrune(a.kind)}
            />
          ))}

          {manual.length > 0 && (
            <div className="mt-1">
              <button
                type="button"
                data-testid="manual-add-toggle"
                onClick={() => setShowManual((s) => !s)}
                className="press text-[11px] text-[var(--color-text-3)] hover:text-[var(--color-text-1)]"
              >
                {showManual ? "▾" : "▸"} Add manually · {manual.length} supported but not detected
              </button>
              {showManual && (
                <div className="mt-2 flex flex-col gap-2">
                  <p className="text-[10.5px] text-[var(--color-text-3)]">
                    Register a supported agent that wasn&apos;t auto-detected — to pre-seat a project, or for a
                    non-standard install.
                  </p>
                  {manual.map((a) => (
                    <RosterRow
                      key={a.kind}
                      row={a}
                      author={author}
                      busy={busy === a.kind}
                      pruneBusy={pruneBusy === a.kind}
                      pruneResult={pruneResult?.kind === a.kind ? pruneResult : null}
                      manual
                      onToggle={() => toggle(a.kind, a.registered)}
                      onPrune={() => handlePrune(a.kind)}
                    />
                  ))}
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </section>
  );
}

function RosterRow({
  row,
  author,
  busy,
  pruneBusy,
  pruneResult,
  manual = false,
  onToggle,
  onPrune,
}: {
  row: AgentRow;
  author: boolean;
  busy: boolean;
  pruneBusy?: boolean;
  pruneResult?: { skipped: boolean; output: string } | null;
  manual?: boolean;
  onToggle: () => void;
  onPrune?: () => void;
}) {
  return (
    <div
      data-testid={`roster-${row.kind}`}
      className="flex items-center gap-3 rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-3 py-2.5"
    >
      <AgentLogo kind={row.kind} size={26} />
      <div className="flex min-w-0 flex-col">
        <span className="mono text-[12px] font-[650] leading-tight text-[var(--color-text-1)]">
          {row.kind}
          {row.label ? ` · ${row.label}` : ""}
        </span>
        {row.detail && (
          <span className="truncate text-[10.5px] leading-tight text-[var(--color-text-3)]">{row.detail}</span>
        )}
      </div>
      <Badge color={row.installed ? "success" : "warn"}>{row.installed ? "Installed" : "Not detected"}</Badge>
      <div className="ml-auto flex items-center gap-1.5">
        {row.registered ? (
          <>
            <Badge color="role-dev">Registered</Badge>
            {author && (
              <Button size="sm" variant="ghost" loading={busy} onClick={onToggle}>
                Remove
              </Button>
            )}
          </>
        ) : (
          <Button
            size="sm"
            variant={manual ? "ghost" : "primary"}
            disabled={!author}
            loading={busy}
            onClick={onToggle}
          >
            {manual ? "Register anyway" : "Register"}
          </Button>
        )}
        {author && (
          <Button size="sm" variant="ghost" loading={pruneBusy} onClick={onPrune}>
            Prune sessions
          </Button>
        )}
      </div>
      {pruneResult && (
        <div className="col-span-full mt-2 rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-raised)] px-2.5 py-1.5 text-[10.5px] text-[var(--color-text-2)]">
          {pruneResult.skipped ? "Nothing to prune" : <pre className="whitespace-pre-wrap [overflow-wrap:anywhere]">{pruneResult.output}</pre>}
        </div>
      )}
    </div>
  );
}
