import { useState } from "react";
import { createPortal } from "react-dom";
import { Button } from "../ui/Button";
import { Modal } from "../ui/Modal";
import { responseError } from "./data";
import type { JobsSource } from "./source";
import { storageBytes, storageItems, storageSummaries, storageRemovalDetails, type StoragePlan, type StorageRun } from "./Storage";
import { panelBuild } from "../../buildTarget";

export const StorageCleanup = panelBuild ? () => null : WriteStorageCleanup;
function WriteStorageCleanup({ source, path, onChanged }: { source: JobsSource; path: string; onChanged: (text: string) => void }) {
  const [plan, setPlan] = useState<StoragePlan | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function preview() {
    setBusy(true); setError("");
    try {
      const response = await source.fetch(path + "/plan", new AbortController().signal);
      if (!response.ok) throw Error(await responseError(response));
      const value = await response.json();
      if (!value.version || !Array.isArray(value.locations)) throw Error("Invalid cleanup plan");
      setPlan(value);
    } catch (e) { setError((e as Error).message); }
    finally { setBusy(false); }
  }
  async function execute() {
    if (!plan || !source.write || busy) return;
    setBusy(true); setError("");
    try {
      const response = await source.write(path + "/prune", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ version: plan.version }) });
      if (!response.ok) throw Error(await responseError(response));
      const result: StorageRun = await response.json();
      const bytes = result.removed.reduce((n, l) => n + l.bytes, 0);
      const details = result.removed.map(storageRemovalDetails).filter(Boolean).join("; ");
      onChanged(`Cleanup ${result.failures.length ? "completed with failures" : "completed"}; freed ${storageBytes(bytes)}${details}${result.failures.length ? ": " + result.failures.join("; ") : ""}`);
      setPlan(null);
    } catch (e) { setError((e as Error).message); }
    finally { setBusy(false); }
  }
  return <div>
    <Button variant="primary" loading={busy} onClick={() => void preview()}>Clean up now</Button>
    {error && !plan && <p role="alert">{error}</p>}
    {plan && createPortal(<div className="v2-app v2-modal-host"><Modal title="Confirm storage cleanup" variant="danger" onClose={() => { if (!busy) { setPlan(null); setError(""); } }} footer={<>
      <Button variant="danger" loading={busy} disabled={!!error} onClick={() => void execute()}>Confirm cleanup</Button>
      <Button variant="ghost" disabled={busy} onClick={() => { setPlan(null); setError(""); }}>Cancel</Button>
    </>}>
      <p>This permanently deletes eligible jobs, their harness sessions, old logs and cache entries. Deleted jobs are removed from cost history. Deleted Codex threads disappear from Remote Control. Pending worktrees are kept.</p>
      <div className="storage-plan">{plan.locations.map(l => <section key={l.name} aria-label={`${l.name} cleanup plan`}>
        <h3>{l.name}: {storageItems((l.items || []).filter(i => !i.protected).length)} · {storageBytes(l.reclaimable)} reclaimable</h3>
        <p>{storageBytes(l.bytes)} total</p>
        {storageSummaries(l).map(g => <details open key={g.action + g.reason}><summary><strong>{g.action === "KEEP" ? "Keep" : g.action === "SWEEP SESSIONS" ? "Clean orphan sessions" : "Remove"}</strong>: {storageItems(g.count)} · {storageBytes(g.bytes)} · {g.reason}</summary><ul>{g.examples.map(path => <li key={path}><code>{path}</code></li>)}</ul>{g.count > g.examples.length && <p>{storageItems(g.count - g.examples.length)} more; use <code>pactify sub prune --verbose</code> for the full list.</p>}</details>)}
      </section>)}</div>
      {plan.failures.map((f, i) => <p role="alert" key={i}>{f}</p>)}
      {error && <p role="alert">{error}. Cancel and review a new plan before retrying.</p>}
    </Modal></div>, document.body)}
  </div>;
}
