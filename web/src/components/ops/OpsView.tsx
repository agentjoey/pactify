import { Projects } from "./Projects";
import { Wiring } from "./Wiring";
import { Seats } from "./Seats";

// OpsView composes the three ops panels — registry management, per-kind agent
// wiring, and seat provenance — stacked for the selected project. onRegistryChanged
// bubbles up so App can re-fetch the project switcher after a register/remove.
export function OpsView({
  project,
  author,
  refreshTick,
  onRegistryChanged,
}: {
  project: string;
  author: boolean;
  refreshTick?: number;
  onRegistryChanged?: () => void;
}) {
  return (
    <div data-testid="ops-view" className="flex-1 overflow-auto p-4 text-[#e6edf3]">
      <Projects author={author} onChanged={onRegistryChanged} />
      <Wiring project={project} author={author} refreshKey={refreshTick} />
      <Seats project={project} refreshKey={refreshTick} />
    </div>
  );
}
