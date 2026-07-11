import { useEffect, useRef, useState } from "react";
import type { CockpitEvent } from "../lib/api";
import type { Seat, State, Feature } from "../lib/types";
import { useDataSource } from "../lib/datasource";
import { COCKPIT_STATUS_POLL_MS } from "../lib/constants";
import { MetricStrip } from "./ui/MetricStrip";
import { MiniPipeline } from "./ui/MiniPipeline";

type PendingApproval = {
  id: string;
  kind: string;
  toolName: string;
  rawInput?: unknown;
  risk?: string;
};

type StreamItem =
  | { id: number; type: "turn"; time: string }
  | { id: number; type: "message"; role: "user" | "assistant"; text: string }
  | {
      id: number;
      type: "tool";
      name: string;
      phase?: string;
      text?: string;
      exit?: number;
      expanded: boolean;
    }
  | { id: number; type: "system"; kind: string; text: string }
  | { id: number; type: "diff"; path: string; patch: string; expanded: boolean }
  | {
      id: number;
      type: "plan";
      title: string;
      done: number;
      total: number;
      items: { text: string; state: "done" | "current" | "pending" }[];
    }
  | { id: number; type: "streaming" };

const COCKPIT_CAPABLE_KINDS = new Set([
  "claude-code",
  "codex-cli",
  "kimi-cli",
  "gemini-cli",
  "opencode",
]);

function formatTime(d: Date): string {
  return `${d.getHours().toString().padStart(2, "0")}:${d.getMinutes().toString().padStart(2, "0")}`;
}

function formatRawInput(rawInput: unknown): string {
  const text = JSON.stringify(rawInput, null, 1);
  if (text.length > 600) return text.slice(0, 600) + "…";
  return text;
}

function fmtTokens(n: number): string {
  if (n < 1000) return String(n);
  return `${(n / 1000).toFixed(1).replace(".0", "")}k`;
}

function fmtCost(usd: number): string {
  return `~$${usd.toFixed(2)}`;
}

function isRateLimit(e: unknown): boolean {
  const msg = e instanceof Error ? e.message : String(e);
  return /429|rate|too many/i.test(msg);
}

function setErrorMessage(
  e: unknown,
  setError: (m: string) => void,
  onNotify?: (m: string, kind?: "error") => void,
): void {
  if (isRateLimit(e)) {
    const m = "Rate limited — please wait a moment.";
    setError(m);
    onNotify?.("Rate limited (429)", "error");
    return;
  }
  setError(e instanceof Error ? e.message : String(e));
}

function monogram(name: string): string {
  return name.slice(0, 2).toLowerCase();
}

function avatarGradient(seatId: string): string {
  const id = seatId.toLowerCase();
  if (id.startsWith("cl")) return "linear-gradient(135deg,#ffd479,#e0a93a)";
  return "linear-gradient(135deg,#6ee7a0,#39b97a)";
}

function roleChip(roles: string[]): { label: string; color: string } {
  if (roles.includes("orchestrator")) return { label: "orchestrator", color: "var(--proj)" };
  if (roles.includes("reviewer")) return { label: "reviewer", color: "var(--info)" };
  return { label: "worker", color: "var(--dev)" };
}

function currentFeature(state: State): Feature | null {
  if (!state.features.length) return null;
  const active = state.features.find((f) => f.tasks.some((t) => t.status === "in_progress"));
  if (active) return active;
  const nonShipped = state.features.find((f) => f.status !== "shipped");
  return nonShipped ?? state.features[0];
}

function extractExit(text?: string): number | undefined {
  if (!text) return undefined;
  const m = text.match(/(?:exit\s*|=)\s*(-?\d+)/i);
  if (m) return Number(m[1]);
  return undefined;
}

function extractUsage(ev: CockpitEvent): { tokens?: number; cost?: number; iter?: number; model?: string } {
  const u = ev.usage;
  if (!u || typeof u !== "object") return {};
  const out: { tokens?: number; cost?: number; iter?: number; model?: string } = {};
  const getNum = (...keys: string[]) => {
    for (const k of keys) {
      const v = (u as Record<string, unknown>)[k];
      if (typeof v === "number") return v;
      if (typeof v === "string") {
        const n = Number(v);
        if (!Number.isNaN(n)) return n;
      }
    }
    return undefined;
  };
  out.tokens = getNum("tokens", "total_tokens", "input_tokens", "output_tokens");
  out.cost = getNum("cost", "usd", "total_cost");
  out.iter = getNum("iter", "iterations", "iteration");
  const model = (u as Record<string, unknown>).model;
  if (typeof model === "string") out.model = model;
  return out;
}

function isDangerRisk(risk?: string): boolean {
  return risk === "exec" || risk === "strong" || risk === "network" || risk === "destructive";
}

export function CockpitPanel({
  project,
  seat,
  agents,
  state,
  onClose,
  onSeatChange,
  onNotify,
  onOpenBoard,
  viewMode,
}: {
  project: string;
  seat: string;
  agents: Seat[];
  state?: State;
  onClose?: () => void;
  onSeatChange?: (seat: string) => void;
  onNotify?: (msg: string, kind?: "error") => void;
  onOpenBoard?: (taskId?: string) => void;
  viewMode?: boolean;
}) {
  const src = useDataSource();
  const [stream, setStream] = useState<StreamItem[]>([]);
  const [pending, setPending] = useState<PendingApproval[]>([]);
  const [confirmAllow, setConfirmAllow] = useState<string | null>(null);
  const [input, setInput] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [threadId, setThreadId] = useState("");
  const [capable, setCapable] = useState(true);
  const [capableReason, setCapableReason] = useState("");
  const [resumable, setResumable] = useState(false);
  const [runningTool, setRunningTool] = useState<string | null>(null);
  const [statusFailures, setStatusFailures] = useState(0);
  const [streamKey, setStreamKey] = useState(0);
  const [model, setModel] = useState("");
  const [usage, setUsage] = useState({ tokens: 0, cost: 0, iter: 0 });
  const streamRef = useRef<HTMLDivElement>(null);
  const itemId = useRef(0);
  const seatInitRef = useRef(true);
  const statusUnavailable = statusFailures >= 3;

  const seatAgent = agents.find((a) => a.id === seat);
  const seatKind = seatAgent?.kind;
  const seatRoles = seatAgent?.roles ?? [];
  const seatCapableByRoster = seatKind ? COCKPIT_CAPABLE_KINDS.has(seatKind) : true;
  const effectiveCapable = capable && seatCapableByRoster;
  const role = roleChip(seatRoles);

  const loadStatus = async () => {
    if (!src.cockpitStatus) return;
    try {
      const st = await src.cockpitStatus(project, seat);
      setPending(st.pending);
      setThreadId(st.threadId ?? "");
      setCapable(st.capable ?? true);
      setCapableReason(st.reason ?? "");
      setResumable(st.resumable ?? false);
      setStatusFailures(0);
    } catch {
      setStatusFailures((f) => f + 1);
    }
  };

  // Clear stale stream content when the seat/project changes.
  useEffect(() => {
    if (seatInitRef.current) {
      seatInitRef.current = false;
      return;
    }
    setStream([]);
    setPending([]);
    setInput("");
    setError("");
    setRunningTool(null);
    setThreadId("");
    setModel("");
    setUsage({ tokens: 0, cost: 0, iter: 0 });
    setResumable(false);
    setStreamKey((k) => k + 1);
  }, [project, seat]);

  useEffect(() => {
    loadStatus();
    const t = setInterval(loadStatus, COCKPIT_STATUS_POLL_MS);
    return () => clearInterval(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [project, seat, src.cockpitStatus]);

  useEffect(() => {
    if (!src.cockpitSubscribe) return;
    const off = src.cockpitSubscribe(project, seat, (ev) => {
      try {
        if (ev.kind === "session" && ev.threadId) {
          setThreadId(ev.threadId);
          return;
        }
        if (ev.kind === "status") {
          if (typeof ev.threadId === "string") setThreadId(ev.threadId);
          if (typeof ev.resumable === "boolean") setResumable(ev.resumable);
          if (typeof ev.capable === "boolean") setCapable(ev.capable);
          if (typeof ev.reason === "string") setCapableReason(ev.reason);
          if (ev.pending) setPending(ev.pending);
          return;
        }
        if (ev.kind === "tool" && ev.tool) {
          if (ev.tool.phase === "start") {
            setRunningTool(ev.tool.name);
            itemId.current += 1;
            setStream((prev) => [
              ...prev,
              {
                id: itemId.current,
                type: "tool",
                name: ev.tool!.name,
                phase: "start",
                expanded: true,
              },
            ]);
          } else {
            setRunningTool(null);
            setStream((prev) => {
              const next = [...prev];
              for (let i = next.length - 1; i >= 0; i--) {
                const it = next[i];
                if (it.type === "tool" && it.name === ev.tool!.name) {
                  next[i] = {
                    ...it,
                    phase: ev.tool!.phase,
                    text: ev.tool!.text,
                    exit: extractExit(ev.tool!.text),
                  };
                  break;
                }
              }
              return next;
            });
          }
        } else if (ev.kind === "state" && ev.state === "turn_completed") {
          setRunningTool(null);
        } else if (ev.kind === "error") {
          setRunningTool(null);
          itemId.current += 1;
          setStream((prev) => [
            ...prev,
            { id: itemId.current, type: "system", kind: "error", text: ev.err ?? ev.text ?? "error" },
          ]);
        }
        if (ev.kind === "message" && typeof ev.text === "string") {
          const delta = ev.text;
          setStream((prev) => {
            const last = prev[prev.length - 1];
            if (last?.type === "message" && last.role === "assistant") {
              const next = [...prev];
              next[next.length - 1] = { ...last, text: last.text + delta };
              return next;
            }
            itemId.current += 1;
            return [...prev, { id: itemId.current, type: "message", role: "assistant", text: delta }];
          });
        } else if (ev.kind === "state" || ev.kind === "usage") {
          // usage/state handled below; avoid double system rows for state.
        } else if (ev.kind !== "tool" && ev.kind !== "error") {
          itemId.current += 1;
          setStream((prev) => [
            ...prev,
            { id: itemId.current, type: "system", kind: ev.kind, text: ev.text ?? "" },
          ]);
        }
        if (ev.kind === "usage") {
          const u = extractUsage(ev);
          setUsage((prev) => ({
            tokens: prev.tokens + (u.tokens ?? 0),
            cost: prev.cost + (u.cost ?? 0),
            iter: Math.max(prev.iter, u.iter ?? prev.iter),
          }));
          if (u.model) setModel(u.model);
        }
        if (ev.kind === "state") {
          itemId.current += 1;
          setStream((prev) => [
            ...prev,
            {
              id: itemId.current,
              type: "system",
              kind: "state",
              text: typeof ev.state === "string" ? ev.state : JSON.stringify(ev.state),
            },
          ]);
        }
        // Components ready for diff/plan events when the stream begins emitting them.
        if ((ev as unknown as Record<string, unknown>).diff) {
          const diff = (ev as unknown as Record<string, unknown>).diff;
          itemId.current += 1;
          setStream((prev) => [
            ...prev,
            {
              id: itemId.current,
              type: "diff",
              path: typeof diff === "string" ? "change" : String((diff as Record<string, unknown>)?.path ?? "change"),
              patch: typeof diff === "string" ? diff : String((diff as Record<string, unknown>)?.patch ?? ""),
              expanded: true,
            },
          ]);
        }
        if ((ev as unknown as Record<string, unknown>).plan) {
          const plan = (ev as unknown as Record<string, unknown>).plan as {
            title?: string;
            done?: number;
            total?: number;
            items?: { text?: string; state?: string }[];
          };
          itemId.current += 1;
          setStream((prev) => [
            ...prev,
            {
              id: itemId.current,
              type: "plan",
              title: plan?.title ?? "Plan updated",
              done: plan?.done ?? 0,
              total: plan?.total ?? 0,
              items: (plan?.items ?? []).map((it) => ({
                text: it.text ?? "",
                state: (it.state as "done" | "current" | "pending") ?? "pending",
              })),
            },
          ]);
        }
        if (ev.kind === "tool" || ev.kind === "state") {
          loadStatus();
        }
      } catch {
        // ignore malformed frames
      }
    });
    return () => off();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [project, seat, src.cockpitSubscribe, streamKey]);

  useEffect(() => {
    if (viewMode) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose?.();
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [onClose, viewMode]);

  // Auto-scroll to the bottom of the stream unless the user has scrolled up.
  useEffect(() => {
    const el = streamRef.current;
    if (!el) return;
    const distance = el.scrollHeight - el.scrollTop - el.clientHeight;
    if (distance <= 80) {
      el.scrollTop = el.scrollHeight;
    }
  }, [stream, pending, runningTool]);

  const send = async () => {
    const text = input.trim();
    if (!text || !src.cockpitPrompt) return;
    itemId.current += 1;
    const turnId = itemId.current;
    itemId.current += 1;
    const msgId = itemId.current;
    setStream((prev) => [
      ...prev,
      { id: turnId, type: "turn", time: formatTime(new Date()) },
      { id: msgId, type: "message", role: "user", text },
    ]);
    setInput("");
    setBusy(true);
    setError("");
    try {
      const resp = await src.cockpitPrompt(project, seat, text);
      if (resp.threadId) setThreadId(resp.threadId);
    } catch (e) {
      setErrorMessage(e, setError, onNotify);
    } finally {
      setBusy(false);
    }
  };

  const cancel = async () => {
    if (!src.cockpitCancel) return;
    try {
      await src.cockpitCancel(project, seat);
    } catch (e) {
      setErrorMessage(e, setError, onNotify);
    }
  };

  const respond = async (approvalId: string, decision: "allow" | "deny") => {
    if (!src.cockpitRespond) return;
    try {
      await src.cockpitRespond(project, seat, approvalId, decision);
      await loadStatus();
    } catch (e) {
      setErrorMessage(e, setError, onNotify);
    }
  };

  const resume = async () => {
    if (!src.cockpitResume) return;
    setBusy(true);
    setError("");
    try {
      const resp = await src.cockpitResume(project, seat);
      if (resp.threadId) setThreadId(resp.threadId);
      setResumable(false);
      setStreamKey((k) => k + 1);
      await loadStatus();
    } catch (e) {
      setErrorMessage(e, setError, onNotify);
    } finally {
      setBusy(false);
    }
  };

  // Exec-risk approvals require a two-step Allow confirmation.
  useEffect(() => {
    if (!confirmAllow) return;
    const t = setTimeout(() => setConfirmAllow(null), 3000);
    return () => clearTimeout(t);
  }, [confirmAllow]);

  useEffect(() => {
    if (!confirmAllow) return;
    const onDown = (e: MouseEvent) => {
      const card = (e.target as HTMLElement).closest('[data-testid="cockpit-approval"]');
      if (!card) setConfirmAllow(null);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [confirmAllow]);

  useEffect(() => {
    if (confirmAllow && !pending.some((p) => p.id === confirmAllow)) {
      setConfirmAllow(null);
    }
  }, [pending, confirmAllow]);

  const inputEnabled = !busy && effectiveCapable && !!src.cockpitPrompt;
  const inputPlaceholder = !effectiveCapable
    ? "This seat can't host a cockpit"
    : !src.cockpitPrompt
      ? "Cockpit unavailable"
      : "Message the orchestrator…";

  const orchestratorSeat = agents.find((a) => a.roles.includes("orchestrator"))?.id;
  const capableSeats = agents
    .filter((a) => COCKPIT_CAPABLE_KINDS.has(a.kind ?? ""))
    .map((a) => a.id)
    .sort((a, b) => {
      if (a === orchestratorSeat) return -1;
      if (b === orchestratorSeat) return 1;
      return a.localeCompare(b);
    });

  const headerMetrics = [
    { label: "TOK", value: usage.tokens > 0 ? fmtTokens(usage.tokens) : "—" },
    { label: "COST", value: usage.cost > 0 ? fmtCost(usage.cost) : "—" },
    { label: "ITER", value: usage.iter > 0 ? `×${usage.iter}` : "—" },
  ];

  return (
    <div
      data-testid="cockpit-view"
      className="flex flex-1 overflow-hidden"
      role={viewMode ? undefined : "dialog"}
      aria-modal={viewMode ? undefined : "true"}
      aria-label="Cockpit"
    >
      {/* Main column */}
      <div className="flex min-w-0 flex-1 flex-col border-r border-[rgba(255,255,255,0.06)]">
        {/* Session header */}
        <div
          data-testid="cockpit-session-header"
          className="flex shrink-0 items-center gap-3 border-b border-[rgba(255,255,255,0.06)] bg-[var(--bg-panel)] px-5 py-2.5"
        >
          <AgentAvatar seatId={seat} />
          <div className="flex min-w-0 flex-col gap-0.5">
            <div className="flex items-center gap-2">
              <span className="font-mono text-[13px] font-[650] text-[var(--color-text-1)]">{seat}</span>
              <span
                className="rounded-full px-1.5 py-0.5 text-[8.5px] font-semibold uppercase tracking-[.4px]"
                style={{ color: role.color, background: "color-mix(in srgb, currentColor 14%, transparent)" }}
              >
                {role.label}
              </span>
            </div>
            <div className="flex items-center gap-1.5 font-mono text-[10px] text-[var(--color-text-3)]">
              <span className="inline-flex items-center gap-1 text-[var(--color-success)]">
                <span
                  className="h-[5px] w-[5px] rounded-full bg-[var(--color-success)] shell-breath"
                  style={{ boxShadow: "0 0 5px var(--color-success)" }}
                />
                live
              </span>
              {model && (
                <>
                  <span>·</span>
                  <span>{model}</span>
                </>
              )}
            </div>
          </div>
          <div className="ml-auto">
            <MetricStrip items={headerMetrics} />
          </div>
        </div>

        {(statusUnavailable || !effectiveCapable) && (
          <div
            data-testid="cockpit-notice"
            className={`shrink-0 border-b px-3 py-1.5 text-[11px] ${
              !effectiveCapable
                ? "border-[var(--color-border-subtle)] bg-[var(--color-bg-raised)] text-[var(--color-text-2)]"
                : "border-[var(--color-warn)]/20 bg-[var(--color-warn)]/10 text-[var(--color-warn)]"
            }`}
          >
            {!effectiveCapable ? capableReason || "This seat can't host a cockpit" : "Status unavailable — retrying…"}
          </div>
        )}

        {effectiveCapable && resumable && stream.length === 0 && (
          <div
            data-testid="cockpit-resume"
            className="shrink-0 border-b border-[var(--color-border-subtle)] bg-[var(--color-bg-raised)] px-3 py-2 text-[11px] text-[var(--color-text-2)]"
          >
            Previous session available —{" "}
            <button
              type="button"
              data-testid="cockpit-resume-button"
              onClick={resume}
              disabled={busy}
              className="font-[650] text-[var(--color-role-design)] hover:underline disabled:opacity-50"
            >
              Resume
            </button>
          </div>
        )}

        {/* Session stream */}
        <div
          ref={streamRef}
          data-testid="cockpit-messages"
          role="log"
          aria-live="polite"
          aria-label="Conversation"
          className="flex flex-1 flex-col gap-3 overflow-y-auto bg-[var(--color-bg-page)] px-5 py-4"
          style={{
            background:
              "radial-gradient(700px 340px at 78% -10%, color-mix(in srgb, var(--color-role-product) 3%, transparent), transparent 60%), var(--color-bg-page)",
          }}
        >
          {stream.map((item) => (
            <StreamCard
              key={item.id}
              item={item}
              seatId={seat}
              onTaskClick={(id) => onOpenBoard?.(id)}
              toggleExpanded={(id) =>
                setStream((prev) =>
                  prev.map((it) => (it.id === id && (it.type === "tool" || it.type === "diff") ? { ...it, expanded: !it.expanded } : it)),
                )
              }
            />
          ))}

          {pending.map((p) => (
            <ApprovalCard
              key={p.id}
              approval={p}
              confirmAllow={confirmAllow}
              setConfirmAllow={setConfirmAllow}
              onRespond={respond}
            />
          ))}

          {(busy || runningTool) && <StreamingIndicator seatId={seat} hasPending={pending.length > 0} />}

          {error && (
            <div data-testid="cockpit-error" className="text-[11px] text-[var(--color-danger)]">
              {error}
            </div>
          )}
        </div>

        {/* Sticky input row */}
        <div className="shrink-0 border-t border-[rgba(255,255,255,0.07)] bg-[var(--bg-panel)] px-5 py-3">
          <div className="flex items-end gap-2.5 rounded-xl border border-[rgba(255,255,255,0.12)] bg-[var(--bg-elev)] p-2">
            <button
              type="button"
              disabled
              title="Attach (coming soon)"
              className="pb-1 text-[16px] text-[var(--color-text-3)] disabled:cursor-not-allowed"
            >
              ＋
            </button>
            <input
              type="text"
              data-testid="cockpit-input"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.shiftKey) {
                  e.preventDefault();
                  send();
                }
              }}
              disabled={!inputEnabled}
              placeholder={inputPlaceholder}
              className="min-w-0 flex-1 bg-transparent px-1 py-1.5 text-[13px] text-[var(--color-text-1)] placeholder:text-[var(--color-text-3)] focus:outline-none"
            />
            {src.cockpitCancel && (
              <button
                type="button"
                data-testid="cockpit-cancel"
                onClick={cancel}
                className="rounded-lg border border-[var(--color-danger)]/40 bg-[var(--color-danger)]/10 px-3 py-1.5 text-[11px] font-semibold text-[var(--color-danger)] hover:bg-[var(--color-danger)]/15"
              >
                ■ Interrupt
              </button>
            )}
            <button
              type="button"
              data-testid="cockpit-send"
              onClick={send}
              disabled={!inputEnabled || !input.trim()}
              className="rounded-lg bg-[var(--proj)] px-4 py-1.5 text-[12px] font-semibold text-[#0a0e14] disabled:opacity-50"
            >
              Send ↵
            </button>
          </div>
          {busy && (
            <div className="mt-1.5 flex items-center gap-1.5 px-0.5 text-[10px] text-[var(--color-text-3)]">
              <span className="h-1 w-1 rounded-full bg-[var(--proj)]" />
              A turn is running — your next message will queue and send when it ends.
            </div>
          )}
        </div>
      </div>

      {/* Right column */}
      <div
        data-testid="cockpit-right-rail"
        className="flex w-[340px] flex-none flex-col gap-4 overflow-y-auto bg-[var(--color-bg-page)] p-4"
      >
        <ApprovalQueue
          pending={pending}
          confirmAllow={confirmAllow}
          setConfirmAllow={setConfirmAllow}
          onRespond={respond}
        />
        <SessionInfoCard
          seatId={seat}
          roles={seatRoles}
          model={model}
          usage={usage}
          threadId={threadId}
        />
        <RunContextCard state={state} onOpenBoard={onOpenBoard} />
        <SessionsList
          seat={seat}
          agents={agents}
          capableSeats={capableSeats}
          onSeatChange={onSeatChange}
        />
      </div>
    </div>
  );
}

function AgentAvatar({ seatId }: { seatId: string }) {
  return (
    <span
      className="grid h-7 w-7 flex-none place-items-center rounded-lg font-mono text-[11px] font-semibold"
      style={{
        background: avatarGradient(seatId),
        color: "#0a0e14",
      }}
    >
      {monogram(seatId)}
    </span>
  );
}

function StreamingIndicator({ seatId, hasPending }: { seatId: string; hasPending: boolean }) {
  return (
    <div data-testid="cockpit-streaming" className="mb-1 flex items-center gap-2.5">
      <AgentAvatar seatId={seatId} />
      <span className="inline-flex h-3 items-end gap-0.5">
        <span className="shell-eq-bar h-full w-[2.5px] rounded-sm bg-[var(--proj)]" style={{ animationDelay: "0s" }} />
        <span className="shell-eq-bar h-full w-[2.5px] rounded-sm bg-[var(--proj)]" style={{ animationDelay: "0.2s" }} />
        <span className="shell-eq-bar h-full w-[2.5px] rounded-sm bg-[var(--proj)]" style={{ animationDelay: "0.4s" }} />
      </span>
      <span className="text-[11px] text-[var(--color-text-3)]">
        {hasPending ? "waiting on your approval to continue…" : "thinking…"}
      </span>
    </div>
  );
}

function StreamCard({
  item,
  seatId,
  onTaskClick,
  toggleExpanded,
}: {
  item: StreamItem;
  seatId: string;
  onTaskClick: (id: string) => void;
  toggleExpanded: (id: number) => void;
}) {
  if (item.type === "turn") {
    return (
      <div data-testid="cockpit-turn-divider" className="my-1 flex items-center gap-2.5">
        <span className="h-px flex-1 bg-[rgba(255,255,255,0.07)]" />
        <span className="font-mono text-[9px] uppercase tracking-[.8px] text-[var(--color-text-3)]">
          turn started · {item.time}
        </span>
        <span className="h-px flex-1 bg-[rgba(255,255,255,0.07)]" />
      </div>
    );
  }

  if (item.type === "message") {
    if (item.role === "user") {
      return (
        <div className="mb-1 flex justify-end">
          <div
            data-testid="cockpit-message"
            data-role="user"
            className="max-w-[74%] border border-[rgba(138,180,255,0.18)] bg-[rgba(138,180,255,0.08)] px-3.5 py-2.5 text-[13px] leading-[1.55] text-[var(--color-text-1)]"
            style={{ borderRadius: "12px 12px 4px 12px" }}
          >
            {item.text}
          </div>
        </div>
      );
    }
    return (
      <div data-testid="cockpit-message" data-role="assistant" className="mb-1 flex gap-2.5">
        <AgentAvatar seatId={seatId} />
        <div className="min-w-0 text-[13px] leading-[1.6] text-[rgba(234,238,245,0.9)]">
          <RichText text={item.text} onTaskClick={onTaskClick} />
        </div>
      </div>
    );
  }

  if (item.type === "tool") {
    const output = item.text ?? "";
    const lines = output.split("\n").filter((l) => l.length > 0);
    const showExit = item.exit !== undefined;
    const exitOk = item.exit === 0;
    return (
      <div
        data-testid="cockpit-tool-card"
        className="mb-1 ml-8 overflow-hidden rounded-[10px] border border-[rgba(255,255,255,0.10)] bg-[var(--bg-panel)]"
      >
        <div className="flex items-center gap-2 border-b border-[rgba(255,255,255,0.06)] bg-[var(--bg-code)] px-3 py-2">
          <span className="font-mono text-[12px] font-medium text-[var(--color-success)]">$</span>
          <span className="min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[11.5px] text-[var(--color-text-1)]">
            {item.name}
          </span>
          {showExit && (
            <span
              className="rounded-full border px-2 py-0.5 text-[9px] font-medium"
              style={{
                color: exitOk ? "var(--color-success)" : "var(--color-danger)",
                background: exitOk ? "color-mix(in srgb, var(--color-success) 12%, transparent)" : "color-mix(in srgb, var(--color-danger) 12%, transparent)",
                borderColor: exitOk ? "color-mix(in srgb, var(--color-success) 28%, transparent)" : "color-mix(in srgb, var(--color-danger) 28%, transparent)",
              }}
            >
              exit {item.exit}
            </span>
          )}
          {lines.length > 0 && (
            <button
              type="button"
              onClick={() => toggleExpanded(item.id)}
              className="text-[9px] text-[var(--color-text-3)] hover:text-[var(--color-text-1)]"
            >
              {item.expanded ? "▾" : "▸"} {lines.length} lines
            </button>
          )}
        </div>
        {item.expanded && lines.length > 0 && (
          <pre className="m-0 overflow-auto bg-[var(--bg-code)] p-3 font-mono text-[11px] leading-[1.75] text-[rgba(234,238,245,0.6)]">
            <code>{output}</code>
          </pre>
        )}
      </div>
    );
  }

  if (item.type === "diff") {
    const additions = (item.patch.match(/^\+[^+]/gm) ?? []).length;
    const deletions = (item.patch.match(/^-[^-]/gm) ?? []).length;
    return (
      <div
        data-testid="cockpit-diff-card"
        className="mb-1 ml-8 overflow-hidden rounded-[10px] border border-[rgba(255,255,255,0.10)] bg-[var(--bg-panel)]"
      >
        <div className="flex items-center gap-2 border-b border-[rgba(255,255,255,0.06)] px-3 py-2">
          <span className="text-[11px] text-[var(--color-text-3)]">⑂</span>
          <span className="min-w-0 flex-1 font-mono text-[11.5px] font-medium text-[var(--color-text-1)]">
            {item.path}
          </span>
          <span className="font-mono text-[10px] text-[var(--color-success)]">+{additions}</span>
          <span className="font-mono text-[10px] text-[var(--color-danger)]">−{deletions}</span>
          <button
            type="button"
            onClick={() => toggleExpanded(item.id)}
            className="text-[9px] text-[var(--color-text-3)] hover:text-[var(--color-text-1)]"
          >
            {item.expanded ? "▾" : "▸"}
          </button>
        </div>
        {item.expanded && (
          <pre className="m-0 overflow-x-auto p-3 font-mono text-[11px] leading-[1.8]">
            {item.patch.split("\n").map((line, i) => {
              let bg = "transparent";
              let color = "var(--color-text-1)";
              if (line.startsWith("+") && !line.startsWith("++")) {
                bg = "rgba(110,231,160,0.08)";
                color = "#8fe8b4";
              } else if (line.startsWith("-") && !line.startsWith("--")) {
                bg = "rgba(249,112,102,0.09)";
                color = "#f4a09a";
              } else {
                color = "rgba(234,238,245,0.4)";
              }
              return (
                <span key={i} className="block px-3 py-0.5" style={{ background: bg, color }}>
                  {line || " "}
                </span>
              );
            })}
          </pre>
        )}
      </div>
    );
  }

  if (item.type === "plan") {
    return (
      <div
        data-testid="cockpit-plan-card"
        className="mb-1 ml-8 rounded-[10px] border border-[rgba(255,255,255,0.10)] bg-[var(--bg-elev)] p-3"
      >
        <div className="mb-2 flex items-center gap-2">
          <span className="text-[12px] text-[var(--proj)]">◇</span>
          <span className="text-[11px] font-semibold text-[var(--color-text-1)]">{item.title}</span>
          <span className="ml-auto font-mono text-[9px] text-[var(--color-text-3)]">
            {item.done} / {item.total}
          </span>
        </div>
        <div className="flex flex-col gap-1.5">
          {item.items.map((it, idx) => (
            <div key={idx} className="flex items-center gap-2 text-[11.5px]">
              {it.state === "done" ? (
                <span className="grid h-3.5 w-3.5 place-items-center rounded bg-[rgba(110,231,160,0.16)] text-[9px] text-[var(--color-success)]">
                  ✓
                </span>
              ) : it.state === "current" ? (
                <span className="h-3.5 w-3.5 rounded border-[1.5px] border-[var(--color-role-design)] shadow-[0_0_6px_rgba(138,180,255,0.4)]" />
              ) : (
                <span className="h-3.5 w-3.5 rounded border-[1.5px] border-[rgba(255,255,255,0.18)]" />
              )}
              <span
                className={
                  it.state === "done" ? "text-[rgba(234,238,245,0.55)] line-through" : "text-[var(--color-text-1)]"
                }
              >
                {it.text}
              </span>
            </div>
          ))}
        </div>
      </div>
    );
  }

  // "streaming" items are drawn by StreamingIndicator, never as a card.
  if (item.type === "streaming") return null;

  // system / fallback
  return (
    <div
      data-testid="cockpit-system-row"
      data-kind={item.kind}
      className="self-start rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-raised)] px-2.5 py-1.5 text-[11px] text-[var(--color-text-2)]"
    >
      <span className="mr-1.5 rounded bg-[var(--color-bg-inset)] px-1 py-0.5 text-[10px] text-[var(--color-text-3)]">
        {item.kind}
      </span>
      {item.text}
    </div>
  );
}

function RichText({ text, onTaskClick }: { text: string; onTaskClick: (id: string) => void }) {
  // Tokenize inline `code` spans and t-xxx task references.
  const tokens: { type: "text" | "code" | "task"; value: string }[] = [];
  const regex = /(`[^`]+`)|\b(t-[a-zA-Z0-9_-]+)\b/g;
  let last = 0;
  let m: RegExpExecArray | null;
  while ((m = regex.exec(text))) {
    if (m.index > last) tokens.push({ type: "text", value: text.slice(last, m.index) });
    if (m[1]) tokens.push({ type: "code", value: m[1].slice(1, -1) });
    else if (m[2]) tokens.push({ type: "task", value: m[2] });
    last = regex.lastIndex;
  }
  if (last < text.length) tokens.push({ type: "text", value: text.slice(last) });

  return (
    <>
      {tokens.map((t, i) => {
        if (t.type === "code") {
          return (
            <span
              key={i}
              className="mx-0.5 rounded bg-[rgba(255,255,255,0.05)] px-1 py-0.5 font-mono text-[11.5px]"
            >
              {t.value}
            </span>
          );
        }
        if (t.type === "task") {
          return (
            <button
              key={i}
              type="button"
              onClick={() => onTaskClick(t.value)}
              className="mx-0.5 rounded border border-[rgba(138,180,255,0.24)] bg-[rgba(138,180,255,0.10)] px-1.5 py-0.5 font-mono text-[11.5px] text-[var(--color-role-design)] hover:underline"
            >
              {t.value}
            </button>
          );
        }
        return <span key={i}>{t.value}</span>;
      })}
    </>
  );
}

function ApprovalCard({
  approval,
  confirmAllow,
  setConfirmAllow,
  onRespond,
}: {
  approval: PendingApproval;
  confirmAllow: string | null;
  setConfirmAllow: (id: string | null) => void;
  onRespond: (id: string, d: "allow" | "deny") => void;
}) {
  const p = approval;
  const danger = isDangerRisk(p.risk);
  const showRaw = p.rawInput !== undefined;
  const title = typeof p.rawInput === "object" && p.rawInput !== null ? (p.rawInput as Record<string, unknown>).title : undefined;

  return (
    <div
      key={p.id}
      data-testid="cockpit-approval"
      className="mb-1 ml-8 overflow-hidden rounded-xl border bg-[var(--bg-panel)] p-0"
      style={
        danger
          ? {
              borderColor: "rgba(249,112,102,0.45)",
              boxShadow: "0 0 0 1px rgba(249,112,102,0.15), 0 0 20px rgba(249,112,102,0.10)",
            }
          : { borderColor: "rgba(255,255,255,0.10)" }
      }
    >
      <div
        className="flex items-center gap-2 px-3 py-2"
        style={
          danger
            ? { background: "rgba(249,112,102,0.08)", borderBottom: "1px solid rgba(249,112,102,0.24)" }
            : { background: "transparent", borderBottom: "1px solid rgba(255,255,255,0.06)" }
        }
      >
        <span className="text-[13px]" style={{ color: danger ? "var(--color-danger)" : "var(--color-role-design)" }}>
          {danger ? "⚠" : "⑂"}
        </span>
        <span className="text-[12px] font-[650] text-[var(--color-text-1)]">
          {danger ? "Approval required · command execution" : p.toolName}
        </span>
        {p.risk && (
          <span
            data-testid="cockpit-approval-risk"
            className="ml-auto rounded-full px-1.5 py-0.5 text-[8.5px] font-semibold uppercase tracking-[.4px]"
            style={{
              color: danger ? "var(--color-danger)" : "var(--color-text-3)",
              background: danger ? "rgba(249,112,102,0.12)" : "rgba(255,255,255,0.06)",
              border: "1px solid",
              borderColor: danger ? "rgba(249,112,102,0.34)" : "rgba(255,255,255,0.10)",
            }}
          >
            {p.risk}
          </span>
        )}
      </div>
      <div className="p-3">
        {title !== undefined && (
          <div className="mb-1 text-[9px] text-[var(--color-text-3)]">
            agent title <span className="text-[var(--color-text-3)]/70">(advisory)</span> · {String(title)}
          </div>
        )}
        {showRaw && (
          <>
            <div className="mb-1 font-mono text-[9px] uppercase tracking-[.4px] text-[var(--color-text-3)]">
              raw command
            </div>
            <pre
              data-testid="cockpit-approval-rawinput"
              className="mb-3 max-h-32 overflow-auto rounded-lg border border-[rgba(249,112,102,0.22)] bg-[var(--bg-code)] p-2.5 font-mono text-[11.5px] leading-[1.7] text-[var(--color-text-1)]"
            >
              <span className="text-[var(--color-success)]">$</span> {formatRawInput(p.rawInput)}
            </pre>
          </>
        )}
        <div className="flex flex-wrap items-center gap-2">
          {danger && confirmAllow === p.id ? (
            <button
              type="button"
              data-testid="cockpit-approval-allow-confirm"
              onClick={() => {
                setConfirmAllow(null);
                onRespond(p.id, "allow");
              }}
              className="rounded-lg bg-[var(--color-danger)] px-4 py-2 text-[11px] font-semibold text-[var(--color-on-accent)]"
            >
              Confirm allow ▸
            </button>
          ) : (
            <button
              type="button"
              data-testid="cockpit-approval-allow"
              onClick={() => {
                if (danger) setConfirmAllow(p.id);
                else onRespond(p.id, "allow");
              }}
              className="rounded-lg bg-[var(--color-success)] px-4 py-2 text-[11px] font-semibold text-[var(--color-on-accent)]"
            >
              Allow
            </button>
          )}
          <button
            type="button"
            data-testid="cockpit-approval-deny"
            onClick={() => {
              setConfirmAllow(null);
              onRespond(p.id, "deny");
            }
            }
            className="rounded-lg border px-4 py-2 text-[11px] font-semibold"
            style={{
              color: danger ? "var(--color-danger)" : "rgba(234,238,245,0.7)",
              borderColor: danger ? "rgba(249,112,102,0.36)" : "rgba(255,255,255,0.14)",
              background: danger ? "rgba(249,112,102,0.10)" : "transparent",
            }}
          >
            Deny
          </button>
          {danger && (
            <span className="ml-auto text-[9.5px] text-[rgba(249,112,102,0.75)]">
              ⤷ strong approval — confirm once more
            </span>
          )}
        </div>
      </div>
    </div>
  );
}

function ApprovalQueue({
  pending,
  confirmAllow,
  setConfirmAllow,
  onRespond,
}: {
  pending: PendingApproval[];
  confirmAllow: string | null;
  setConfirmAllow: (id: string | null) => void;
  onRespond: (id: string, d: "allow" | "deny") => void;
}) {
  return (
    <div data-testid="cockpit-approval-queue">
      <div className="mb-2 flex items-center gap-2 px-0.5">
        <span className="font-mono text-[9.5px] uppercase tracking-[.8px] text-[var(--color-text-3)]">
          Approval queue
        </span>
        {pending.length > 0 && (
          <span
            className="rounded-full border border-[rgba(249,112,102,0.3)] bg-[rgba(249,112,102,0.12)] px-1.5 py-0.5 font-mono text-[9px] text-[var(--color-danger)]"
          >
            {pending.length} pending
          </span>
        )}
      </div>
      {pending.length === 0 && (
        <div className="rounded-xl border border-[rgba(255,255,255,0.08)] bg-[var(--bg-panel)] p-3 text-[11px] text-[var(--color-text-3)]">
          No pending approvals.
        </div>
      )}
      {pending.map((p) => (
        <QueueApprovalCard
          key={p.id}
          approval={p}
          confirmAllow={confirmAllow}
          setConfirmAllow={setConfirmAllow}
          onRespond={onRespond}
        />
      ))}
    </div>
  );
}

function QueueApprovalCard({
  approval,
  confirmAllow,
  setConfirmAllow,
  onRespond,
}: {
  approval: PendingApproval;
  confirmAllow: string | null;
  setConfirmAllow: (id: string | null) => void;
  onRespond: (id: string, d: "allow" | "deny") => void;
}) {
  const p = approval;
  const danger = isDangerRisk(p.risk);
  const rawText = typeof p.rawInput === "string" ? p.rawInput : JSON.stringify(p.rawInput);

  return (
    <div
      data-testid="cockpit-queue-approval"
      className="mb-2 rounded-[11px] border bg-[var(--bg-panel)] p-3"
      style={
        danger
          ? { borderColor: "rgba(249,112,102,0.4)", boxShadow: "0 0 0 1px rgba(249,112,102,0.12)" }
          : { borderColor: "rgba(255,255,255,0.10)" }
      }
    >
      <div className="mb-2 flex items-center gap-2">
        <span className="text-[11px]" style={{ color: danger ? "var(--color-danger)" : "var(--color-role-design)" }}>
          {danger ? "⚠" : "⑂"}
        </span>
        <span className="text-[10.5px] font-semibold text-[var(--color-text-1)]">{p.toolName}</span>
        {p.risk && (
          <span
            className="ml-auto rounded-full px-1.5 py-0.5 text-[8px] font-semibold uppercase tracking-[.3px]"
            style={{
              color: danger ? "var(--color-danger)" : "var(--color-text-3)",
              background: danger ? "rgba(249,112,102,0.12)" : "rgba(255,255,255,0.06)",
            }}
          >
            {p.risk}
          </span>
        )}
      </div>
      {p.rawInput !== undefined && (
        <pre className="mb-2 overflow-auto rounded-md border border-[rgba(255,255,255,0.06)] bg-[var(--bg-code)] p-2 font-mono text-[10.5px] leading-[1.5] text-[var(--color-text-1)]">
          {typeof rawText === "string" && rawText.length > 280 ? rawText.slice(0, 280) + "…" : rawText}
        </pre>
      )}
      <div className="flex gap-1.5">
        {danger && confirmAllow === p.id ? (
          <button
            type="button"
            onClick={() => {
              setConfirmAllow(null);
              onRespond(p.id, "allow");
            }}
            className="flex-1 rounded-md bg-[var(--color-success)] py-1.5 text-center text-[10px] font-semibold text-[var(--color-on-accent)]"
          >
            Confirm
          </button>
        ) : (
          <button
            type="button"
            onClick={() => {
              if (danger) setConfirmAllow(p.id);
              else onRespond(p.id, "allow");
            }}
            className="flex-1 rounded-md bg-[var(--color-success)] py-1.5 text-center text-[10px] font-semibold text-[var(--color-on-accent)]"
          >
            Allow
          </button>
        )}
        <button
          type="button"
          onClick={() => {
            setConfirmAllow(null);
            onRespond(p.id, "deny");
          }}
          className="flex-1 rounded-md border py-1.5 text-center text-[10px] font-semibold"
          style={{
            color: danger ? "var(--color-danger)" : "rgba(234,238,245,0.7)",
            borderColor: danger ? "rgba(249,112,102,0.36)" : "rgba(255,255,255,0.14)",
          }}
        >
          Deny
        </button>
      </div>
    </div>
  );
}

function SessionInfoCard({
  seatId,
  roles,
  model,
  usage,
  threadId,
}: {
  seatId: string;
  roles: string[];
  model: string;
  usage: { tokens: number; cost: number; iter: number };
  threadId: string;
}) {
  const role = roleChip(roles);
  const [copied, setCopied] = useState(false);

  const copyThread = () => {
    if (!threadId) return;
    navigator.clipboard.writeText(threadId).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    }).catch(() => {});
  };

  return (
    <div data-testid="cockpit-session-info" className="rounded-xl border border-[rgba(255,255,255,0.08)] bg-[var(--bg-elev)] p-3.5">
      <div className="mb-3 flex items-center gap-2.5">
        <AgentAvatar seatId={seatId} />
        <div className="min-w-0">
          <div className="font-mono text-[12px] font-[650] text-[var(--color-text-1)]">{seatId}</div>
          <div className="text-[9px] font-medium uppercase tracking-[.3px]" style={{ color: role.color }}>
            {role.label}
          </div>
        </div>
        <span className="ml-auto inline-flex items-center gap-1 rounded-full px-2 py-0.5 font-mono text-[9.5px] text-[var(--color-success)]"
          style={{ background: "color-mix(in srgb, var(--color-success) 12%, transparent)" }}>
          <span className="h-1.5 w-1.5 rounded-full bg-[var(--color-success)]" style={{ boxShadow: "0 0 5px var(--color-success)" }} />
          live
        </span>
      </div>
      <div className="flex flex-col gap-2">
        <div className="flex items-center gap-2 font-mono text-[10.5px]">
          <span className="flex-1 text-[var(--color-text-3)]">model</span>
          <span className="text-[rgba(234,238,245,0.82)]">{model || "—"}</span>
        </div>
        <div className="flex items-center gap-2 font-mono text-[10.5px]">
          <span className="flex-1 text-[var(--color-text-3)]">usage</span>
          <span className="text-[rgba(234,238,245,0.82)]">
            {usage.tokens > 0 ? fmtTokens(usage.tokens) : "—"} tok · {usage.cost > 0 ? fmtCost(usage.cost) : "—"}
          </span>
        </div>
        <div className="flex items-center gap-2 font-mono text-[10.5px]">
          <span className="flex-1 text-[var(--color-text-3)]">thread</span>
          <span className="text-[rgba(234,238,245,0.7)]">{threadId ? threadId.slice(0, 12) : "—"}</span>
          {threadId && (
            <button
              type="button"
              onClick={copyThread}
              className="text-[var(--color-text-3)] hover:text-[var(--color-text-1)]"
              aria-label="copy thread id"
            >
              {copied ? "✓" : "⧉"}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

function RunContextCard({
  state,
  onOpenBoard,
}: {
  state?: State;
  onOpenBoard?: (taskId?: string) => void;
}) {
  if (!state) return null;
  const feature = currentFeature(state);
  if (!feature) return null;

  const currentTask = feature.tasks.find((t) => t.status === "in_progress" || t.status === "awaiting_review");

  return (
    <button
      type="button"
      data-testid="cockpit-run-context"
      onClick={() => onOpenBoard?.(currentTask?.id)}
      className="w-full rounded-xl border border-[rgba(255,255,255,0.08)] bg-[var(--bg-elev)] p-3.5 text-left hover:border-[rgba(255,255,255,0.14)]"
    >
      <div className="mb-3 flex items-center gap-2">
        <span className="font-mono text-[11.5px] font-semibold text-[var(--color-text-1)]">{feature.id}</span>
        <span className="font-mono text-[9.5px] text-[var(--color-text-3)]">{feature.branch}</span>
        <span className="ml-auto text-[9px] text-[var(--color-text-3)]">open Board →</span>
      </div>
      <MiniPipeline tasks={feature.tasks} merge />
    </button>
  );
}

function SessionsList({
  seat,
  agents,
  capableSeats,
  onSeatChange,
}: {
  seat: string;
  agents: Seat[];
  capableSeats: string[];
  onSeatChange?: (seat: string) => void;
}) {
  return (
    <div data-testid="cockpit-sessions">
      <div className="mb-2 px-0.5 font-mono text-[9.5px] uppercase tracking-[.8px] text-[var(--color-text-3)]">
        Sessions
      </div>
      {capableSeats.map((id) => {
        const a = agents.find((x) => x.id === id);
        const active = id === seat;
        const r = roleChip(a?.roles ?? []);
        return (
          <button
            key={id}
            type="button"
            onClick={() => onSeatChange?.(id)}
            className={`mb-1.5 flex w-full items-center gap-2 rounded-[10px] border p-2.5 text-left ${
              active
                ? "border-[rgba(255,212,121,0.28)] bg-[rgba(255,212,121,0.06)]"
                : "border-[rgba(255,255,255,0.08)] bg-[var(--bg-panel)] hover:bg-[var(--bg-elev)]"
            }`}
          >
            <AgentAvatar seatId={id} />
            <div className="min-w-0 flex-1">
              <div className={`text-[11px] font-semibold ${active ? "text-[var(--color-text-1)]" : "text-[var(--color-text-2)]"}`}>
                {id} · {r.label}
              </div>
              <div className="text-[9px] text-[var(--color-text-3)]">{active ? "this session" : "click to switch"}</div>
            </div>
            {active && <span className="h-1.5 w-1.5 rounded-full bg-[var(--color-success)]" style={{ boxShadow: "0 0 5px var(--color-success)" }} />}
          </button>
        );
      })}
      <div className="flex items-center gap-2 rounded-[10px] border border-dashed border-[rgba(255,255,255,0.12)] p-2.5 opacity-70">
        <span className="grid h-6 w-6 place-items-center rounded-lg bg-[rgba(255,255,255,0.05)] text-[12px] text-[var(--color-text-3)]">
          ＋
        </span>
        <div className="min-w-0">
          <div className="text-[10.5px] font-medium text-[rgba(234,238,245,0.6)]">Attach a worker session</div>
          <div className="text-[9px] text-[rgba(234,238,245,0.36)]">multi-session · E3</div>
        </div>
      </div>
    </div>
  );
}
