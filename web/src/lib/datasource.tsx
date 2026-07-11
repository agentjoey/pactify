import { createContext, useContext } from "react";
import type { ReactNode } from "react";
import * as api from "./api";
import type {
  ProjectStats,
  RunOrchestrateBody,
  ShipBody,
  PlanGenStatus,
  CockpitStatus,
  CockpitEvent,
} from "./api";
import type {
  OrchestrateStatusResponse,
  ParallelStatusResponse,
  PlanReviewResponse,
  PactEvent,
} from "./types";
import type { Machine, PactEventDetail, ProjectMeta, State } from "./types";

export interface DataSourceCapabilities {
  canWrite: boolean;
  canOrchestrate: boolean;
  multiMachine: boolean;
  cockpit: boolean;
}

export interface DataSource {
  capabilities: DataSourceCapabilities;
  /** Hosted-mode sources may be locked (bearer-only, no master secret). */
  locked?: boolean;
  listProjects(): Promise<ProjectMeta[]>;
  getState(id: string, wt?: string): Promise<State>;
  getStats(id: string): Promise<ProjectStats>;
  subscribe(
    id: string,
    onState: (s: State) => void,
    onEvent?: (e: PactEvent) => void,
    onError?: () => void,
    onLive?: (live: boolean) => void,
  ): () => void;
  fetchEventsLog?(id: string, wt?: string, n?: number): Promise<PactEvent[]>;
  /** Hosted-mode sources may expose the full decrypted event history. */
  getEvents?(project: string): Promise<PactEventDetail[]>;
  /** Hosted-mode sources may expose the account's machine roster. */
  getMachines?(): Promise<Machine[]>;

  verb?(
    project: string,
    verb: "assign" | "accept" | "changes" | "merge",
    body: Record<string, unknown>,
  ): Promise<void>;
  postTask?(project: string, body: { id: string; spec_md: string }): Promise<void>;
  getOrchestrateStatus?(project: string): Promise<OrchestrateStatusResponse>;
  getParallelOrchestrate?(project: string): Promise<ParallelStatusResponse>;
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
  getDiff?(project: string): Promise<{ diff: string }>;
  generatePlan?(
    project: string,
    body: { goal: string; feature: string; planner_kind?: string },
  ): Promise<{ status_url: string; feature: string }>;
  getPlanGenStatus?(project: string): Promise<PlanGenStatus>;
  getPlanReview?(project: string, feature: string): Promise<PlanReviewResponse>;
  applyPlan?(project: string, feature: string): Promise<{ assigned: number }>;

  cockpitPrompt?(
    project: string,
    seat: string,
    text: string,
  ): Promise<{ ok: boolean; threadId: string }>;
  cockpitRespond?(
    project: string,
    seat: string,
    approvalId: string,
    decision: "allow" | "deny",
  ): Promise<void>;
  cockpitCancel?(project: string, seat: string): Promise<void>;
  cockpitResume?(project: string, seat: string): Promise<{ ok: boolean; threadId: string }>;
  cockpitStatus?(project: string, seat: string): Promise<CockpitStatus>;
  cockpitStreamUrl?(project: string, seat: string): string;
  /** Subscribe to a seat's cockpit event stream. Returns an unsubscribe function. */
  cockpitSubscribe?(
    project: string,
    seat: string,
    onEvent: (e: CockpitEvent) => void,
  ): () => void;
}

export class LocalServeSource implements DataSource {
  capabilities: DataSourceCapabilities = {
    canWrite: true,
    canOrchestrate: true,
    multiMachine: false,
    cockpit: true,
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

  subscribe(
    id: string,
    onState: (s: State) => void,
    onEvent?: (e: PactEvent) => void,
    onError?: () => void,
    onLive?: (live: boolean) => void,
  ): () => void {
    return api.subscribeEvents(
      id,
      (e) => {
        onEvent?.(e);
        api.fetchState(id).then(onState).catch(() => onError?.());
      },
      onLive,
    );
  }

  fetchEventsLog(id: string, wt?: string, n?: number): Promise<PactEvent[]> {
    return api.fetchEventsLog(id, wt, n);
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

  getOrchestrateStatus(project: string): Promise<OrchestrateStatusResponse> {
    return api.getOrchestrateStatus(project);
  }

  getParallelOrchestrate(project: string): Promise<ParallelStatusResponse> {
    return api.getParallelOrchestrate(project);
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
    return api.shipFeature(project, body ?? {});
  }

  getDiff(project: string): Promise<{ diff: string }> {
    return api.getDiff(project);
  }

  generatePlan(
    project: string,
    body: { goal: string; feature: string; planner_kind?: string },
  ): Promise<{ status_url: string; feature: string }> {
    return api.generatePlan(project, body);
  }

  getPlanGenStatus(project: string): Promise<PlanGenStatus> {
    return api.getPlanGenStatus(project);
  }

  getPlanReview(project: string, feature: string): Promise<PlanReviewResponse> {
    return api.getPlanReview(project, feature);
  }

  applyPlan(project: string, feature: string): Promise<{ assigned: number }> {
    return api.applyPlan(project, feature);
  }

  cockpitPrompt(project: string, seat: string, text: string): Promise<{ ok: boolean; threadId: string }> {
    return api.cockpitPrompt(project, seat, text);
  }

  cockpitRespond(
    project: string,
    seat: string,
    approvalId: string,
    decision: "allow" | "deny",
  ): Promise<void> {
    return api.cockpitRespond(project, seat, approvalId, decision);
  }

  cockpitCancel(project: string, seat: string): Promise<void> {
    return api.cockpitCancel(project, seat);
  }

  cockpitResume(project: string, seat: string): Promise<{ ok: boolean; threadId: string }> {
    return api.cockpitResume(project, seat);
  }

  cockpitStatus(project: string, seat: string): Promise<CockpitStatus> {
    return api.cockpitStatus(project, seat);
  }

  cockpitStreamUrl(project: string, seat: string): string {
    return api.cockpitStreamUrl(project, seat);
  }

  cockpitSubscribe(
    project: string,
    seat: string,
    onEvent: (e: CockpitEvent) => void,
  ): () => void {
    const es = new EventSource(api.cockpitStreamUrl(project, seat));
    es.onmessage = (e) => {
      try {
        onEvent(JSON.parse(e.data) as CockpitEvent);
      } catch {
        // ignore malformed frames
      }
    };
    return () => es.close();
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
