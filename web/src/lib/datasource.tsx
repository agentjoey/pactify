import { createContext, useContext } from "react";
import type { ReactNode } from "react";
import * as api from "./api";
import type {
  ProjectStats,
  RunOrchestrateBody,
  ShipBody,
} from "./api";
import type { ProjectMeta, State } from "./types";

export interface DataSourceCapabilities {
  canWrite: boolean;
  canOrchestrate: boolean;
  multiMachine: boolean;
}

export interface DataSource {
  capabilities: DataSourceCapabilities;
  listProjects(): Promise<ProjectMeta[]>;
  getState(id: string, wt?: string): Promise<State>;
  getStats(id: string): Promise<ProjectStats>;
  subscribe(id: string, onState: (s: State) => void): () => void;

  verb?(
    project: string,
    verb: "assign" | "accept" | "changes" | "merge",
    body: Record<string, unknown>,
  ): Promise<void>;
  postTask?(project: string, body: { id: string; spec_md: string }): Promise<void>;
  runOrchestrate?(
    project: string,
    body?: RunOrchestrateBody,
  ): Promise<{ status_url: string }>;
  resumeOrchestrate?(
    project: string,
    body?: RunOrchestrateBody,
  ): Promise<{ status_url: string }>;
  shipFeature?(
    project: string,
    body?: ShipBody,
  ): Promise<{ pushed: boolean; pr_url?: string }>;
  generatePlan?(
    project: string,
    body: { goal: string; feature: string; planner_kind?: string },
  ): Promise<{ status_url: string; feature: string }>;
  applyPlan?(project: string, feature: string): Promise<{ assigned: number }>;
}

export class LocalServeSource implements DataSource {
  capabilities: DataSourceCapabilities = {
    canWrite: true,
    canOrchestrate: true,
    multiMachine: false,
  };

  listProjects(): Promise<ProjectMeta[]> {
    return api.fetchProjects();
  }

  getState(id: string, wt?: string): Promise<State> {
    return api.fetchState(id, wt);
  }

  getStats(id: string): Promise<ProjectStats> {
    return api.getStats(id);
  }

  subscribe(id: string, onState: (s: State) => void): () => void {
    return api.subscribeEvents(id, async () => {
      const state = await api.fetchState(id);
      onState(state);
    });
  }

  verb(
    project: string,
    verb: "assign" | "accept" | "changes" | "merge",
    body: Record<string, unknown>,
  ): Promise<void> {
    return api.postVerb(project, verb, body);
  }

  postTask(project: string, body: { id: string; spec_md: string }): Promise<void> {
    return api.postTask(project, body);
  }

  runOrchestrate(
    project: string,
    body?: RunOrchestrateBody,
  ): Promise<{ status_url: string }> {
    return api.runOrchestrate(project, body);
  }

  resumeOrchestrate(
    project: string,
    body?: RunOrchestrateBody,
  ): Promise<{ status_url: string }> {
    return api.resumeOrchestrate(project, body);
  }

  shipFeature(
    project: string,
    body?: ShipBody,
  ): Promise<{ pushed: boolean; pr_url?: string }> {
    return api.shipFeature(project, body);
  }

  generatePlan(
    project: string,
    body: { goal: string; feature: string; planner_kind?: string },
  ): Promise<{ status_url: string; feature: string }> {
    return api.generatePlan(project, body);
  }

  applyPlan(project: string, feature: string): Promise<{ assigned: number }> {
    return api.applyPlan(project, feature);
  }
}

const DataSourceContext = createContext<DataSource | null>(null);

export function DataSourceProvider({
  children,
  source = new LocalServeSource(),
}: {
  children: ReactNode;
  source?: DataSource;
}) {
  return <DataSourceContext.Provider value={source}>{children}</DataSourceContext.Provider>;
}

export function useDataSource(): DataSource {
  const ctx = useContext(DataSourceContext);
  if (!ctx) {
    throw new Error("useDataSource must be used within DataSourceProvider");
  }
  return ctx;
}
