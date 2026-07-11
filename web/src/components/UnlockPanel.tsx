import { useState, type FormEvent } from "react";
import { Button } from "./ui/Button";
import { Input } from "./ui/Input";
import { Alert } from "./ui/Alert";
import { connectRelaySource } from "../lib/source";
import type { DataSource } from "../lib/datasource";

export function UnlockPanel({ onUnlock }: { onUnlock: (source: DataSource) => void }) {
  const [secret, setSecret] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      onUnlock(await connectRelaySource(secret));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div
      data-testid="unlock-panel"
      className="flex flex-1 flex-col items-center justify-center gap-4 overflow-hidden p-6"
    >
      <div className="max-w-sm rounded-xl border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] p-5 shadow-[var(--shadow-raised)]">
        <div className="mb-3 text-sm font-medium text-[var(--color-text-1)]">Content is locked</div>
        <p className="mb-4 text-xs leading-relaxed text-[var(--color-text-2)]">
          This board is end-to-end encrypted. Pair a device or paste your master secret to decrypt.
        </p>
        <form onSubmit={submit} className="flex flex-col gap-3">
          <Input
            type="password"
            value={secret}
            onChange={(e) => setSecret(e.target.value)}
            placeholder="master secret (hex)"
            autoComplete="off"
            spellCheck={false}
            aria-label="master secret (hex)"
          />
          {error && <Alert tone="danger">{error}</Alert>}
          <Button type="submit" loading={busy} disabled={secret.trim() === ""}>
            Unlock
          </Button>
        </form>
      </div>
    </div>
  );
}
