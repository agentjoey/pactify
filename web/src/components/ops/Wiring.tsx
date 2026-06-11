import { useEffect, useState } from "react";
import type { WiringStatus, WireResult } from "../../lib/types";
import { getWiring, postWire } from "../../lib/api";
import { isSlug } from "../../lib/ops";

// WireModal is the inline form for wiring one kind: seat id + roles, with a
// client-side slug check on the seat (server re-validates). Desktop (global)
// kinds add a red machine-global line + a required confirmation checkbox before
// Confirm is enabled. The server error is shown verbatim.
function WireModal({
  row,
  project,
  onClose,
  onWired,
}: {
  row: WiringStatus;
  project: string;
  onClose: () => void;
  onWired: () => void;
}) {
  const [seat, setSeat] = useState("");
  const [roles, setRoles] = useState("");
  const [ack, setAck] = useState(false);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [result, setResult] = useState<WireResult | null>(null);

  const seatOk = isSlug(seat);
  const rolesOk = roles.trim().length > 0;
  const ackOk = !row.global || ack;
  const canConfirm = seatOk && rolesOk && ackOk && !busy;

  // docOnly kinds (codex) write only the entry; the action shows the snippet.
  const confirmLabel = row.docOnly ? "wire entry + show snippet" : "Confirm";

  const submit = async () => {
    setBusy(true);
    setErr("");
    try {
      const res = await postWire(project, row.kind, { seat, roles: roles.trim() });
      setResult(res);
      onWired();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div data-testid={`wire-modal-${row.kind}`} className="mt-2 rounded border border-gray-700 bg-[#0d1117] p-2">
      <div className="text-[10px] font-semibold text-gray-500 uppercase mb-1.5">Wire {row.kind}</div>

      {row.global && (
        <p data-testid="global-warn" className="text-[11px] text-[#f85149] mb-1.5">
          writes a machine-global file: {row.detail.replace(/^config /, "") || "(machine config)"}
        </p>
      )}

      <div className="flex gap-2 mb-1.5">
        <input
          aria-label="seat"
          className="flex-1 rounded border border-gray-700 bg-[#161b22] px-1.5 py-1 text-[11px] text-[#e6edf3]"
          placeholder="seat (slug)"
          value={seat}
          onChange={(e) => setSeat(e.target.value)}
        />
        <input
          aria-label="roles"
          className="flex-1 rounded border border-gray-700 bg-[#161b22] px-1.5 py-1 text-[11px] text-[#e6edf3]"
          placeholder="roles (e.g. worker)"
          value={roles}
          onChange={(e) => setRoles(e.target.value)}
        />
      </div>
      {seat && !seatOk && (
        <p className="text-[10px] text-[#f85149] mb-1.5">seat must match [a-z0-9][a-z0-9-]*</p>
      )}

      {row.global && (
        <label className="flex items-center gap-1.5 text-[11px] text-gray-400 mb-1.5">
          <input type="checkbox" checked={ack} onChange={(e) => setAck(e.target.checked)} />
          I understand this writes a machine-global file
        </label>
      )}

      <div className="flex gap-1.5">
        <button
          className="rounded bg-[#238636] px-2 py-0.5 text-[11px] font-semibold text-white disabled:opacity-50"
          disabled={!canConfirm}
          onClick={submit}
        >
          {confirmLabel}
        </button>
        <button
          className="rounded border border-gray-700 px-2 py-0.5 text-[11px] text-gray-400"
          onClick={onClose}
        >
          Close
        </button>
      </div>

      {err && <p className="mt-1.5 whitespace-pre-wrap text-[11px] text-[#f85149]">{err}</p>}

      {result && (
        <div className="mt-2">
          {result.wrote && !result.docOnly && (
            <p className="text-[11px] text-[#3fb950]">wired → {result.path}</p>
          )}
          {result.snippet && (
            <div>
              <div className="text-[10px] font-semibold text-gray-500 uppercase mb-1">snippet (copy into your config)</div>
              <pre
                data-testid="wire-snippet"
                className="text-[10px] text-green-400 bg-[#0d1117] border border-gray-800 rounded p-2 whitespace-pre-wrap select-all cursor-text"
                onClick={(e) => {
                  const sel = window.getSelection();
                  if (sel) { sel.selectAllChildren(e.currentTarget); }
                }}
              >{result.snippet}</pre>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// Wiring lists each agent kind for the selected project with a content-aware
// status dot, detail text, and a machine-global tag for desktop kinds. A Wire
// button opens an inline modal. Mutations are hidden when !author.
export function Wiring({
  project,
  author,
  refreshKey,
}: {
  project: string;
  author: boolean;
  refreshKey?: number;
}) {
  const [rows, setRows] = useState<WiringStatus[]>([]);
  const [err, setErr] = useState("");
  const [open, setOpen] = useState<string>("");
  const [localTick, setLocalTick] = useState(0);

  useEffect(() => {
    if (!project) {
      setRows([]);
      return;
    }
    let alive = true;
    getWiring(project)
      .then((w) => { if (alive) { setRows(w); setErr(""); } })
      .catch((e) => { if (alive) setErr(e instanceof Error ? e.message : String(e)); });
    return () => { alive = false; };
  }, [project, refreshKey, localTick]);

  return (
    <section data-testid="ops-wiring" className="mb-4">
      <h2 className="text-[10px] font-semibold text-gray-500 uppercase mb-2">Wiring · {project || "(no project)"}</h2>
      {err && <p className="text-[11px] text-[#f85149] whitespace-pre-wrap">{err}</p>}
      {rows.map((row) => (
        <div key={row.kind} data-testid={`wiring-row-${row.kind}`} className="border-t border-gray-800 py-1.5">
          <div className="flex items-center gap-2 text-[11px]">
            <span
              data-testid={`wire-dot-${row.kind}`}
              title={row.wired ? "wired" : "not wired"}
              className={`inline-block h-2 w-2 rounded-full ${row.wired ? "bg-[#3fb950]" : "bg-[#484f58]"}`}
            />
            <span className="font-semibold text-[#e6edf3]">{row.kind}</span>
            <span className="text-gray-500">{row.detail}</span>
            {row.global && (
              <span className="rounded px-1 py-0.5 text-[10px]" style={{ background: "#21262d", color: "#ffd479" }}>
                machine-global
              </span>
            )}
            {author && !row.docOnly && (
              <button
                className="ml-auto rounded border border-gray-700 px-2 py-0.5 text-[10px] text-[#8ab4ff] hover:border-[#8ab4ff]"
                onClick={() => setOpen(open === row.kind ? "" : row.kind)}
              >
                Wire
              </button>
            )}
            {author && row.docOnly && (
              <button
                className="ml-auto rounded border border-gray-700 px-2 py-0.5 text-[10px] text-[#8ab4ff] hover:border-[#8ab4ff]"
                onClick={() => setOpen(open === row.kind ? "" : row.kind)}
              >
                show snippet
              </button>
            )}
          </div>
          {author && open === row.kind && (
            <WireModal
              row={row}
              project={project}
              onClose={() => setOpen("")}
              onWired={() => setLocalTick((n) => n + 1)}
            />
          )}
        </div>
      ))}
    </section>
  );
}
