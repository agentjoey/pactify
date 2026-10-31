import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { Button } from "../ui/Button";
import { Modal } from "../ui/Modal";
import type { Job } from "./types";
import type { JobsSource } from "./source";
import { responseError } from "./data";
import { panelBuild } from "../../buildTarget";
const labels: Record<string, string> = {
  cancel: "Cancel",
  resume: "Resume",
  dismiss: "Dismiss",
};
const consequences: Record<string, string> = {
  dismiss: "Dismiss this job? It stays in history and moves to Shipped. Any worktree is kept.",
  cancel:
    "This will stop the running job. Its worktree and recorded results will be kept.",
};
export const JobActions = panelBuild ? () => null : WriteJobActions;

function WriteJobActions({
  job,
  source,
  readOnly = false,
  onChanged,
  notify,
}: {
  job: Job;
  source: JobsSource;
  readOnly?: boolean;
  onChanged: () => void;
  notify: (text: string) => void;
}) {
  const [action, setAction] = useState("");
  const [confirmStep, setConfirmStep] = useState(1);
  const [instructions, setInstructions] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (!action) return;
    const elements = [...document.body.children].filter(
      (element) => !element.classList.contains("v2-modal-host"),
    ) as HTMLElement[];
    const previous = elements.map((element) => element.inert);
    elements.forEach((element) => {
      element.inert = true;
    });
    return () =>
      elements.forEach((element, index) => {
        element.inert = previous[index];
      });
  }, [action]);
  if (readOnly || !source.write) return null;
  async function execute() {
    if (busy) return;
    setBusy(true);
    try {
      const response = await source.write!(
        `${source.jobsPath}/${encodeURIComponent(job.id)}/${action}`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(
            action === "resume" ? { brief: instructions } : {},
          ),
        },
      );
      if (!response.ok) throw new Error(await responseError(response));
      if (action === "resume") {
        const body = await response.json();
        notify(`Started ${body.jobId}`);
      } else notify(`${labels[action]} completed`);
      setAction("");
      setConfirmStep(1);
      setInstructions("");
      onChanged();
    } catch (e) {
      notify((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  function closeAction() {
    if (busy) return;
    setAction("");
    setConfirmStep(1);
    setInstructions("");
  }
  const secondCancelStep = action === "cancel" && confirmStep === 2;
  return (
    <div className="job-actions" onClick={(e) => e.stopPropagation()}>
      {(job.actions || [])
        .filter((a) => labels[a])
        .map((a) => (
          <Button
            key={a}
            size="sm"
            variant="ghost"
            onClick={() => {
              setConfirmStep(1);
              setAction(a);
            }}
          >
            {labels[a]}
          </Button>
        ))}
      {action &&
        createPortal(
          <div className="v2-app v2-modal-host">
            <Modal
              title={
                action === "resume"
                  ? `Resume ${job.title || job.id}`
                  : `${labels[action]} ${job.title || job.id}?`
              }
              onClose={closeAction}
              footer={
                <>
                  {action === "cancel" && confirmStep === 1 ? (
                    <Button
                      variant="primary"
                      onClick={() => setConfirmStep(2)}
                    >
                      Confirm Cancel
                    </Button>
                  ) : (
                    <Button
                      variant="primary"
                      loading={busy}
                      disabled={action === "resume" && !instructions.trim()}
                      onClick={() => void execute()}
                    >
                      {action === "resume"
                        ? "Start"
                        : action === "cancel"
                          ? "Stop job"
                          : `Confirm ${labels[action]}`}
                    </Button>
                  )}
                  <Button
                    variant="ghost"
                    disabled={busy}
                    onClick={closeAction}
                  >
                    Back
                  </Button>
                </>
              }
            >
              {action === "resume" ? (
                <label className="routing-field">
                  Instructions for the next run
                  <textarea
                    autoFocus
                    rows={6}
                    value={instructions}
                    onChange={(e) => setInstructions(e.target.value)}
                  />
                </label>
              ) : secondCancelStep ? (
                <p>
                  Stop this job now? The harness stops immediately and the run
                  cannot be continued as it is.
                </p>
              ) : (
                <p>{consequences[action]}</p>
              )}
            </Modal>
          </div>,
          document.body,
        )}
    </div>
  );
}
