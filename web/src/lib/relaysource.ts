import type { RelayClient } from "@pactify-apps/relay-client";
import { project } from "@pactify-apps/pact-project";
import type { PactEvent as PactProjectEvent } from "@pactify-apps/pact-project";
import type { DataSource, DataSourceCapabilities } from "./datasource";
import type { ProjectStats, TaskStat, AgentStat } from "./api";
import type { ProjectMeta, State } from "./types";

/**
 * RelaySource implements DataSource against the zero-knowledge pact relay.
 * It is read-only: events are pulled from the relay, decrypted client-side,
 * and projected through the shared pact-project fold. Writes are gated out by
 * capabilities (canWrite=false) and left undefined.
 */
export class RelaySource implements DataSource {
  private client: RelayClient;

  capabilities: DataSourceCapabilities = {
    canWrite: false,
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
