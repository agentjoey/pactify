import { useEffect, useState } from "react";
import { CopyCommand } from "../src/components/jobs/NextSteps";
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
  const [ended, setEnded] = useState(false);
  useEffect(() => {
    const end = () => setEnded(true);
    window.addEventListener("pactify-panel-session-ended", end);
    return () => window.removeEventListener("pactify-panel-session-ended", end);
  }, []);
  if (ended) return (
    <main className="v2-app session-ended">
      <section className="panel-section">
        <h1>Session ended</h1>
        <p>Start a new panel session from your terminal, then open the link it prints.</p>
        <CopyCommand command="pactify sub panel" />
      </section>
    </main>
  );
  return <AppContent source={source} readOnly />;
}
