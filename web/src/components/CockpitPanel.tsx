import { useEffect, useRef, useState } from "react";
import type { CockpitEvent } from "../lib/api";
import type { Seat } from "../lib/types";
import { useDataSource } from "../lib/datasource";
import { COCKPIT_STATUS_POLL_MS } from "../lib/constants";
import { Select } from "./ui/Select";

type Message = { role: "user" | "assistant"; text: string };
type SystemRow = { id: number; kind: string; text: string };

const COCKPIT_CAPABLE_KINDS = new Set([
  "claude-code",
  "codex-cli",
  "kimi-cli",
  "gemini-cli",
  "opencode",
]);

function prefersReducedMotion(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches
  );
}

function formatRawInput(rawInput: unknown): string {
  const text = JSON.stringify(rawInput, null, 1);
  if (text.length > 600) return text.slice(0, 600) + "…";
  return text;
}

function formatSystemRow(ev: CockpitEvent): string {
  if (ev.kind === "tool" && ev.tool) {
    const tail = ev.tool.text ? `: ${ev.tool.text}` : "";
    return `${ev.tool.name} (${ev.tool.phase})${tail}`;
  }
  if (ev.kind === "error") return ev.err ?? ev.text ?? "error";
  if (ev.kind === "state" && ev.state) return JSON.stringify(ev.state);
  if (ev.kind === "usage" && ev.usage) return JSON.stringify(ev.usage);
  return ev.text ?? "";
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

export function CockpitPanel({
  project,
  seat,
  agents,
  onClose,
  onSeatChange,
  onNotify,
}: {
  project: string;
  seat: string;
  agents: Seat[];
  onClose: () => void;
  onSeatChange?: (seat: string) => void;
  onNotify?: (msg: string, kind?: "error") => void;
}) {
  const src = useDataSource();
  const [messages, setMessages] = useState<Message[]>([]);
  const [systemRows, setSystemRows] = useState<SystemRow[]>([]);
  const [pending, setPending] = useState<
    { id: string; kind: string; toolName: string; rawInput?: unknown; risk?: string }[]
  >([]);
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
  const panelRef = useRef<HTMLElement>(null);
  const messagesRef = useRef<HTMLDivElement>(null);
  const rowId = useRef(0);
  const seatInitRef = useRef(true);
  const reduced = prefersReducedMotion();
  const statusUnavailable = statusFailures >= 3;

  const seatKind = agents.find((a) => a.id === seat)?.kind;
  const seatCapableByRoster = seatKind
    ? COCKPIT_CAPABLE_KINDS.has(seatKind)
    : true;
  const effectiveCapable = capable && seatCapableByRoster;

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

  // When the selected seat changes (prop or dropdown-driven) clear stale stream
  // content so the previous seat's events don't leak into the new session. Skip
  // the first render — the stream effect already subscribes with the initial seat.
  useEffect(() => {
    if (seatInitRef.current) {
      seatInitRef.current = false;
      return;
    }
    setMessages([]);
    setSystemRows([]);
    setPending([]);
    setInput("");
    setError("");
    setRunningTool(null);
    setThreadId("");
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
          } else if (ev.tool.phase === "end") {
            setRunningTool(null);
          }
        } else if (ev.kind === "state" && ev.state === "turn_completed") {
          setRunningTool(null);
        } else if (ev.kind === "error") {
          setRunningTool(null);
        }
        if (ev.kind === "message" && typeof ev.text === "string") {
          const delta = ev.text;
          setMessages((prev) => {
            if (prev.length === 0 || prev[prev.length - 1].role !== "assistant") {
              return [...prev, { role: "assistant", text: delta }];
            }
            const next = [...prev];
            next[next.length - 1] = {
              ...next[next.length - 1],
              text: next[next.length - 1].text + delta,
            };
            return next;
          });
        } else {
          rowId.current += 1;
          const text = formatSystemRow(ev);
          setSystemRows((prev) => [...prev, { id: rowId.current, kind: ev.kind, text }]);
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
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose();
      }
    };
    window.addEventListener("keydown", onKey, true);
    panelRef.current?.focus();
    return () => window.removeEventListener("keydown", onKey, true);
  }, [onClose]);

  // Auto-scroll to the bottom of the message list, unless the user has
  // intentionally scrolled up (>80px from the bottom).
  useEffect(() => {
    const el = messagesRef.current;
    if (!el) return;
    const distance = el.scrollHeight - el.scrollTop - el.clientHeight;
    if (distance <= 80) {
      el.scrollTop = el.scrollHeight;
    }
  }, [messages, systemRows, pending, runningTool]);

  const send = async () => {
    const text = input.trim();
    if (!text || !src.cockpitPrompt) return;
    setMessages((prev) => [...prev, { role: "user", text }]);
    setInput("");
    setBusy(true);
    setError("");
    try {
      const resp = await src.cockpitPrompt(project, seat, text);
      if (resp.threadId) {
        setThreadId(resp.threadId);
      }
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
      if (resp.threadId) {
        setThreadId(resp.threadId);
      }
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

  function riskBadgeStyle(risk?: string) {
    switch (risk) {
      case "exec":
        return { color: "var(--color-danger)", background: "color-mix(in srgb, var(--color-danger) 14%, transparent)" };
      case "write":
        return { color: "var(--color-warn)", background: "color-mix(in srgb, var(--color-warn) 14%, transparent)" };
      case "mcp":
        return { color: "var(--color-role-design)", background: "color-mix(in srgb, var(--color-role-design) 14%, transparent)" };
      default:
        return { color: "var(--color-text-3)", background: "color-mix(in srgb, var(--color-text-3) 14%, transparent)" };
    }
  }

  const inputEnabled = !busy && effectiveCapable && !!src.cockpitPrompt;
  const inputPlaceholder = !effectiveCapable
    ? "This seat can't host a cockpit"
    : !src.cockpitPrompt
      ? "Cockpit unavailable"
      : "Message orchestrator…";

  // Seat dropdown: capable roster seats, orchestrator first, current seat always valid.
  const orchestratorSeat = agents.find((a) => a.roles.includes("orchestrator"))?.id;
  let seatOptions = agents
    .filter((a) => COCKPIT_CAPABLE_KINDS.has(a.kind ?? ""))
    .map((a) => a.id);
  if (seat && !seatOptions.includes(seat)) {
    seatOptions = [seat, ...seatOptions];
  }
  seatOptions.sort((a, b) => {
    if (a === orchestratorSeat) return -1;
    if (b === orchestratorSeat) return 1;
    return a.localeCompare(b);
  });

  return (
    <>
      <div
        data-testid="cockpit-scrim"
        onClick={onClose}
        className="absolute inset-0 z-40 bg-black/10"
        style={
          reduced
            ? undefined
            : { animation: `panel-scrim-in var(--motion-layout) var(--motion-ease)` }
        }
      />
      <aside
        ref={panelRef}
        tabIndex={-1}
        data-testid="cockpit-panel"
        role="dialog"
        aria-modal="true"
        aria-label="Cockpit"
        className="absolute right-3 top-3 bottom-3 z-50 flex w-[360px] flex-col overflow-hidden rounded-2xl border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] shadow-[var(--shadow-raised)]"
        style={
          reduced
            ? undefined
            : { animation: `panel-slide-in var(--motion-layout) var(--motion-ease)` }
        }
      >
        {/* Header */}
        <div className="flex shrink-0 items-center justify-between rounded-t-2xl border-b border-[var(--color-border-subtle)] bg-[linear-gradient(170deg,color-mix(in_srgb,var(--color-role-design)_8%,transparent),transparent_70%)] px-4 py-3">
          <div className="min-w-0 flex-1">
            <div className="mono text-[11px] text-[var(--color-text-3)]">
              {seat}
              {threadId && (
                <span
                  data-testid="cockpit-thread-id"
                  className="ml-1.5 text-[10px] text-[var(--color-text-3)]"
                  title={threadId}
                >
                  {threadId.slice(0, 8)}
                </span>
              )}
            </div>
            <div className="text-[15px] font-[650] text-[var(--color-text-1)]">Cockpit</div>
            {seatOptions.length > 0 && (
              <div className="mt-1.5">
                <Select
                  data-testid="cockpit-seat-select"
                  value={seat}
                  onChange={(e) => onSeatChange?.(e.target.value)}
                  className="h-7 py-1 text-[11px]"
                >
                  {seatOptions.map((s) => (
                    <option key={s} value={s}>
                      {s === orchestratorSeat ? `${s} (orchestrator)` : s}
                    </option>
                  ))}
                </Select>
              </div>
            )}
          </div>
          <div className="flex items-center gap-1">
            {src.cockpitCancel && (
              <button
                type="button"
                data-testid="cockpit-cancel"
                onClick={cancel}
                className="rounded-md px-2 py-1 text-[11px] text-[var(--color-text-3)] hover:text-[var(--color-danger)]"
                aria-label="Cancel"
              >
                Cancel
              </button>
            )}
            <button
              type="button"
              data-testid="cockpit-close"
              onClick={onClose}
              className="rounded-md px-2 py-1 text-[11px] text-[var(--color-text-3)] hover:text-[var(--color-text-1)]"
              aria-label="Close"
            >
              ✕
            </button>
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

        {effectiveCapable && resumable && messages.length === 0 && systemRows.length === 0 && (
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

        {/* Messages */}
        <div
          ref={messagesRef}
          data-testid="cockpit-messages"
          role="log"
          aria-live="polite"
          aria-label="Conversation"
          className="flex flex-1 flex-col gap-3 overflow-y-auto px-4 py-3"
        >
          {messages.map((m, i) => (
            <div
              key={i}
              data-testid="cockpit-message"
              data-role={m.role}
              className={`max-w-[85%] rounded-xl border border-[var(--color-border-subtle)] px-3 py-2 text-[12px] leading-[1.6] ${
                m.role === "user"
                  ? "self-end bg-[var(--color-role-dev)] text-[var(--color-on-accent)]"
                  : "self-start bg-[var(--color-bg-inset)] text-[var(--color-text-1)]"
              }`}
            >
              {m.text}
            </div>
          ))}

          {systemRows.map((r) => (
            <div
              key={r.id}
              data-testid="cockpit-system-row"
              data-kind={r.kind}
              className="self-start rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-raised)] px-2.5 py-1.5 text-[11px] text-[var(--color-text-2)]"
            >
              <span className="mr-1.5 rounded bg-[var(--color-bg-inset)] px-1 py-0.5 text-[10px] text-[var(--color-text-3)]">
                {r.kind}
              </span>
              {r.text}
            </div>
          ))}

          {error && (
            <div data-testid="cockpit-error" className="text-[11px] text-[var(--color-danger)]">
              {error}
            </div>
          )}

          {runningTool && (
            <div
              data-testid="cockpit-running"
              className="self-start rounded-lg px-2.5 py-1.5 text-[11px] text-[var(--color-text-2)] animate-pulse"
            >
              ⏺ {runningTool} running…
            </div>
          )}

          {pending.map((p) => (
            <div
              key={p.id}
              data-testid="cockpit-approval"
              className="rounded-xl border border-[var(--color-border-subtle)] bg-[var(--color-bg-raised)] p-3"
            >
              <div
                data-testid="cockpit-approval-tool"
                className="mb-2 flex items-center gap-2 text-[12px] font-[650] text-[var(--color-text-1)]"
              >
                <span>{p.toolName}</span>{" "}
                <span className="text-[var(--color-text-3)]">· {p.kind}</span>
                {p.risk && (
                  <span
                    data-testid="cockpit-approval-risk"
                    className="rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wider"
                    style={riskBadgeStyle(p.risk)}
                  >
                    {p.risk}
                  </span>
                )}
              </div>
              {p.rawInput !== undefined && (
                <pre
                  data-testid="cockpit-approval-rawinput"
                  className="mb-3 max-h-32 overflow-auto rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] p-2 font-mono text-[11px] text-[var(--color-text-2)]"
                >
                  <code>{formatRawInput(p.rawInput)}</code>
                </pre>
              )}
              <div className="flex gap-2">
                {p.risk === "exec" && confirmAllow === p.id ? (
                  <button
                    type="button"
                    data-testid="cockpit-approval-allow-confirm"
                    onClick={() => {
                      setConfirmAllow(null);
                      respond(p.id, "allow");
                    }}
                    className="flex-1 rounded-lg bg-[var(--color-danger)] px-3 py-1.5 text-[11px] font-[650] text-[var(--color-on-accent)]"
                  >
                    Confirm allow ▸
                  </button>
                ) : (
                  <button
                    type="button"
                    data-testid="cockpit-approval-allow"
                    onClick={() => {
                      if (p.risk === "exec") {
                        setConfirmAllow(p.id);
                      } else {
                        respond(p.id, "allow");
                      }
                    }}
                    className="flex-1 rounded-lg bg-[var(--color-success)] px-3 py-1.5 text-[11px] font-[650] text-[var(--color-on-accent)]"
                  >
                    Allow
                  </button>
                )}
                <button
                  type="button"
                  data-testid="cockpit-approval-deny"
                  onClick={() => {
                    setConfirmAllow(null);
                    respond(p.id, "deny");
                  }}
                  className="flex-1 rounded-lg bg-[var(--color-danger)] px-3 py-1.5 text-[11px] font-[650] text-[var(--color-danger-ink)]"
                >
                  Deny
                </button>
              </div>
            </div>
          ))}
        </div>

        {/* Input */}
        <div className="shrink-0 border-t border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] p-3">
          <div className="flex gap-2">
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
              className="min-w-0 flex-1 rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-3 py-2 text-[12px] text-[var(--color-text-1)] placeholder:text-[var(--color-text-3)] focus:outline-none focus:ring-1 focus:ring-[var(--color-role-design)]"
            />
            <button
              type="button"
              data-testid="cockpit-send"
              onClick={send}
              disabled={!inputEnabled || !input.trim()}
              className="rounded-lg bg-[var(--color-role-design)] px-3 py-2 text-[12px] font-[650] text-[var(--color-on-accent)] disabled:opacity-50"
            >
              Send
            </button>
          </div>
        </div>
      </aside>
    </>
  );
}
