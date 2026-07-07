import { useEffect, useRef, useState } from "react";
import { subscribeAgentStream } from "../../lib/api";

// AgentTerminal tails a task's raw agent output (P1: raw lines; structured
// rendering arrives in P2). Newest at the bottom; bounded to the last 500 lines.
export function AgentTerminal({ project, task, seat, height = 380 }: { project: string; task: string; seat?: string; height?: number }) {
  const [lines, setLines] = useState<string[]>([]);
  const endRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    setLines([]);
    // Server tags each line with its 1-based stream ordinal (SSE id). Tracking
    // the highest one seen drops any re-delivered backfill after an EventSource
    // auto-reconnect — belt and braces with the server's Last-Event-ID resume.
    let last = 0;
    const off = subscribeAgentStream(project, task, (line, id) => {
      const n = id ? Number(id) : NaN;
      if (Number.isFinite(n)) {
        if (n <= last) return;
        last = n;
      }
      setLines((prev) => (prev.length > 500 ? [...prev.slice(-499), line] : [...prev, line]));
    });
    return off;
  }, [project, task]);

  useEffect(() => { endRef.current?.scrollIntoView?.({ block: "end" }); }, [lines]);

  return (
    <div
      data-testid="agent-terminal"
      className="mono overflow-y-auto rounded-[10px] border border-[var(--color-border-subtle)] px-[14px] py-3 text-[10.5px] leading-[1.9] text-[var(--color-text-2)]"
      style={{ background: "var(--color-bg-terminal,#07090d)", minHeight: height, maxHeight: height }}
    >
      <div className="mb-2 flex gap-2 border-b border-[color-mix(in_srgb,var(--color-text-1)_6%,transparent)] pb-1.5 text-[8.5px] tracking-[0.8px] text-[var(--color-text-3)]">
        LIVE{seat ? ` · ${seat}` : ""} · {task}
      </div>
      {lines.length === 0 ? (
        <div className="text-[var(--color-text-3)]">waiting for agent output…</div>
      ) : (
        lines.map((l, i) => <div key={i} className="whitespace-pre-wrap">{l}</div>)
      )}
      <div ref={endRef} />
    </div>
  );
}
