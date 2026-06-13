import { Projects } from "./Projects";
import { Wiring } from "./Wiring";
import { Seats } from "./Seats";
import { AgentConfig } from "./AgentConfig";
import { OpsSkeleton } from "../Skeleton";

// OpsView composes the three ops panels — registry management, per-kind agent
// wiring, and seat provenance — stacked for the selected project. onRegistryChanged
// bubbles up so App can re-fetch the project switcher after a register/remove.
export function OpsView({
  project,
  author,
  refreshTick,
  onRegistryChanged,
  loading,
}: {
  project: string;
  author: boolean;
  refreshTick?: number;
  onRegistryChanged?: () => void;
  // First load of a project: the registry (Projects) is independent and stays
  // live, but the project-scoped Wiring/Seats panels show a skeleton until the
  // first snapshot lands.
  loading?: boolean;
}) {
  return (
    <div data-testid="ops-view" className="flex-1 overflow-auto p-4 text-[#e6edf3]">
      <Projects author={author} onChanged={onRegistryChanged} />
      <AgentConfig refreshKey={refreshTick} />
      {loading ? (
        <OpsSkeleton />
      ) : (
        <>
          <Wiring project={project} author={author} refreshKey={refreshTick} />
          <Seats project={project} refreshKey={refreshTick} />
        </>
      )}
    </div>
  );
}
