import type { RelayClient } from "@pactify-apps/relay-client";
import type { RpcRequest } from "@pactify-apps/wire";
import { project } from "@pactify-apps/pact-project";
import type { PactEvent as PactProjectEvent } from "@pactify-apps/pact-project";
import type { DataSource, DataSourceCapabilities } from "./datasource";
import type { ProjectStats, TaskStat, AgentStat } from "./api";
import type { PactEventDetail, ProjectMeta, State } from "./types";

/**
 * RelaySource implements DataSource against the zero-knowledge pact relay: events
 * are pulled from the relay, decrypted client-side, and projected through the
 * shared pact-project fold. Pact verbs (assign/accept/changes/merge) are driven
 * remotely via the U3 down-channel — sent as pact.* rpc that the target machine's
 * `pactify serve --remote-control` executes locally (canWrite=true). Orchestrate
 * (run/ship/plan) is NOT an rpc verb, so canOrchestrate stays false.
 */
export class RelaySource implements DataSource {
  private client: RelayClient;

  capabilities: DataSourceCapabilities = {
    canWrite: true,
    canOrchestrate: false,
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

  /**
   * Drive a pact verb remotely: build the matching pact.* rpc and send it to this
   * account's machine over the relay (fire-and-forget — the machine executes it
   * and the effect returns through the event stream, re-projecting the board).
   * machineId is the account id (MVP one-machine-per-account).
   */
  async verb(
    project: string,
    verb: "assign" | "accept" | "changes" | "merge",
    body: Record<string, unknown>,
  ): Promise<void> {
    const machineId = this.client.account();
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
    }));
    return { tasks, agents };
  }

  subscribe(id: string, onState: (s: State) => void): () => void {
    return this.client.subscribe(id, async () => {
      const state = await this.getState(id);
      onState(state);
    });
  }
}
