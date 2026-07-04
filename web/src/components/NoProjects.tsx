import { useState } from "react";
import { postRegister } from "../lib/api";
import { humanizeError } from "../lib/protocolErrors";
import { isHostedMode } from "../lib/source";
import { CableMark } from "./shell/CableMark";
import { Button } from "./ui/Button";
import { Input } from "./ui/Input";

// NoProjects — the empty-registry hero (spec §6.7). When `serve` boots with no
// registered repos there is nothing to show in ANY view, so this replaces the
// whole main area: cable mark + copy + a register form that reuses the exact
// ops postRegister API. On success it calls onRegistered (App.refreshProjects),
// which seeds the first project and tears this hero down. Errors are humanized.
//
// HOSTED-AWARE: in hosted mode the browser talks to the zero-knowledge relay and
// CANNOT register a repo by local absolute path (there is no co-located serve and
// postRegister would hit a non-existent /api). So the register form is LOCAL-only;
// hosted shows guidance to connect a machine instead (projects appear once a
// machine running `pactify serve --relay-url … --remote-control` uploads them).
export function NoProjects({ onRegistered }: { onRegistered: () => void }) {
  if (isHostedMode()) return <NoProjectsHosted />;
  return <NoProjectsLocal onRegistered={onRegistered} />;
}

// NoProjectsHosted — hosted empty state: no local-path form (it can't work over
// the relay). Same CableMark hero + copy, guiding the user to connect a machine.
function NoProjectsHosted() {
  return (
    <div
      data-testid="no-projects"
      className="flex flex-1 flex-col items-center justify-center px-6 text-center"
    >
      <div className="scale-[2.2] mb-6 opacity-90">
        <CableMark />
      </div>
      <h2 className="text-lg font-semibold text-[var(--color-text-1)]">
        No projects yet on your account
      </h2>
      <p className="mt-2 max-w-md text-sm text-[var(--color-text-3)]">
        Projects appear here once a machine running Pactify connects to the relay.
        Connect one by running this on a machine that has your pact projects:
      </p>
      <div
        data-testid="no-projects-hosted-guidance"
        className="mt-6 w-full max-w-md rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-raised)] p-4 text-left"
      >
        <pre className="mono overflow-x-auto whitespace-pre-wrap break-words text-xs text-[var(--color-text-1)]">
          pactify serve --relay-url &lt;relay&gt; --remote-control
        </pre>
        <p className="mt-3 text-[11px] text-[var(--color-text-3)]">
          The machine encrypts and uploads its <span className="mono">.pact/</span> ledger to the
          zero-knowledge relay; its projects then show up on this board automatically.
        </p>
      </div>
    </div>
  );
}

// NoProjectsLocal — the original local empty-registry hero with the register-by-
// absolute-path form (unchanged behavior).
function NoProjectsLocal({ onRegistered }: { onRegistered: () => void }) {
  const [path, setPath] = useState("");
  const [name, setName] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  const register = async () => {
    // busy guard: Enter in either input calls this directly — a double Enter
    // must not fire a second concurrent POST (the loser 409s confusingly).
    if (busy || path.trim().length === 0) return;
    setBusy(true);
    setErr("");
    try {
      await postRegister(path.trim(), name.trim() || undefined);
      onRegistered();
    } catch (e) {
      setErr(humanizeError(e instanceof Error ? e.message : String(e)));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      data-testid="no-projects"
      className="flex flex-1 flex-col items-center justify-center px-6 text-center"
    >
      <div className="scale-[2.2] mb-6 opacity-90">
        <CableMark />
      </div>
      <h2 className="text-lg font-semibold text-[var(--color-text-1)]">
        No repos connected yet
      </h2>
      <p className="mt-2 max-w-md text-sm text-[var(--color-text-3)]">
        Pactify coordinates multiple agents through the <span className="mono">.pact/</span> file contract.
        Enter a repo's absolute path to connect it to this board.
      </p>

      <div
        data-testid="no-projects-form"
        className="mt-6 flex w-full max-w-md flex-col gap-2 rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-raised)] p-4 text-left"
      >
        <Input
          aria-label="path"
          autoFocus
          placeholder="absolute path to repo"
          value={path}
          onChange={(ev) => setPath(ev.target.value)}
          onKeyDown={(ev) => { if (ev.key === "Enter") register(); }}
        />
        <Input
          aria-label="name"
          placeholder="name (optional)"
          value={name}
          onChange={(ev) => setName(ev.target.value)}
          onKeyDown={(ev) => { if (ev.key === "Enter") register(); }}
        />
        <Button
          className="mt-1 self-end"
          size="sm"
          disabled={busy || path.trim().length === 0}
          onClick={register}
        >
          Register
        </Button>
        {err && (
          <p
            data-testid="no-projects-error"
            className="whitespace-pre-wrap text-[11px] text-[var(--color-danger)]"
          >
            {err}
          </p>
        )}
      </div>
    </div>
  );
}
