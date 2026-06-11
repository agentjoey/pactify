import { useEffect, useState } from "react";
import type { WiringStatus, WireResult } from "../../lib/types";
import { getWiring, postWire } from "../../lib/api";
import { isSlug } from "../../lib/ops";
import { Modal } from "../ui/Modal";
import { Button } from "../ui/Button";
import { Input } from "../ui/Input";
import { Badge } from "../ui/Badge";

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
  const [copied, setCopied] = useState(false);

  const copySnippet = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // clipboard unavailable (insecure context / denied) — leave the select-all
      // pre as the fallback copy affordance.
    }
  };

  // A machine-global write only warrants the red consent line + checkbox when
  // the kind actually writes a global file. docOnly kinds (e.g. codex-app) never
  // write — only a snippet is shown — so they must not false-claim a global write.
  const showGlobalWarn = row.global && !row.docOnly;
  const seatOk = isSlug(seat);
  const rolesOk = roles.trim().length > 0;
  const ackOk = !showGlobalWarn || ack;
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

  const fields = (
    <>
      {showGlobalWarn && (
        <p data-testid="global-warn" className="text-[11px] text-[var(--color-danger)] mb-1.5">
          writes a machine-global file: {row.path || "(machine config)"}
        </p>
      )}

      <div className="flex gap-2 mb-1.5">
        <Input
          aria-label="seat"
          className="flex-1"
          placeholder="seat (slug)"
          value={seat}
          onChange={(e) => setSeat(e.target.value)}
        />
        <Input
          aria-label="roles"
          className="flex-1"
          placeholder="roles (e.g. worker)"
          value={roles}
          onChange={(e) => setRoles(e.target.value)}
        />
      </div>
      {seat && !seatOk && (
        <p className="text-[10px] text-[var(--color-danger)] mb-1.5">seat must match [a-z0-9][a-z0-9-]*</p>
      )}

      {showGlobalWarn && (
        <label className="flex items-center gap-1.5 text-[11px] text-[var(--color-text-2)] mb-1.5">
          <input type="checkbox" checked={ack} onChange={(e) => setAck(e.target.checked)} />
          I understand this writes a machine-global file
        </label>
      )}

      {err && <p className="mt-1.5 whitespace-pre-wrap text-[11px] text-[var(--color-danger)]">{err}</p>}

      {result && (
        <div className="mt-2">
          {result.wrote && !result.docOnly && (
            <p className="text-[11px] text-[var(--color-success)]">wired → {result.path}</p>
          )}
          {result.snippet && (
            <div>
              <div className="mb-1 flex items-center gap-2">
                <span className="text-[10px] font-semibold text-[var(--color-text-3)] uppercase">snippet (copy into your config)</span>
                <Button
                  data-testid="wire-snippet-copy"
                  variant="ghost"
                  size="sm"
                  className="ml-auto"
                  onClick={() => copySnippet(result.snippet!)}
                >
                  {copied ? "copied ✓" : "Copy"}
                </Button>
              </div>
              <pre
                data-testid="wire-snippet"
                className="text-[10px] text-[var(--color-success)] bg-[var(--color-bg-surface)] border border-[var(--color-border-subtle)] rounded p-2 whitespace-pre-wrap select-all cursor-text"
                onClick={(e) => {
                  const sel = window.getSelection();
                  if (sel) { sel.selectAllChildren(e.currentTarget); }
                }}
              >{result.snippet}</pre>
            </div>
          )}
        </div>
      )}
    </>
  );

  const footer = (
    <>
      <Button size="sm" disabled={!canConfirm} onClick={submit}>
        {confirmLabel}
      </Button>
      <Button variant="ghost" size="sm" onClick={onClose}>
        Close
      </Button>
    </>
  );

  // Machine-global writes are a destructive, consent-gated flow → present in a
  // danger Modal. The consent checkbox + exact machine-global path line above
  // are unchanged; only the container is the overlay Modal.
  if (showGlobalWarn) {
    return (
      <Modal
        testId={`wire-modal-${row.kind}`}
        variant="danger"
        title={`Wire ${row.kind}`}
        onClose={onClose}
        width="460px"
        footer={footer}
      >
        {fields}
      </Modal>
    );
  }

  return (
    <div data-testid={`wire-modal-${row.kind}`} className="mt-2 rounded border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] p-2">
      <div className="text-[10px] font-semibold text-[var(--color-text-3)] uppercase mb-1.5">Wire {row.kind}</div>
      {fields}
      <div className="mt-1.5 flex gap-1.5">{footer}</div>
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
      <h2 className="text-[10px] font-semibold text-[var(--color-text-3)] uppercase mb-2">Wiring · {project || "(no project)"}</h2>
      {err && <p className="text-[11px] text-[var(--color-danger)] whitespace-pre-wrap">{err}</p>}
      {rows.map((row) => (
        <div key={row.kind} data-testid={`wiring-row-${row.kind}`} className="border-t border-[var(--color-border-subtle)] py-1.5">
          <div className="flex items-center gap-2 text-[11px]">
            <span
              data-testid={`wire-dot-${row.kind}`}
              title={row.wired ? "wired" : "not wired"}
              className={`inline-block h-2 w-2 rounded-full ${row.wired ? "bg-[#3fb950]" : "bg-[#484f58]"}`}
            />
            <span className="font-semibold text-[var(--color-text-1)]">{row.kind}</span>
            <span className="text-[var(--color-text-2)]">{row.detail}</span>
            {row.global && <Badge color="warn">machine-global</Badge>}
            {author && !row.docOnly && (
              <Button
                variant="ghost"
                size="sm"
                className="ml-auto"
                onClick={() => setOpen(open === row.kind ? "" : row.kind)}
              >
                Wire
              </Button>
            )}
            {author && row.docOnly && (
              <Button
                variant="ghost"
                size="sm"
                className="ml-auto"
                onClick={() => setOpen(open === row.kind ? "" : row.kind)}
              >
                show snippet
              </Button>
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
