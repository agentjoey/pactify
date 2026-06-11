import { useEffect, useState } from "react";
import type { RegistryEntry } from "../../lib/types";
import { getRegistry, postRegister, deleteRegistry } from "../../lib/api";
import { relativeTime } from "../../lib/ops";
import { Modal } from "../ui/Modal";
import { Button } from "../ui/Button";
import { Input } from "../ui/Input";

// Projects is the registry panel: one card per registered project (status badge,
// seat count, last activity), plus a Register form and per-card Remove. The
// register/remove mutations are hidden in observe-only mode. onChanged fires
// after any successful mutation so App can re-fetch the project switcher list.
export function Projects({
  author,
  onChanged,
}: {
  author: boolean;
  onChanged?: () => void;
}) {
  const [entries, setEntries] = useState<RegistryEntry[]>([]);
  const [loadErr, setLoadErr] = useState("");
  const [path, setPath] = useState("");
  const [name, setName] = useState("");
  const [regErr, setRegErr] = useState("");
  const [busy, setBusy] = useState(false);
  const [tick, setTick] = useState(0);
  // The project pending a Remove confirmation (null = no dialog open).
  const [pendingRemove, setPendingRemove] = useState<RegistryEntry | null>(null);

  useEffect(() => {
    let alive = true;
    getRegistry()
      .then((e) => { if (alive) { setEntries(e); setLoadErr(""); } })
      .catch((e) => { if (alive) setLoadErr(e instanceof Error ? e.message : String(e)); });
    return () => { alive = false; };
  }, [tick]);

  const refresh = () => { setTick((n) => n + 1); onChanged?.(); };

  const register = async () => {
    setBusy(true);
    setRegErr("");
    try {
      await postRegister(path.trim(), name.trim() || undefined);
      setPath("");
      setName("");
      refresh();
    } catch (e) {
      setRegErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const remove = async (entry: RegistryEntry) => {
    setPendingRemove(null);
    try {
      await deleteRegistry(entry.name);
      refresh();
    } catch (e) {
      setLoadErr(e instanceof Error ? e.message : String(e));
    }
  };

  return (
    <section data-testid="ops-projects" className="mb-4">
      <h2 className="text-[10px] font-semibold text-gray-500 uppercase mb-2">Projects · registry</h2>
      {loadErr && <p className="text-[11px] text-[#f85149] whitespace-pre-wrap mb-2">{loadErr}</p>}

      <div className="flex flex-col gap-1.5 mb-3">
        {entries.length === 0 && !loadErr && (
          <p className="text-[11px] text-gray-600">no registered projects</p>
        )}
        {entries.map((e) => (
          <div
            key={e.name}
            data-testid={`project-card-${e.name}`}
            className="flex items-center gap-2 rounded border border-gray-800 bg-[#0d1117] px-2 py-1.5 text-[11px]"
          >
            {e.status.valid ? (
              <span data-testid={`project-badge-${e.name}`} title="valid" className="text-[#3fb950]">✓</span>
            ) : (
              <span data-testid={`project-badge-${e.name}`} title="error" className="text-[#f85149]">✗</span>
            )}
            <span className="font-semibold text-[#e6edf3]">{e.name}</span>
            <span className="font-mono text-gray-600 truncate" title={e.path}>{e.path}</span>
            {e.status.valid ? (
              <span className="ml-auto flex items-center gap-2 text-gray-500">
                <span>{e.status.seats} seat{e.status.seats === 1 ? "" : "s"}</span>
                <span className="font-mono">{relativeTime(e.status.lastEventTs)}</span>
              </span>
            ) : (
              <span className="ml-auto text-[#f85149] whitespace-pre-wrap">{e.status.error || "invalid"}</span>
            )}
            {author && (
              <Button
                data-testid={`project-remove-${e.name}`}
                variant="ghost"
                size="sm"
                className="hover:border-[var(--color-danger)] hover:text-[var(--color-danger)]"
                onClick={() => setPendingRemove(e)}
              >
                Remove
              </Button>
            )}
          </div>
        ))}
      </div>

      {author && (
        <div data-testid="register-form" className="rounded border border-gray-800 bg-[#0d1117] p-2">
          <div className="text-[10px] font-semibold text-gray-500 uppercase mb-1.5">Register a project</div>
          <div className="flex gap-2 mb-1.5">
            <Input
              aria-label="path"
              className="flex-1"
              placeholder="absolute path to repo"
              value={path}
              onChange={(ev) => setPath(ev.target.value)}
            />
            <Input
              aria-label="name"
              className="w-32"
              placeholder="name (optional)"
              value={name}
              onChange={(ev) => setName(ev.target.value)}
            />
            <Button
              size="sm"
              disabled={busy || path.trim().length === 0}
              onClick={register}
            >
              Register
            </Button>
          </div>
          {regErr && <p data-testid="register-error" className="text-[11px] text-[var(--color-danger)] whitespace-pre-wrap">{regErr}</p>}
        </div>
      )}

      {pendingRemove && (
        <Modal
          testId="remove-confirm"
          variant="danger"
          title={`Remove ${pendingRemove.name}?`}
          onClose={() => setPendingRemove(null)}
          width="420px"
          footer={
            <>
              <Button variant="danger" onClick={() => remove(pendingRemove)}>
                Remove
              </Button>
              <Button variant="ghost" className="ml-auto" onClick={() => setPendingRemove(null)}>
                Cancel
              </Button>
            </>
          }
        >
          <p className="text-xs text-[var(--color-text-2)]">
            Remove “{pendingRemove.name}” from the registry? The files at{" "}
            <span className="font-mono text-[var(--color-text-1)]">{pendingRemove.path}</span>{" "}
            are untouched.
          </p>
        </Modal>
      )}
    </section>
  );
}
