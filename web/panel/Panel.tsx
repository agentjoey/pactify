import { AppContent } from "../src/App";
import type { JobsSource } from "../src/components/jobs/source";
import { panelFetch } from "./auth";
const source: JobsSource = {
  jobsPath: "/api/jobs",
  costPath: "/api/cost",
  routerPath: "/api/router",
  harnessesPath: "/api/harnesses",
  fetch: panelFetch,
};
export {
  JobList,
  LogView,
  filterJobs,
  splitDiff,
} from "../src/components/jobs/Jobs";
export type { Job } from "../src/components/jobs/Jobs";
export function Panel() {
  return <AppContent source={source} readOnly />;
}
