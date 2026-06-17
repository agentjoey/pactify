import { Modal } from "../ui/Modal";
import { Projects } from "../ops/Projects";
import { AgentRoster } from "../ops/AgentRoster";
import { CustomAgentForm } from "../ops/CustomAgentForm";
import { AgentConfig } from "../ops/AgentConfig";
import { Wiring } from "../ops/Wiring";
import { Seats } from "../ops/Seats";

// SettingsModal — top-right ⚙ entry that integrates machine-level agent setup
// and the current project's seat wiring (replaces the standalone ops/setup views).
export function SettingsModal({
  project,
  author,
  onClose,
}: {
  project: string;
  author: boolean;
  onClose: () => void;
}) {
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
        <section>
          <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-text-3)]">Project seats — {project}</h3>
          <Wiring project={project} author={author} refreshKey={0} />
          <Seats project={project} refreshKey={0} />
        </section>
      </div>
    </Modal>
  );
}
