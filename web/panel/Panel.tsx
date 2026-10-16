import { Jobs } from "../src/components/jobs/Jobs";
import type { JobsSource } from "../src/components/jobs/source";
import { panelFetch } from "./auth";

const source: JobsSource = { jobsPath: "/api/jobs", costPath: "/api/cost", fetch: panelFetch };

export { JobList, LogView, filterJobs, splitDiff } from "../src/components/jobs/Jobs";
export type { Job } from "../src/components/jobs/Jobs";

export function Panel() {
  return <Jobs source={source} standalone />;
}
