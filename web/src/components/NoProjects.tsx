import { useState } from "react";
import { postRegister } from "../lib/api";
import { humanizeError } from "../lib/protocolErrors";
import { CableMark } from "./TopBar";
import { Button } from "./ui/Button";
import { Input } from "./ui/Input";

// NoProjects — the empty-registry hero (spec §6.7). When `serve` boots with no
// registered repos there is nothing to show in ANY view, so this replaces the
// whole main area: cable mark + copy + a register form that reuses the exact
// ops postRegister API. On success it calls onRegistered (App.refreshProjects),
// which seeds the first project and tears this hero down. Errors are humanized.
export function NoProjects({ onRegistered }: { onRegistered: () => void }) {
  const [path, setPath] = useState("");
  const [name, setName] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  const register = async () => {
    if (path.trim().length === 0) return;
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
        还没有接入任何 repo
      </h2>
      <p className="mt-2 max-w-md text-sm text-[var(--color-text-3)]">
        Pactify 通过 <span className="mono">.pact/</span> 文件契约协同多个 agent。
        填入一个仓库的绝对路径,把它接入这块看板。
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
