import { useEffect, useMemo } from "react";
import type { Lens } from "../../App";

// Toolbar — shared product shell (ui2-shell). Left: wordmark.
// Center: Dashboard | Board | Cockpit lens segments with Cockpit pending badge.
// Right: ⌘K, live pill, dispatch (Cockpit lens only), Settings gear, user tile.

const LENSES: Lens[] = ["dashboard", "board", "cockpit"];

export function Toolbar({
  live,
  onOpenDispatch,
  onLensChange,
  lens,
  cockpitPending,
  profileEmail,
}: {
  live: boolean;
  onOpenDispatch: () => void;
  onLensChange: (lens: Lens) => void;
  lens: Lens;
  cockpitPending?: number;
  profileEmail?: string;
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const t = e.target as HTMLElement | null;
      if (
        !t ||
        t.tagName === "INPUT" ||
        t.tagName === "TEXTAREA" ||
        t.tagName === "SELECT" ||
        t.isContentEditable
      ) {
        return;
      }
      if (e.key === "1") onLensChange("dashboard");
      if (e.key === "2") onLensChange("board");
      if (e.key === "3") onLensChange("cockpit");
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onLensChange]);

  const monogram = useMemo(() => {
    if (profileEmail) return profileEmail.slice(0, 2).toLowerCase();
    return "cl";
  }, [profileEmail]);

  return (
    <div
      data-testid="toolbar"
      className="relative z-50 flex min-h-[48px] items-center gap-3 border-b border-[rgba(255,255,255,0.07)] bg-[rgba(12,17,25,0.82)] px-4 py-2 backdrop-blur-[12px]"
    >
      {/* left: wordmark */}
      <div className="flex shrink-0 items-center gap-2.5">
        <Wordmark />
        <span className="text-[13px] font-[700] tracking-[-0.01em] text-[var(--color-text-1)]">
          pactify
        </span>
      </div>

      <div className="mx-auto" />

      {/* center: lens segments */}
      <div
        data-testid="lens-segments"
        role="group"
        aria-label="lens"
        className="absolute left-1/2 top-1/2 hidden -translate-x-1/2 -translate-y-1/2 items-center gap-[3px] rounded-[9px] border border-[var(--border)] bg-[var(--bg-input)] p-[3px] sm:flex"
      >
        {LENSES.map((l) => {
          const active = lens === l;
          return (
            <button
              key={l}
              type="button"
              data-testid={`lens-${l}`}
              data-active={active ? "true" : "false"}
              aria-pressed={active}
              onClick={() => onLensChange(l)}
              className="relative inline-flex items-center gap-1 rounded-md px-[14px] py-[5px] text-[11.5px] font-semibold transition-colors"
              style={{
                color: active ? "var(--color-text-1)" : "var(--color-text-3)",
                background: active ? "var(--bg-elev2)" : "transparent",
                boxShadow: active ? "0 1px 2px rgba(0,0,0,.4)" : "none",
              }}
            >
              {l.charAt(0).toUpperCase() + l.slice(1)}
              {l === "cockpit" && (cockpitPending ?? 0) > 0 && (
                <span className="ml-0.5 inline-flex items-center gap-1 text-[var(--color-danger)]">
                  <span
                    className="h-[6px] w-[6px] rounded-full bg-[var(--color-danger)] shell-breath"
                    style={{ boxShadow: "0 0 6px var(--color-danger)" }}
                  />
                  <span className="font-mono text-[10px] font-semibold">
                    {cockpitPending}
                  </span>
                </span>
              )}
            </button>
          );
        })}
      </div>

      {/* right: ⌘K · dispatch (cockpit lens) · live · settings · user tile */}
      <div className="flex shrink-0 items-center gap-2.5">
        <button
          type="button"
          data-testid="cmdk-hint"
          aria-label="command palette"
          onClick={() => window.dispatchEvent(new CustomEvent("pactify:cmdk"))}
          className="inline-flex items-center gap-1.5 rounded-md border border-[rgba(255,255,255,0.10)] bg-[var(--bg)] px-2 py-1 text-[11px] text-[var(--color-text-3)] transition-colors hover:text-[var(--color-text-1)]"
        >
          ⌘K
        </button>
        {lens === "cockpit" && (
          <button
            type="button"
            data-testid="toolbar-dispatch"
            aria-label="dispatch"
            title="Dispatch a task from a goal"
            onClick={onOpenDispatch}
            className="grid h-[28px] w-[28px] place-items-center rounded-lg border border-[rgba(255,255,255,0.10)] bg-[var(--bg)] text-[13px] text-[var(--proj)] transition-colors hover:text-[var(--color-role-product)]"
          >
            ◉
          </button>
        )}
        <LiveBadge live={live} />
        <button
          type="button"
          data-testid="toolbar-settings"
          aria-label="settings"
          title="Settings"
          onClick={() => onLensChange("settings")}
          className="grid h-[28px] w-[28px] place-items-center rounded-lg border border-[rgba(255,255,255,0.10)] bg-[var(--bg)] text-[var(--color-text-3)] transition-colors hover:text-[var(--color-text-1)]"
          style={{
            color: lens === "settings" ? "var(--color-success)" : undefined,
          }}
        >
          <GearIcon />
        </button>
        <span
          data-testid="user-avatar-tile"
          title={profileEmail || "local"}
          className="grid h-[26px] w-[26px] place-items-center rounded-[8px] font-mono text-[11px] font-semibold text-[#0a0e14]"
          style={{
            background: "linear-gradient(135deg,#ffd479,#e0a93a)",
            boxShadow: "inset 0 0 0 1px rgba(255,255,255,.16)",
          }}
        >
          {monogram}
        </span>
      </div>
    </div>
  );
}

function Wordmark() {
  return (
    <svg width="26" height="16" viewBox="0 0 30 18" aria-hidden="true">
      <path
        d="M1 4 C10 4, 14 9, 29 9"
        stroke="#ffd479"
        strokeWidth="2.2"
        fill="none"
        strokeLinecap="round"
      />
      <path
        d="M1 9 L29 9"
        stroke="#8ab4ff"
        strokeWidth="2.2"
        fill="none"
        strokeLinecap="round"
      />
      <path
        d="M1 14 C10 14, 14 9, 29 9"
        stroke="#6ee7a0"
        strokeWidth="2.2"
        fill="none"
        strokeLinecap="round"
      />
    </svg>
  );
}

function GearIcon() {
  return (
    <svg
      width="15"
      height="15"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.9"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <circle cx="12" cy="12" r="3.2" />
      <path d="M19.4 15a1.6 1.6 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.6 1.6 0 0 0-2.7 1.1V21a2 2 0 1 1-4 0v-.1A1.6 1.6 0 0 0 6.6 19l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1A1.6 1.6 0 0 0 3 13.6H3a2 2 0 1 1 0-4h.1A1.6 1.6 0 0 0 4.6 6.6l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1A1.6 1.6 0 0 0 10 3.6V3a2 2 0 1 1 4 0v.1a1.6 1.6 0 0 0 2.7 1.1l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.6 1.6 0 0 0-.3 1.8" />
    </svg>
  );
}

function LiveBadge({ live }: { live: boolean }) {
  if (!live) return null;
  return (
    <span
      data-testid="live-badge"
      data-state="live"
      className="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[10.5px] font-medium"
      style={{
        color: "var(--color-success)",
        background: "color-mix(in srgb, var(--color-success) 12%, transparent)",
      }}
    >
      <span className="relative inline-flex h-[5px] w-[5px]">
        <span className="absolute inset-0 rounded-full bg-[var(--color-success)] shell-ring" />
        <span
          className="h-[5px] w-[5px] rounded-full bg-[var(--color-success)]"
          style={{ boxShadow: "0 0 6px var(--color-success)" }}
        />
      </span>
      live
    </span>
  );
}

export default Toolbar;
