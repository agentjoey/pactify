import { useEffect, useRef } from "react";
import { Modal } from "../ui/Modal";
import { Projects } from "../ops/Projects";
import { AgentRoster } from "../ops/AgentRoster";
import { CustomAgentForm } from "../ops/CustomAgentForm";
import { AgentConfig } from "../ops/AgentConfig";
import { Wiring } from "../ops/Wiring";
import { Seats } from "../ops/Seats";

// SettingsModal — top-right ⚙ entry that integrates machine-level agent setup
// and the current project's seat wiring (replaces the standalone ops/setup views).
// When opened from a RosterDock seat gear, `focusSeat` carries that seat id: the
// modal scrolls straight to the project-seats section and names the seat, so the
// click lands on the seat's wiring context instead of the top Agents area.
export function SettingsModal({
  project,
  author,
  focusSeat,
  onClose,
}: {
  project: string;
  author: boolean;
  focusSeat?: string | null;
  onClose: () => void;
}) {
  const seatsRef = useRef<HTMLElement>(null);

  useEffect(() => {
    if (focusSeat) seatsRef.current?.scrollIntoView({ block: "start" });
  }, [focusSeat]);

  return (
    <Modal testId="settings-modal" title="Settings" width="720px" onClose={onClose}>
      <div className="flex max-h-[70vh] flex-col gap-4 overflow-auto">
        <section>
          <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-text-3)]">Agents (machine-level)</h3>
          <Projects author={author} onChanged={undefined} />
          <AgentRoster author={author} refreshKey={0} onChanged={undefined} />
          <CustomAgentForm author={author} onCreated={() => {}} />
          <AgentConfig refreshKey={0} />
        </section>
        <section ref={seatsRef} data-testid="settings-project-seats">
          <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-text-3)]">
            Project seats — {project}
            {focusSeat ? <span className="ml-1 text-[var(--color-accent,inherit)] normal-case">· {focusSeat}</span> : null}
          </h3>
          <Wiring project={project} author={author} refreshKey={0} />
          <Seats project={project} refreshKey={0} />
        </section>
      </div>
    </Modal>
  );
}
