import { useEffect, useState } from "react";
import type { RegistryEntry } from "../../lib/types";
import { getRegistry, postRegister, deleteRegistry } from "../../lib/api";
import { relativeTime } from "../../lib/ops";

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
    if (!window.confirm(`Remove "${entry.name}" from the registry? (files are untouched)`)) return;
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
              <button
                data-testid={`project-remove-${e.name}`}
                className="rounded border border-gray-700 px-2 py-0.5 text-[10px] text-gray-400 hover:border-[#f85149] hover:text-[#f85149]"
                onClick={() => remove(e)}
              >
                Remove
              </button>
            )}
          </div>
        ))}
      </div>

      {author && (
        <div data-testid="register-form" className="rounded border border-gray-800 bg-[#0d1117] p-2">
          <div className="text-[10px] font-semibold text-gray-500 uppercase mb-1.5">Register a project</div>
          <div className="flex gap-2 mb-1.5">
            <input
              aria-label="path"
              className="flex-1 rounded border border-gray-700 bg-[#161b22] px-1.5 py-1 text-[11px] text-[#e6edf3]"
              placeholder="absolute path to repo"
              value={path}
              onChange={(ev) => setPath(ev.target.value)}
            />
            <input
              aria-label="name"
              className="w-32 rounded border border-gray-700 bg-[#161b22] px-1.5 py-1 text-[11px] text-[#e6edf3]"
              placeholder="name (optional)"
              value={name}
              onChange={(ev) => setName(ev.target.value)}
            />
            <button
              className="rounded bg-[#238636] px-2 py-0.5 text-[11px] font-semibold text-white disabled:opacity-50"
              disabled={busy || path.trim().length === 0}
              onClick={register}
            >
              Register
            </button>
          </div>
          {regErr && <p data-testid="register-error" className="text-[11px] text-[#f85149] whitespace-pre-wrap">{regErr}</p>}
        </div>
      )}
    </section>
  );
}
