import { useEffect, useState, type FormEvent } from "react";
import { Button } from "./ui/Button";
import { Input } from "./ui/Input";
import { Alert } from "./ui/Alert";
import {
  relayUrl,
  connectRelaySource,
  connectSessionSource,
  bytesToHex,
  hexToBytes,
} from "../lib/source";
import type { RelaySource } from "../lib/relaysource";
import {
  fetchMe,
  sendMagicLink,
  createAccount,
  fetchLinkChallenge,
  linkAccount,
  clearIdentitySession,
  type MeResponse,
} from "../lib/identity";
import { generateMasterSecret, deriveAccountKeypair } from "@pactify-apps/crypto";

type View = "loading" | "login" | "magic-sent" | "onboarding";

interface IdentityGateProps {
  onSource: (source: RelaySource) => void;
}

export function IdentityGate({ onSource }: IdentityGateProps) {
  const [view, setView] = useState<View>("loading");
  const [me, setMe] = useState<MeResponse | null>(null);
  const [magicEmail, setMagicEmail] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    let alive = true;
    async function check() {
      try {
        const m = await fetchMe();
        if (!alive) return;
        if (m.accounts.length > 0) {
          const account = m.accounts[0];
          if (!account) throw new Error("no account");
          const src = await connectSessionSource(account.accountId);
          if (!alive) return;
          onSource(src);
        } else {
          setMe(m);
          setView("onboarding");
        }
      } catch {
        if (!alive) return;
        setView("login");
      }
    }
    void check();
    return () => {
      alive = false;
    };
  }, [onSource]);

  async function handleMagic(email: string) {
    setError("");
    setMagicEmail(email);
    try {
      await sendMagicLink(email);
      setView("magic-sent");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  async function handleMasterSecret(secret: string) {
    setError("");
    try {
      onSource(await connectRelaySource(secret));
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  function logout() {
    clearIdentitySession();
    setMe(null);
    setView("login");
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
      <div
        style={{
          width: "min(420px, 100%)",
          display: "flex",
          flexDirection: "column",
          gap: "14px",
        }}
      >
        <div>
          <h1 style={{ fontSize: "18px", fontWeight: 650, margin: 0 }}>pactify</h1>
          <p style={{ fontSize: "13px", opacity: 0.7, margin: "6px 0 0" }}>
            {relayUrl()} · hosted dashboard
          </p>
        </div>

        {error && <Alert tone="danger" title="Error">{error}</Alert>}

        {view === "loading" && (
          <div className="text-sm text-[var(--color-text-2)]">Checking session…</div>
        )}

        {view === "login" && (
          <LoginPanel
            onMagic={handleMagic}
            onMasterSecret={handleMasterSecret}
          />
        )}

        {view === "magic-sent" && (
          <MagicSentPanel
            email={magicEmail}
            onBack={() => setView("login")}
          />
        )}

        {view === "onboarding" && me && (
          <OnboardingPanel
            email={me.user.email}
            onSource={onSource}
            onLogout={logout}
          />
        )}
      </div>
    </div>
  );
}

function LoginPanel({
  onMagic,
  onMasterSecret,
}: {
  onMagic: (email: string) => void;
  onMasterSecret: (secret: string) => void;
}) {
  const [email, setEmail] = useState("");
  const [secret, setSecret] = useState("");
  const [showSecret, setShowSecret] = useState(false);
  const [busy, setBusy] = useState(false);

  async function submitMagic(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    await onMagic(email);
    setBusy(false);
  }

  async function submitSecret(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    await onMasterSecret(secret);
    setBusy(false);
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "12px" }}>
      <a
        href={`${relayUrl()}/v1/id/oauth/github/start`}
        className="inline-flex items-center justify-center gap-2 rounded-md bg-[var(--color-text-1)] px-3 py-2 text-xs font-medium text-[var(--color-bg-page)] transition-colors hover:opacity-90"
        data-testid="github-signin"
      >
        Sign in with GitHub
      </a>

      <div className="relative flex items-center gap-3">
        <div className="h-px flex-1 bg-[var(--color-border-subtle)]" />
        <span className="text-[11px] text-[var(--color-text-3)]">or</span>
        <div className="h-px flex-1 bg-[var(--color-border-subtle)]" />
      </div>

      <form onSubmit={submitMagic} style={{ display: "flex", flexDirection: "column", gap: "10px" }}>
        <Input
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          placeholder="email"
          autoComplete="email"
          aria-label="email"
        />
        <Button type="submit" loading={busy} disabled={email.trim() === ""}>
          Send magic link
        </Button>
      </form>

      <button
        type="button"
        onClick={() => setShowSecret((s) => !s)}
        className="self-start text-[11px] text-[var(--color-text-3)] underline underline-offset-2 hover:text-[var(--color-text-1)]"
        data-testid="toggle-secret"
      >
        {showSecret ? "Hide" : "Use master secret instead"}
      </button>

      {showSecret && (
        <form onSubmit={submitSecret} style={{ display: "flex", flexDirection: "column", gap: "10px" }}>
          <Input
            type="password"
            value={secret}
            onChange={(e) => setSecret(e.target.value)}
            placeholder="master secret (hex)"
            autoComplete="off"
            spellCheck={false}
            aria-label="master secret (hex)"
          />
          <Button type="submit" loading={busy} disabled={secret.trim() === ""}>
            Connect
          </Button>
        </form>
      )}
    </div>
  );
}

function MagicSentPanel({ email, onBack }: { email: string; onBack: () => void }) {
  return (
    <div className="rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] p-4">
      <div className="mb-2 text-sm font-medium text-[var(--color-text-1)]">Check your email</div>
      <p className="mb-3 text-xs text-[var(--color-text-2)]">
        A one-time sign-in link was sent to <strong className="text-[var(--color-text-1)]">{email}</strong>.
      </p>
      <Button variant="ghost" size="sm" onClick={onBack}>
        Back
      </Button>
    </div>
  );
}

type OnboardingMode = "choose" | "create" | "link";

function OnboardingPanel({
  email,
  onSource,
  onLogout,
}: {
  email: string;
  onSource: (source: RelaySource) => void;
  onLogout: () => void;
}) {
  const [mode, setMode] = useState<OnboardingMode>("choose");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [createdMaster, setCreatedMaster] = useState<Uint8Array | null>(null);
  const [savedConfirmed, setSavedConfirmed] = useState(false);

  async function handleCreate() {
    setError("");
    setBusy(true);
    try {
      const master = generateMasterSecret();
      const kp = deriveAccountKeypair(master);
      await createAccount(kp.publicKeyHex);
      setCreatedMaster(master);
      setMode("create");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  async function handleLink(secret: string) {
    setError("");
    setBusy(true);
    try {
      const master = hexToBytes(secret);
      const kp = deriveAccountKeypair(master);
      const { challenge } = await fetchLinkChallenge();
      const signature = kp.sign(challenge);
      await linkAccount({ publicKey: kp.publicKeyHex, challenge, signature });
      onSource(await connectRelaySource(secret));
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  if (mode === "create" && createdMaster) {
    const secretHex = bytesToHex(createdMaster);
    return (
      <div style={{ display: "flex", flexDirection: "column", gap: "12px" }}>
        <div className="text-sm font-medium text-[var(--color-text-1)]">Save your master secret</div>
        <p className="text-xs text-[var(--color-text-2)]">
          This is the only key that can decrypt your pact data. We cannot recover it.
        </p>
        <textarea
          readOnly
          value={secretHex}
          rows={3}
          className="w-full rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-page)] p-2 font-mono text-[11px] text-[var(--color-text-1)] outline-none"
          data-testid="created-secret"
        />
        <div className="flex gap-2">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => navigator.clipboard.writeText(secretHex)}
          >
            Copy
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              const blob = new Blob([secretHex], { type: "text/plain" });
              const url = URL.createObjectURL(blob);
              const a = document.createElement("a");
              a.href = url;
              a.download = "pactify-master-secret.txt";
              a.click();
              URL.revokeObjectURL(url);
            }}
          >
            Download .txt
          </Button>
        </div>
        <label className="flex items-start gap-2 text-xs text-[var(--color-text-2)]">
          <input
            type="checkbox"
            checked={savedConfirmed}
            onChange={(e) => setSavedConfirmed(e.target.checked)}
            data-testid="saved-confirm"
          />
          <span>I have saved the secret and understand it cannot be recovered.</span>
        </label>
        <Button
          loading={busy}
          disabled={!savedConfirmed}
          onClick={async () => {
            setBusy(true);
            try {
              onSource(await connectRelaySource(secretHex));
            } catch (e) {
              setError(e instanceof Error ? e.message : String(e));
            } finally {
              setBusy(false);
            }
          }}
        >
          Continue to dashboard
        </Button>
        {error && <Alert tone="danger">{error}</Alert>}
      </div>
    );
  }

  if (mode === "link") {
    return (
      <LinkAccountPanel
        onBack={() => setMode("choose")}
        onLink={handleLink}
        busy={busy}
        error={error}
      />
    );
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "12px" }}>
      <div className="text-sm font-medium text-[var(--color-text-1)]">
        Welcome, {email}
      </div>
      <p className="text-xs text-[var(--color-text-2)]">
        Your SSO identity is signed in. Link it to an existing pactify account, or create a new one.
      </p>
      <Button loading={busy} onClick={handleCreate} data-testid="create-account">
        Create new account
      </Button>
      <Button variant="ghost" onClick={() => setMode("link")} data-testid="link-account">
        Link existing account
      </Button>
      <button
        type="button"
        onClick={onLogout}
        className="self-start text-[11px] text-[var(--color-text-3)] underline underline-offset-2 hover:text-[var(--color-text-1)]"
      >
        Sign out
      </button>
    </div>
  );
}

function LinkAccountPanel({
  onBack,
  onLink,
  busy,
  error,
}: {
  onBack: () => void;
  onLink: (secret: string) => void;
  busy: boolean;
  error: string;
}) {
  const [secret, setSecret] = useState("");

  async function submit(e: FormEvent) {
    e.preventDefault();
    await onLink(secret);
  }

  return (
    <form onSubmit={submit} style={{ display: "flex", flexDirection: "column", gap: "12px" }}>
      <div className="text-sm font-medium text-[var(--color-text-1)]">Link existing account</div>
      <p className="text-xs text-[var(--color-text-2)]">
        Paste the account master secret to prove key possession.
      </p>
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
      <div className="flex gap-2">
        <Button type="button" variant="ghost" size="sm" onClick={onBack}>
          Back
        </Button>
        <Button type="submit" loading={busy} disabled={secret.trim() === ""}>
          Link
        </Button>
      </div>
    </form>
  );
}
