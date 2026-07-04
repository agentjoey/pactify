import { useState, type FormEvent } from "react";
import { Button } from "./ui/Button";
import { Input } from "./ui/Input";
import { Alert } from "./ui/Alert";
import { connectRelaySource, relayUrl } from "../lib/source";
import type { RelaySource } from "../lib/relaysource";

/**
 * Hosted-mode gate: the dashboard talks to the zero-knowledge relay, so it needs
 * the account master secret to derive keys and decrypt event bodies client-side.
 * The user pastes it (hex); nothing is persisted — it lives in memory for the
 * session only. A device-pairing flow replaces the paste step later (backlog FE-8).
 */
export function RelayConnect({ onConnected }: { onConnected: (s: RelaySource) => void }) {
  const [secret, setSecret] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      onConnected(await connectRelaySource(secret));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div
      style={{
        minHeight: "100vh",
        display: "grid",
        placeItems: "center",
        padding: "24px",
      }}
    >
      <form
        onSubmit={submit}
        style={{ width: "min(420px, 100%)", display: "flex", flexDirection: "column", gap: "14px" }}
      >
        <div>
          <h1 style={{ fontSize: "18px", fontWeight: 650, margin: 0 }}>Connect to relay</h1>
          <p style={{ fontSize: "13px", opacity: 0.7, margin: "6px 0 0" }}>
            {relayUrl()} · zero-knowledge — your master secret is used locally to decrypt and never sent.
          </p>
        </div>
        <Input
          type="password"
          value={secret}
          onChange={(e) => setSecret(e.target.value)}
          placeholder="master secret (hex)"
          autoFocus
          autoComplete="off"
          spellCheck={false}
          aria-label="master secret (hex)"
        />
        {error && <Alert tone="danger" title="Could not connect">{error}</Alert>}
        <Button type="submit" loading={busy} disabled={secret.trim() === ""}>
          Connect
        </Button>
      </form>
    </div>
  );
}
