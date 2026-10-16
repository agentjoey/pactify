export interface Job {
  actions?: string[];
  startedAt?: string;
  finishedAt?: string;
  costUSD?: number | null;
  id: string;
  title: string;
  harness: string;
  modelRequested: string;
  modelActual?: string;
  repo: string;
  cwd: string;
  access: string;
  configMode: string;
  status: string;
  duration: string;
  retryAttempt?: number;
  retryLimit?: number;
  retryCount?: number;
  failureClass?: string;
  quotaResetAt?: string;
  createdAt: string;
  usagePartial?: boolean;
  inputTokens: number;
  outputTokens: number;
  attention: { kind: string; message: string }[];
  activity?: {
    summary: string;
    inputTokens?: number;
    outputTokens?: number;
    usagePartial?: boolean;
  };
  worktree?: { path: string; baseCommit: string; status: string };
  fallbackFrom?: string;
  fallbackTo?: string;
  resumeOf: string;
  resumedBy: string[];
}
