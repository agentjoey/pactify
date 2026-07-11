import type { RelayClient } from "@pactify-apps/relay-client";
import type { RpcRequest } from "@pactify-apps/wire";
import { project } from "@pactify-apps/pact-project";
import type { PactEvent as PactProjectEvent } from "@pactify-apps/pact-project";
import type { DataSource, DataSourceCapabilities } from "./datasource";
import type {
  ProjectStats,
  TaskStat,
  AgentStat,
  RunOrchestrateBody,
  CockpitEvent,
  CockpitStatus,
} from "./api";
import type { Machine, PactEvent, PactEventDetail, ProjectMeta, State } from "./types";

/**
 * RelaySource implements DataSource against the zero-knowledge pact relay: events
 * are pulled from the relay, decrypted client-side, and projected through the
 * shared pact-project fold. Pact verbs (assign/accept/changes/merge) are driven
 * remotely via the U3 down-channel — sent as pact.* rpc that the target machine's
 * `pactify serve --remote-control` executes locally (canWrite=true). Orchestrate
 * run/resume is driven remotely too (M4) via orchestrate.* rpc, so
 * canOrchestrate=true; effects arrive back through the event stream.
 */
/** Thrown by a locked RelaySource when a caller tries to decrypt content. */
export class RelayLockedError extends Error {
  constructor() {
    super("locked: master secret required to decrypt");
    this.name = "RelayLockedError";
  }
}

function lockedGuard(): never {
  throw new RelayLockedError();
}

export class RelaySource implements DataSource {
  private client: RelayClient;

  /** True when this source has no master secret in memory; cleartext metadata
   * (projects, machines) still works, but any path that decrypts event bodies is
   * guarded and throws {@link RelayLockedError}. */
  locked: boolean;

  capabilities: DataSourceCapabilities = {
    canWrite: true,
    canOrchestrate: true,
    multiMachine: true,
    cockpit: true,
  };

  constructor(client: RelayClient, opts?: { locked?: boolean }) {
    this.client = client;
    this.locked = opts?.locked ?? false;
  }

  async listProjects(): Promise<ProjectMeta[]> {
    const projects = await this.client.listProjects();
    for (const p of projects) this.projectNames.set(p.id, p.name);
    return projects.map((p) => ({
      id: p.id,
      name: p.name,
      path: p.id,
      project: p.id,
      feature_count: p.feature ? 1 : 0,
      awaiting_count: 0,
    }));
  }

  /** Relay project ids are composite (`accountId:name`) but the machine-side
   * dispatcher resolves rpcs by the REGISTERED PROJECT NAME. Every rpc
   * `project` field must carry the short name; the composite id stays for
   * REST/decrypt (the encryption key is derived from the composite id). */
  private projectNames = new Map<string, string>();
  private rpcProject(id: string): string {
    const known = this.projectNames.get(id);
    if (known) return known;
    const i = id.indexOf(":");
    return i >= 0 ? id.slice(i + 1) : id;
  }

  async getState(id: string, _wt?: string): Promise<State> {
    if (this.locked) lockedGuard();
    const events = await this.client.getProjectEvents(id);
    const decrypted = events.map(
      (e) => this.client.decryptRaw(id, e.bodyEnc) as PactProjectEvent,
    );
    return project(decrypted);
  }

  /**
   * The last `n` raw pact events in the `PactEvent` (SSE-frame) shape. The relay
   * stores each event body encrypted; decrypting it yields exactly the pact-project
   * PactEvent record (event_id / agent_id / role / event_type / task_id / feature /
   * payload) — the SAME shape the local serve's /events/log returns — so this is a
   * pure client-side derive over data the relay already holds, no new backend rpc.
   * `wt` (worktree) is ignored: hosted projects have no worktree addressing. Events
   * arrive in seq order; slice to the last `n` to match the local endpoint's cap.
   */
  async fetchEventsLog(id: string, _wt?: string, n?: number): Promise<PactEvent[]> {
    if (this.locked) lockedGuard();
    const events = await this.client.getProjectEvents(id);
    const decrypted = events.map(
      (e) => this.client.decryptRaw(id, e.bodyEnc) as PactEvent,
    );
    return n !== undefined && n > 0 ? decrypted.slice(-n) : decrypted;
  }

  async getEvents(id: string): Promise<PactEventDetail[]> {
    if (this.locked) lockedGuard();
    const events = await this.client.getProjectEvents(id);
    return events.map((e) => ({
      seq: e.seq,
      eventType: e.eventType,
      task: e.task,
      feature: e.feature,
      ts: e.ts,
      body: this.client.decryptRaw(id, e.bodyEnc) as Record<string, unknown>,
    }));
  }

  async getMachines(): Promise<Machine[]> {
    const machines = await this.client.listMachines();
    return machines.map((m) => ({
      machineId: m.machineId,
      host: m.host,
      agentKinds: m.agentKinds,
      workdirs: m.workdirs,
      online: m.online,
      lastSeenAt: m.lastSeenAt,
    }));
  }

  /** Pin remote commands to a specific machine (e.g. a user selection in the
   * Machines view). When unset, verb() targets the first online machine. */
  setTargetMachine(machineId: string): void {
    this.targetMachineId = machineId;
  }
  private targetMachineId = "";

  private async resolveMachineId(): Promise<string> {
    if (this.targetMachineId) return this.targetMachineId;
    const online = (await this.getMachines()).find((m) => m.online);
    if (!online) throw new Error("no online machine to run the command");
    return online.machineId;
  }

  /**
   * Drive a pact verb remotely: build the matching pact.* rpc and send it to the
   * target machine over the relay (fire-and-forget — the machine executes it
   * locally and the effect returns through the event stream, re-projecting the
   * board). Targets the pinned machine, else the first online one.
   */
  async verb(
    project: string,
    verb: "assign" | "accept" | "changes" | "merge",
    body: Record<string, unknown>,
  ): Promise<void> {
    const machineId = await this.resolveMachineId();
    const s = (k: string): string => (typeof body[k] === "string" ? (body[k] as string) : "");
    let rpc: RpcRequest;
    switch (verb) {
      case "assign":
        rpc = {
          type: "pact.assign",
          machineId,
          project: this.rpcProject(project),
          task: s("task"),
          feature: s("feature"),
          branch: s("branch"),
          owner: s("owner"),
          reviewer: s("reviewer"),
          spec: s("spec"),
          ...(Array.isArray(body.deps) ? { deps: body.deps as string[] } : {}),
        };
        break;
      case "accept":
        rpc = { type: "pact.accept", machineId, project: this.rpcProject(project), task: s("task") };
        break;
      case "changes":
        rpc = { type: "pact.changes", machineId, project: this.rpcProject(project), task: s("task"), reason: s("reason") };
        break;
      case "merge":
        rpc = { type: "pact.merge", machineId, project: this.rpcProject(project), feature: s("feature") };
        break;
    }
    this.client.sendRpc(rpc);
  }

  /**
   * Start the orchestrate driver on the target machine (M4). Fire-and-forget:
   * the machine runs the driver locally and its progress re-projects the board
   * through the event stream, so there is no status_url to poll here. Targets
   * the pinned machine, else the first online one — same as verb().
   */
  async runOrchestrate(
    project: string,
    body?: RunOrchestrateBody,
  ): Promise<{ status_url: string }> {
    const machineId = await this.resolveMachineId();
    const rpc: RpcRequest = {
      type: "orchestrate.run",
      machineId,
      project: this.rpcProject(project),
      ...(body?.feature ? { feature: body.feature } : {}),
      ...(body?.seat_kinds ? { seatKinds: body.seat_kinds } : {}),
    };
    this.client.sendRpc(rpc);
    return { status_url: "" };
  }

  /** Resume a paused orchestrate driver on the target machine (M4). */
  async resumeOrchestrate(
    project: string,
    body?: RunOrchestrateBody,
  ): Promise<{ status_url: string }> {
    const machineId = await this.resolveMachineId();
    const rpc: RpcRequest = {
      type: "orchestrate.resume",
      machineId,
      project: this.rpcProject(project),
      ...(body?.feature ? { feature: body.feature } : {}),
    };
    this.client.sendRpc(rpc);
    return { status_url: "" };
  }

  /**
   * Generate a plan on the target machine (M4). One-shot over the relay: the
   * plan.generate rpc runs the planner AND auto-applies the assigns on the
   * machine (the relay can't read the machine's files to offer a review/apply
   * round-trip), so the resulting tasks arrive on the board via the event
   * stream. There is no status to poll here — hence RelaySource deliberately
   * omits getPlanGenStatus/getPlanReview (see DispatchPanel's hosted branch).
   */
  async generatePlan(
    project: string,
    body: { goal: string; feature: string; planner_kind?: string },
  ): Promise<{ status_url: string; feature: string }> {
    const machineId = await this.resolveMachineId();
    const rpc: RpcRequest = {
      type: "plan.generate",
      machineId,
      project: this.rpcProject(project),
      goal: body.goal,
      feature: body.feature,
      ...(body.planner_kind ? { plannerKind: body.planner_kind } : {}),
    };
    this.client.sendRpc(rpc);
    return { status_url: "", feature: body.feature };
  }

  /**
   * Author a task draft on the target machine: send a `pact.task` rpc that writes
   * `.pact/tasks/{id}.md` (spec_md) and appends the task to the ledger locally.
   * Fire-and-forget like verb()/generatePlan() — the new task surfaces on the board
   * via the event stream, so there is nothing to return. Targets the pinned
   * machine, else the first online one. Mirrors PactTaskRequest in cloud/wire.
   */
  async postTask(project: string, body: { id: string; spec_md: string }): Promise<void> {
    const machineId = await this.resolveMachineId();
    const rpc: RpcRequest = {
      type: "pact.task",
      machineId,
      project: this.rpcProject(project),
      id: body.id,
      specMd: body.spec_md,
    };
    this.client.sendRpc(rpc);
  }

  /** Apply a previously-generated plan on the target machine (M4). Fire-and-
   * forget; the assigns arrive via the event stream, so the count is unknown
   * here (0). The relay flow auto-applies inside plan.generate, so this is only
   * for parity / an explicit re-apply. */
  async applyPlan(project: string, feature: string): Promise<{ assigned: number }> {
    const machineId = await this.resolveMachineId();
    const rpc: RpcRequest = {
      type: "plan.apply",
      machineId,
      project: this.rpcProject(project),
      feature,
    };
    this.client.sendRpc(rpc);
    return { assigned: 0 };
  }

  // ── Hosted cockpit (E3/E4) ─────────────────────────────────────────────────

  private cockpitSubs = new Map<string, CockpitSub>();
  private cockpitStatusCache = new Map<string, CockpitStatus>();

  private async sendCockpitSubscribe(project: string, seat: string): Promise<void> {
    const machineId = await this.resolveMachineId();
    const rpc: RpcRequest = { type: "cockpit.subscribe", machineId, project: this.rpcProject(project), seat };
    this.client.sendRpc(rpc);
  }

  async cockpitPrompt(
    project: string,
    seat: string,
    text: string,
  ): Promise<{ ok: boolean; threadId: string }> {
    const machineId = await this.resolveMachineId();
    this.client.sendRpc({ type: "cockpit.prompt", machineId, project: this.rpcProject(project), seat, text });
    return { ok: true, threadId: "" };
  }

  async cockpitRespond(
    project: string,
    seat: string,
    approvalId: string,
    decision: "allow" | "deny",
  ): Promise<void> {
    const machineId = await this.resolveMachineId();
    this.client.sendRpc({
      type: "cockpit.permission",
      machineId,
      project: this.rpcProject(project),
      seat,
      requestId: approvalId,
      decision,
    });
  }

  async cockpitCancel(project: string, seat: string): Promise<void> {
    const machineId = await this.resolveMachineId();
    this.client.sendRpc({ type: "cockpit.cancel", machineId, project: this.rpcProject(project), seat });
  }

  async cockpitResume(
    project: string,
    seat: string,
  ): Promise<{ ok: boolean; threadId: string }> {
    const machineId = await this.resolveMachineId();
    this.client.sendRpc({ type: "cockpit.resume", machineId, project: this.rpcProject(project), seat });
    return { ok: true, threadId: "" };
  }

  async cockpitStatus(project: string, seat: string): Promise<CockpitStatus> {
    const cached = this.cockpitStatusCache.get(`${project}:${seat}`);
    if (cached) return cached;
    return { threadId: "", capable: true, pending: [] };
  }

  cockpitSubscribe(
    project: string,
    seat: string,
    onEvent: (e: CockpitEvent) => void,
  ): () => void {
    if (this.locked) lockedGuard();
    const key = `${project}:${seat}`;
    // The machine builds runId from the rpc's SHORT project name, not the
    // composite relay id — match on the same.
    const runId = `cockpit:${this.rpcProject(project)}:${seat}`;
    const existing = this.cockpitSubs.get(key);
    if (existing) {
      const prev = existing.onEvent;
      existing.onEvent = (e) => {
        prev(e);
        onEvent(e);
      };
      return () => {
        if (this.cockpitSubs.get(key)?.onEvent === existing.onEvent) {
          existing.onEvent = prev;
        }
      };
    }
    const sub: CockpitSub = {
      onEvent,
      nextSeq: 0,
      buffer: new Map(),
      interval: setInterval(
        () => {
          void this.sendCockpitSubscribe(project, seat);
        },
        COCKPIT_RESUBSCRIBE_MS,
      ),
      offEphemeral: () => {},
    };
    this.cockpitSubs.set(key, sub);
    const off = this.client.onEphemeral((msg) => {
      if (msg.runId !== runId) return;
      let body: unknown;
      try {
        body = this.client.decryptRaw(project, msg.body);
      } catch {
        return;
      }
      if (!body || typeof body !== "object") return;
      const raw = body as Record<string, unknown>;
      if (raw.kind === "status") {
        const status = parseStatusSnapshot(raw);
        if (status) this.cockpitStatusCache.set(key, status);
        return;
      }
      if (raw.kind === "approval-request") {
        const status: CockpitStatus = {
          threadId: typeof raw.threadId === "string" ? raw.threadId : "",
          capable: true,
          resumable: raw.resumable === true,
          pending: parsePending(raw.pending),
        };
        this.cockpitStatusCache.set(key, status);
      }
      const seq = msg.seq;
      if (sub.nextSeq === 0) {
        // Adopt the server's base (the mirror's seq is 1-based) on first sight.
        sub.nextSeq = seq;
      } else if (seq === 1 && sub.nextSeq > 1) {
        // Mirror restarted server-side (TTL/serve restart): seq began again.
        sub.nextSeq = 1;
        sub.buffer.clear();
      }
      if (seq < sub.nextSeq) return;
      sub.buffer.set(seq, raw);
      while (sub.buffer.has(sub.nextSeq)) {
        const ev = sub.buffer.get(sub.nextSeq)!;
        sub.buffer.delete(sub.nextSeq);
        sub.nextSeq++;
        const mapped = mapCockpitEvent(ev);
        if (mapped) sub.onEvent(mapped);
      }
    });
    sub.offEphemeral = off;
    void this.sendCockpitSubscribe(project, seat);
    return () => {
      off();
      clearInterval(sub.interval);
      this.cockpitSubs.delete(key);
    };
  }

  async getStats(id: string): Promise<ProjectStats> {
    if (this.locked) lockedGuard();
    const state = await this.getState(id);
    const tasks: TaskStat[] = [];
    for (const f of state.features) {
      for (const t of f.tasks) {
        tasks.push({
          task_id: t.id,
          feature: f.id,
          owner: t.owner,
          reviewer: t.reviewer,
          status: t.status,
          duration_sec: 0,
          added: 0,
          deleted: 0,
          tokens: 0,
        });
      }
    }
    const agents: AgentStat[] = state.agents.map((a) => ({
      seat: a.id,
      tasks: 0,
      duration_sec: 0,
      added: 0,
      deleted: 0,
      tokens: 0,
      accepted: 0,
      reworked: 0,
    }));
    return { tasks, agents };
  }

  subscribe(
    id: string,
    onState: (s: State) => void,
    onEvent?: (e: PactEvent) => void,
    _onError?: () => void,
    onLive?: (live: boolean) => void,
  ): () => void {
    if (this.locked) {
      // No live decryption possible without the master secret. Return a no-op
      // unsubscribe so the dashboard can mount cleanly in locked session mode.
      return () => {};
    }
    return this.client.subscribe(
      id,
      async (e) => {
        // Forward the decrypted event to the live stream (Live view terminal +
        // per-task token metrics) — previously hosted only re-folded state and
        // the event stream stayed empty.
        if (onEvent) {
          try {
            onEvent(this.client.decryptRaw(id, e.bodyEnc) as PactEvent);
          } catch {
            /* a body we can't decrypt (key rotation / foreign project) is skipped */
          }
        }
        const state = await this.getState(id);
        onState(state);
      },
      onLive,
    );
  }
}

const COCKPIT_RESUBSCRIBE_MS = 4 * 60 * 1000;

interface CockpitSub {
  onEvent: (e: CockpitEvent) => void;
  nextSeq: number;
  buffer: Map<number, Record<string, unknown>>;
  interval: ReturnType<typeof setInterval>;
  offEphemeral: () => void;
}

function parsePending(raw: unknown): CockpitStatus["pending"] {
  if (!Array.isArray(raw)) return [];
  return raw
    .filter(
      (p): p is Record<string, unknown> =>
        p !== null && typeof p === "object" && typeof (p as Record<string, unknown>).id === "string",
    )
    .map((p) => ({
      id: String(p.id),
      kind: typeof p.kind === "string" ? p.kind : "",
      toolName: typeof p.toolName === "string" ? p.toolName : "",
      rawInput: p.rawInput,
      risk: typeof p.risk === "string" ? p.risk : undefined,
    }));
}

function parseStatusSnapshot(raw: Record<string, unknown>): CockpitStatus | null {
  return {
    threadId: typeof raw.threadId === "string" ? raw.threadId : "",
    capable: true,
    resumable: raw.resumable === true,
    reason: typeof raw.reason === "string" ? raw.reason : undefined,
    pending: parsePending(raw.pending),
  };
}

function mapCockpitEvent(raw: Record<string, unknown>): CockpitEvent | null {
  switch (raw.kind) {
    case "message":
      return {
        kind: "message",
        text: typeof raw.text === "string" ? raw.text : "",
        final: raw.final === true,
      };
    case "usage": {
      const usage =
        raw.usage && typeof raw.usage === "object"
          ? (raw.usage as Record<string, unknown>)
          : {};
      return { kind: "usage", usage };
    }
    case "state":
      return {
        kind: "state",
        state: raw.state as Record<string, unknown> | string,
      };
    case "error":
      return {
        kind: "error",
        err: typeof raw.err === "string" ? raw.err : "",
      };
    default:
      return null;
  }
}
