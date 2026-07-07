import type { RelayClient } from "@pactify-apps/relay-client";
import type { RpcRequest } from "@pactify-apps/wire";
import { project } from "@pactify-apps/pact-project";
import type { PactEvent as PactProjectEvent } from "@pactify-apps/pact-project";
import type { DataSource, DataSourceCapabilities } from "./datasource";
import type { ProjectStats, TaskStat, AgentStat, RunOrchestrateBody } from "./api";
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
export class RelaySource implements DataSource {
  private client: RelayClient;

  capabilities: DataSourceCapabilities = {
    canWrite: true,
    canOrchestrate: true,
    multiMachine: true,
  };

  constructor(client: RelayClient) {
    this.client = client;
  }

  async listProjects(): Promise<ProjectMeta[]> {
    const projects = await this.client.listProjects();
    return projects.map((p) => ({
      id: p.id,
      name: p.name,
      path: p.id,
      project: p.id,
      feature_count: p.feature ? 1 : 0,
      awaiting_count: 0,
    }));
  }

  async getState(id: string, _wt?: string): Promise<State> {
    const events = await this.client.getProjectEvents(id);
    const decrypted = events.map(
      (e) => this.client.decrypt(id, e.bodyEnc) as PactProjectEvent,
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
    const events = await this.client.getProjectEvents(id);
    const decrypted = events.map(
      (e) => this.client.decrypt(id, e.bodyEnc) as PactEvent,
    );
    return n !== undefined && n > 0 ? decrypted.slice(-n) : decrypted;
  }

  async getEvents(id: string): Promise<PactEventDetail[]> {
    const events = await this.client.getProjectEvents(id);
    return events.map((e) => ({
      seq: e.seq,
      eventType: e.eventType,
      task: e.task,
      feature: e.feature,
      ts: e.ts,
      body: this.client.decrypt(id, e.bodyEnc) as Record<string, unknown>,
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
          project,
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
        rpc = { type: "pact.accept", machineId, project, task: s("task") };
        break;
      case "changes":
        rpc = { type: "pact.changes", machineId, project, task: s("task"), reason: s("reason") };
        break;
      case "merge":
        rpc = { type: "pact.merge", machineId, project, feature: s("feature") };
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
      project,
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
      project,
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
      project,
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
      project,
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
      project,
      feature,
    };
    this.client.sendRpc(rpc);
    return { assigned: 0 };
  }

  async getStats(id: string): Promise<ProjectStats> {
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
    return this.client.subscribe(
      id,
      async (e) => {
        // Forward the decrypted event to the live stream (Live view terminal +
        // per-task token metrics) — previously hosted only re-folded state and
        // the event stream stayed empty.
        if (onEvent) {
          try {
            onEvent(this.client.decrypt(id, e.bodyEnc) as PactEvent);
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
