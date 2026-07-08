import { useEffect, useRef, useState } from "react";
import type { CockpitEvent } from "../lib/api";
import { useDataSource } from "../lib/datasource";

type Message = { role: "user" | "assistant"; text: string };
type SystemRow = { id: number; kind: string; text: string };

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

export function CockpitPanel({
  project,
  seat,
  onClose,
}: {
  project: string;
  seat: string;
  onClose: () => void;
}) {
  const src = useDataSource();
  const [messages, setMessages] = useState<Message[]>([]);
  const [systemRows, setSystemRows] = useState<SystemRow[]>([]);
  const [pending, setPending] = useState<
    { id: string; kind: string; toolName: string; rawInput?: unknown }[]
  >([]);
  const [input, setInput] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const panelRef = useRef<HTMLElement>(null);
  const rowId = useRef(0);
  const reduced = prefersReducedMotion();

  const loadStatus = async () => {
    if (!src.cockpitStatus) return;
    try {
      const st = await src.cockpitStatus(project, seat);
      setPending(st.pending);
    } catch {
      // status polling is best-effort
    }
  };

  useEffect(() => {
    loadStatus();
    const t = setInterval(loadStatus, 2000);
    return () => clearInterval(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [project, seat, src.cockpitStatus]);

  useEffect(() => {
    if (!src.cockpitStreamUrl) return;
    const es = new EventSource(src.cockpitStreamUrl(project, seat));
    es.onmessage = (e) => {
      try {
        const ev = JSON.parse(e.data) as CockpitEvent;
        if (ev.kind === "message" && typeof ev.text === "string") {
          // Capture the narrowed value: TS loses the `typeof ev.text === string`
          // narrowing inside the deferred setMessages closure (property narrowing
          // doesn't survive across a nested function), so read it into a local.
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
    };
    return () => es.close();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [project, seat, src.cockpitStreamUrl]);

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

  const send = async () => {
    const text = input.trim();
    if (!text || !src.cockpitPrompt) return;
    setMessages((prev) => [...prev, { role: "user", text }]);
    setInput("");
    setBusy(true);
    setError("");
    try {
      await src.cockpitPrompt(project, seat, text);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const cancel = async () => {
    if (!src.cockpitCancel) return;
    try {
      await src.cockpitCancel(project, seat);
    } catch {
      // best-effort
    }
  };

  const respond = async (approvalId: string, decision: "allow" | "deny") => {
    if (!src.cockpitRespond) return;
    try {
      await src.cockpitRespond(project, seat, approvalId, decision);
      await loadStatus();
    } catch {
      // best-effort
    }
  };

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
          <div>
            <div className="mono text-[11px] text-[var(--color-text-3)]">{seat}</div>
            <div className="text-[15px] font-[650] text-[var(--color-text-1)]">Cockpit</div>
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

        {/* Messages */}
        <div
          data-testid="cockpit-messages"
          className="flex flex-1 flex-col gap-3 overflow-y-auto px-4 py-3"
        >
          {messages.map((m, i) => (
            <div
              key={i}
              data-testid="cockpit-message"
              data-role={m.role}
              className={`max-w-[85%] rounded-xl border border-[var(--color-border-subtle)] px-3 py-2 text-[12px] leading-[1.6] ${
                m.role === "user"
                  ? "self-end bg-[var(--color-role-dev)] text-[var(--color-role-dev-ink)]"
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

          {pending.map((p) => (
            <div
              key={p.id}
              data-testid="cockpit-approval"
              className="rounded-xl border border-[var(--color-border-subtle)] bg-[var(--color-bg-raised)] p-3"
            >
              <div
                data-testid="cockpit-approval-tool"
                className="mb-2 text-[12px] font-[650] text-[var(--color-text-1)]"
              >
                {p.toolName} <span className="text-[var(--color-text-3)]">· {p.kind}</span>
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
                <button
                  type="button"
                  data-testid="cockpit-approval-allow"
                  onClick={() => respond(p.id, "allow")}
                  className="flex-1 rounded-lg bg-[var(--color-success)] px-3 py-1.5 text-[11px] font-[650] text-[var(--color-success-ink)]"
                >
                  Allow
                </button>
                <button
                  type="button"
                  data-testid="cockpit-approval-deny"
                  onClick={() => respond(p.id, "deny")}
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
              disabled={busy || !src.cockpitPrompt}
              placeholder={src.cockpitPrompt ? "Message orchestrator…" : "Cockpit unavailable"}
              className="min-w-0 flex-1 rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-3 py-2 text-[12px] text-[var(--color-text-1)] placeholder:text-[var(--color-text-3)] focus:outline-none focus:ring-1 focus:ring-[var(--color-role-design)]"
            />
            <button
              type="button"
              data-testid="cockpit-send"
              onClick={send}
              disabled={busy || !input.trim() || !src.cockpitPrompt}
              className="rounded-lg bg-[var(--color-role-design)] px-3 py-2 text-[12px] font-[650] text-[var(--color-role-design-ink)] disabled:opacity-50"
            >
              Send
            </button>
          </div>
        </div>
      </aside>
    </>
  );
}
