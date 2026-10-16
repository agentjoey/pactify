import type { Job } from "../src/components/jobs/types";

export function seedJobs(): Job[];
export const panelJobs: Job[];
interface FixtureRequest extends AsyncIterable<string | Uint8Array> {
  url?: string;
  method?: string;
}
interface FixtureResponse {
  writeHead(status: number, headers: Record<string, string>): unknown;
  end(body: string): unknown;
}
interface JobsAPI {
  (request: FixtureRequest, response: FixtureResponse, path: string): Promise<boolean>;
  reset(): void;
}
export function createJobsAPI(writable?: boolean): JobsAPI;
