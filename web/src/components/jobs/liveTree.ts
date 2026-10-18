import type { Job } from "./types";

export type ToolKind = "shell" | "edit" | "test" | "read";
export interface LiveTool {
  calls: number;
  active: number;
  pulse: number;
}
export interface LiveState {
  phase?: "running" | "done" | "failed";
  tools: Partial<Record<ToolKind, LiveTool>>;
  pending: Record<string, { kind?: ToolKind; counted: boolean }>;
  usage?: { inputTokens: number; outputTokens: number };
  costUSD?: number;
  diff?: { files: number; added: number; deleted: number };
  rateLimit?: string;
  log: { time: string; text: string }[];
  messageOpen?: boolean;
}
export const emptyLive = (): LiveState => ({ tools: {}, pending: {}, log: [] });
const record = (v: unknown): Record<string, unknown> =>
  v && typeof v === "object" && !Array.isArray(v)
    ? (v as Record<string, unknown>)
    : {};
const text = (v: unknown) => (typeof v === "string" ? v : "");
const count = (v: unknown): v is number =>
  typeof v === "number" && Number.isFinite(v) && v >= 0;

function toolKind(
  name: string,
  detail: string,
  explicit: string,
): ToolKind | undefined {
  const s = `${name} ${detail}`.toLowerCase();
  if (
    /\b(go test|vitest|pytest|playwright|cargo test|npm (run )?test|bats|tsc)\b/.test(
      s,
    )
  )
    return "test";
  if (
    explicit === "edit" ||
    /\b(apply_patch|edit|write|filechange|str_replace)\b/.test(s)
  )
    return "edit";
  if (
    explicit === "read" ||
    /\b(read|reading|cat|rg|grep|ls|glob|find|sed|head)\b/.test(s)
  )
    return "read";
  if (
    explicit === "shell" ||
    /\b(bash|shell|execute|exec_command|terminal|commandexecution|zsh)\b/.test(
      s,
    )
  )
    return "shell";
}

// Consume only the safe structured endpoint contract. No raw payload heuristics
// in the renderer, no unknown tools or fabricated agents.
export function reduceLive(
  previous: LiveState,
  events: readonly unknown[],
): LiveState {
  const state: LiveState = {
    ...previous,
    tools: structuredClone(previous.tools),
    pending: { ...previous.pending },
    log: previous.log.map((l) => ({ ...l })),
  };
  for (const value of events) {
    const e = record(value),
      t = record(e.tool);
    const stamp = text(e.time);
    const append = (s: string) => {
      if (s)
        state.log = [
          ...state.log,
          { time: stamp, text: s.slice(0, 240) },
        ].slice(-60);
    };
    if (e.kind === "state") {
      const phase =
        e.state === "turn_started"
          ? "running"
          : e.state === "turn_completed"
            ? "done"
            : e.state === "turn_failed"
              ? "failed"
              : undefined;
      if (phase) {
        state.phase = phase;
        state.messageOpen = false;
        if (phase !== "running") {
          state.pending = {};
          for (const tool of Object.values(state.tools)) tool.active = 0;
        }
      }
    } else if (e.kind === "tool") {
      if (!["start", "end", "output"].includes(text(t.phase))) continue;
      state.messageOpen = false;
      const id = text(t.id) || text(t.name) || "current";
      const previousCall = state.pending[id];
      const kind =
        previousCall?.kind ||
        toolKind(text(t.name), text(t.text), text(t.type));
      const terminal =
        t.phase === "end" || t.status === "completed" || t.status === "failed";
      if (kind) {
        const tool = (state.tools[kind] ||= { calls: 0, active: 0, pulse: 0 });
        if (!previousCall?.counted) {
          tool.calls++;
          if (!terminal) tool.active++;
        }
        if (terminal && previousCall?.counted)
          tool.active = Math.max(0, tool.active - 1);
        if (t.phase === "start" || terminal || !previousCall?.counted)
          tool.pulse++;
      }
      if (terminal) delete state.pending[id];
      else
        state.pending[id] = {
          kind,
          counted: !!kind || !!previousCall?.counted,
        };
      if (t.phase === "start" || terminal || (!previousCall?.counted && kind))
        append([text(t.name), text(t.text)].filter(Boolean).join(" · "));
    } else if (e.kind === "message" && text(e.text)) {
      const s = text(e.text);
      if (state.messageOpen && state.log.length) {
        const last = state.log[state.log.length - 1];
        last.text = (e.final ? s : last.text + s).slice(0, 240);
      } else append(s);
      state.messageOpen = !e.final;
    } else if (e.kind === "usage") {
      const u = record(e.usage);
      if (count(u.inputTokens) && count(u.outputTokens))
        state.usage = {
          inputTokens: u.inputTokens,
          outputTokens: u.outputTokens,
        };
      if (count(e.costUSD)) state.costUSD = e.costUSD;
    } else if (e.kind === "diff") {
      const d = record(e.diff);
      if (count(d.files) && count(d.added) && count(d.deleted))
        state.diff = { files: d.files, added: d.added, deleted: d.deleted };
    } else if (
      e.kind === "system" &&
      e.state === "rate_limits" &&
      text(e.rateLimit)
    )
      state.rateLimit = text(e.rateLimit);
  }
  return state;
}
export interface LiveEdge {
  from: string;
  to: string;
  kind: "rework" | "fallback" | "retry";
  count?: number;
}
export function liveEdges(jobs: Job[]): LiveEdge[] {
  const edges = new Map<string, LiveEdge>();
  const link = (from: string, to: string, kind: LiveEdge["kind"]) => {
    if (from && to && from !== to)
      edges.set(`${kind}:${from}:${to}`, { from, to, kind });
  };
  for (const j of jobs) {
    link(j.resumeOf, j.id, "rework");
    for (const id of j.resumedBy || []) link(j.id, id, "rework");
    link(j.fallbackFrom || "", j.id, "fallback");
    link(j.id, j.fallbackTo || "", "fallback");
    const n = Math.max(j.retryCount || 0, j.retryAttempt || 0);
    if (n)
      edges.set(`retry:${j.id}`, {
        from: j.id,
        to: j.id,
        kind: "retry",
        count: n,
      });
  }
  return [...edges.values()];
}
export function selectLiveJobs(
  jobs: Job[],
  id?: string,
): { jobs: Job[]; replay: boolean } {
  if (id)
    return {
      jobs: jobs.filter((j) => j.id === id),
      replay: !jobs.some((j) => j.id === id && j.status === "running"),
    };
  const running = jobs.filter(
    (j) => j.status === "running" || j.status === "retrying",
  );
  if (running.length) return { jobs: running, replay: false };
  const last = [...jobs]
    .filter((j) => j.startedAt || ["succeeded", "failed"].includes(j.status))
    .sort((a, b) =>
      (b.finishedAt || b.createdAt).localeCompare(a.finishedAt || a.createdAt),
    )[0];
  return { jobs: last ? [last] : [], replay: !!last };
}

// One batch per animation frame, including message bursts.
export function frameBatch<T>(
  consume: (values: T[]) => void,
  request = requestAnimationFrame,
  cancel = cancelAnimationFrame,
) {
  let queue: T[] = [],
    frame: number | undefined;
  return {
    add(value: T) {
      queue.push(value);
      if (frame === undefined)
        frame = request(() => {
          const batch = queue;
          queue = [];
          frame = undefined;
          consume(batch);
        });
    },
    dispose() {
      if (frame !== undefined) cancel(frame);
      frame = undefined;
      queue = [];
    },
  };
}
