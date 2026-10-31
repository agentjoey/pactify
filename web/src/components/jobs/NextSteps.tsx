import { useState } from "react";
import type { Job } from "./types";
import { Button } from "../ui/Button";

export function CopyCommand({ command }: { command: string }) {
  const [message, setMessage] = useState("");
  async function copy() {
    try {
      await navigator.clipboard.writeText(command);
      setMessage("Copied");
    } catch {
      setMessage("Could not copy. Select the command and copy it manually.");
    }
  }
  return (
    <div className="copy-command" onClick={(event) => event.stopPropagation()}>
      <code>{command}</code>
      <Button
        size="sm"
        variant="ghost"
        aria-label={`Copy ${command}`}
        onClick={() => void copy()}
      >
        Copy
      </Button>
      {message && <span role="status">{message}</span>}
    </div>
  );
}

export function nextCommands(job: Job): string[] {
  // Commands contain only a known CLI verb and a safe job id, never response
  // messages, session ids, URLs, repository paths or arbitrary action names.
  if (!/^[a-zA-Z0-9][a-zA-Z0-9_-]*$/.test(job.id)) return [];
  const terminal = ["succeeded", "failed", "canceled"].includes(job.status);
  const allowed = new Set(job.actions || []);
  const commands: string[] = [];
  const add = (action: string) => commands.push(`pactify sub ${action} ${job.id}`);
  if (terminal) {
    for (const action of ["apply", "discard"]) {
      if (allowed.has(action)) add(action);
    }
    if (allowed.has("resume")) {
      commands.push(
        `pactify sub run --resume ${job.id}${job.worktree ? " --worktree" : ""}`,
      );
    }
  } else if (allowed.has("cancel")) add("cancel");
  add("logs");
  if (terminal) {
    add("result");
    if (
      !job.dismissedAt &&
      (allowed.has("dismiss") || job.attention.some(a => ["failed", "warnings"].includes(a.kind)))
    ) add("dismiss");
  }
  return commands;
}

export function NextSteps({ job, compact = false, title = "Next steps" }: {
  job: Job;
  compact?: boolean;
  title?: string;
}) {
  const available = nextCommands(job);
  const priority = ["failed", "canceled"].includes(job.status)
    ? ["run", "logs", "result", "dismiss"]
    : ["running", "retrying", "queued"].includes(job.status)
      ? ["logs", "cancel"]
      : ["run", "logs", "result", "dismiss"];
  const commands = compact
    ? priority.flatMap(verb => available.filter(command =>
        command.startsWith(`pactify sub ${verb} `),
      )).slice(0, 2)
    : available;
  if (!commands.length) return null;
  return (
    <section className="next-steps" aria-label={title}>
      <h2>{title}</h2>
      {!compact && (
        <p>
          Run a command in your terminal.
          {commands.some(c => c.includes(" --resume ")) &&
            " Resume: type instructions, then Ctrl+D."}
        </p>
      )}
      {commands.map(command => <CopyCommand command={command} key={command} />)}
    </section>
  );
}

export function JobsOnboarding() {
  return (
    <section className="panel-section jobs-onboarding">
      <h2>Start your first job</h2>
      <p>
        In Claude Code, use <code>/delegate</code> or <code>/delegate-codex</code>, or start
        from your terminal:
      </p>
      <CopyCommand command="pactify sub run" />
      <p>Type your task, then press Ctrl+D. Check your setup with:</p>
      <CopyCommand command="pactify sub doctor" />
      <a
        href="https://github.com/agentjoey/pactify#readme"
        target="_blank"
        rel="noopener noreferrer"
      >
        Read the getting started guide
      </a>
    </section>
  );
}

export function LoadingJobs() {
  return (
    <div role="status" aria-label="Loading jobs" className="loading-jobs">
      <span className="sr-only">Loading jobs…</span>
      {["Queued", "Running", "Needs you", "Shipped"].map((name, index) => (
        <div className="loading-station" key={name} aria-hidden="true"><span className="section-label">0{index + 1} · {name}</span><div className="loading-skeleton" /></div>
      ))}
    </div>
  );
}
