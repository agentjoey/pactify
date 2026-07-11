import { useEffect, useState } from "react";
import { Button } from "./ui/Button";
import { Badge } from "./ui/Badge";
import { Alert } from "./ui/Alert";
import { Machines } from "./Machines";
import {
  fetchMe,
  fetchIdentities,
  fetchSessions,
  revokeSession,
  unlinkIdentity,
  logout,
  type MeResponse,
  type Identity,
  type WebSession,
} from "../lib/identity";

export function AccountPanel({ onLogout }: { onLogout: () => void }) {
  const [me, setMe] = useState<MeResponse | null>(null);
  const [identities, setIdentities] = useState<Identity[] | null>(null);
  const [sessions, setSessions] = useState<WebSession[] | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  async function load() {
    setLoading(true);
    setError("");
    try {
      const [m, ids, sess] = await Promise.all([
        fetchMe(),
        fetchIdentities(),
        fetchSessions(),
      ]);
      setMe(m);
      setIdentities(ids);
      setSessions(sess);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  async function handleUnlink(id: string) {
    try {
      await unlinkIdentity(id);
      setIdentities((prev) => (prev ? prev.filter((i) => i.id !== id) : prev));
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  async function handleRevoke(id: string) {
    try {
      await revokeSession(id);
      setSessions((prev) => (prev ? prev.filter((s) => s.id !== id) : prev));
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  async function handleLogout() {
    await logout().catch(() => {});
    onLogout();
  }

  if (loading) {
    return <div className="text-xs text-[var(--color-text-3)]">Loading account…</div>;
  }

  const tier = me?.accounts[0]?.tier ?? "free";

  return (
    <div data-testid="account-panel" className="flex flex-col gap-5">
      {error && <Alert tone="danger">{error}</Alert>}

      <section className="rounded-xl border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] p-4">
        <h3 className="mb-2 text-sm font-medium text-[var(--color-text-1)]">Signed in as</h3>
        <div className="flex items-center gap-2">
          <span className="text-sm text-[var(--color-text-1)]" data-testid="account-email">
            {me?.user.email ?? "—"}
          </span>
          <Badge color={tier === "personal" ? "role-design" : "role-dev"} data-testid="account-tier">
            {tier}
          </Badge>
        </div>
      </section>

      <section>
        <h3 className="mb-2 text-sm font-medium text-[var(--color-text-1)]">Identities</h3>
        {identities === null || identities.length === 0 ? (
          <div className="text-xs text-[var(--color-text-3)]">No identities linked.</div>
        ) : (
          <div className="flex flex-col gap-2">
            {identities.map((id) => (
              <div
                key={id.id}
                data-testid="identity-row"
                className="flex items-center justify-between rounded-xl border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-3 py-2"
              >
                <span className="text-xs text-[var(--color-text-1)] capitalize">{id.provider}</span>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => handleUnlink(id.id)}
                  disabled={identities.length <= 1}
                  title={identities.length <= 1 ? "Keep at least one identity" : undefined}
                >
                  Unlink
                </Button>
              </div>
            ))}
          </div>
        )}
      </section>

      <section>
        <h3 className="mb-2 text-sm font-medium text-[var(--color-text-1)]">Web sessions</h3>
        {sessions === null || sessions.length === 0 ? (
          <div className="text-xs text-[var(--color-text-3)]">No active sessions.</div>
        ) : (
          <div className="flex flex-col gap-2">
            {sessions.map((s) => (
              <div
                key={s.id}
                data-testid="session-row"
                className="flex items-center justify-between rounded-xl border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-3 py-2"
              >
                <div className="min-w-0">
                  <div className="truncate text-xs text-[var(--color-text-1)]">{s.ua ?? "Web session"}</div>
                  <div className="text-[10px] text-[var(--color-text-3)]">
                    since {new Date(s.createdAt).toLocaleDateString()}
                  </div>
                </div>
                <Button variant="ghost" size="sm" onClick={() => handleRevoke(s.id)}>
                  Revoke
                </Button>
              </div>
            ))}
          </div>
        )}
      </section>

      <section>
        <h3 className="mb-2 text-sm font-medium text-[var(--color-text-1)]">Machines</h3>
        <Machines />
      </section>

      <section>
        <Button variant="danger" size="sm" onClick={handleLogout} data-testid="account-logout">
          Sign out
        </Button>
      </section>
    </div>
  );
}
