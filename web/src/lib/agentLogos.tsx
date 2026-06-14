import type { ReactNode } from "react";

// Agent logos — a UNIFIED set of stylized brand marks (v1, in-house / not the
// trademarked originals). Every kind gets the same treatment: a rounded tile,
// the brand accent at low opacity as the fill, and a simple single-color glyph
// in the accent ink — so a row of mixed agents reads as one consistent family,
// light-theme adapted. One <AgentLogo kind=.. size=../> replaces the per-kind
// Phosphor concept icons on agent cards once approved.

type LogoSpec = { accent: string; glyph: ReactNode };

// Each glyph is drawn in a 24×24 viewBox, single-color via currentColor so the
// tile can ink it with the brand accent.
const SPARK = (
  <g stroke="currentColor" strokeWidth="1.7" strokeLinecap="round">
    <path d="M12 4.5V9M12 15v4.5M4.5 12H9M15 12h4.5M6.7 6.7l3.2 3.2M14.1 14.1l3.2 3.2M17.3 6.7l-3.2 3.2M9.9 14.1l-3.2 3.2" />
  </g>
);
const TERMINAL = (
  <g stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" fill="none">
    <path d="M6 8l3.5 3.5L6 15" />
    <path d="M12.5 15.5H18" />
  </g>
);
const SPARKLE4 = (
  <path d="M12 3.2c.5 4.2 1.6 5.3 5.8 5.8-4.2.5-5.3 1.6-5.8 5.8-.5-4.2-1.6-5.3-5.8-5.8 4.2-.5 5.3-1.6 5.8-5.8z" fill="currentColor" />
);
const KNOT = (
  <g stroke="currentColor" strokeWidth="1.5" fill="none">
    <ellipse cx="12" cy="12" rx="7" ry="3.1" />
    <ellipse cx="12" cy="12" rx="7" ry="3.1" transform="rotate(60 12 12)" />
    <ellipse cx="12" cy="12" rx="7" ry="3.1" transform="rotate(120 12 12)" />
  </g>
);
const CURSOR = (
  <path d="M7 5l11 5.2-4.7 1.5-1.5 4.8L7 5z" fill="currentColor" />
);
const MOON = (
  <path d="M15.5 5.2A7 7 0 1 0 18.8 15 5.6 5.6 0 0 1 15.5 5.2z" fill="currentColor" />
);
const ORBIT = (
  <g stroke="currentColor" strokeWidth="1.6" fill="none" strokeLinecap="round">
    <ellipse cx="12" cy="13" rx="7.5" ry="3.4" transform="rotate(-22 12 13)" />
    <path d="M12 13.5V6.5M9 9l3-3 3 3" strokeLinejoin="round" />
  </g>
);
const ROBOT = (
  <g stroke="currentColor" strokeWidth="1.6" fill="none" strokeLinejoin="round">
    <rect x="5.5" y="8" width="13" height="9" rx="2.5" />
    <path d="M12 8V5M9.5 12.5h.01M14.5 12.5h.01" strokeLinecap="round" />
  </g>
);

const LOGOS: Record<string, LogoSpec> = {
  "claude-code": { accent: "#c2603f", glyph: SPARK },
  "claude-desktop": { accent: "#c2603f", glyph: SPARK },
  opencode: { accent: "#0f8a44", glyph: TERMINAL },
  "gemini-cli": { accent: "#2563eb", glyph: SPARKLE4 },
  "codex-cli": { accent: "#0e8c6e", glyph: KNOT },
  "codex-app": { accent: "#0e8c6e", glyph: KNOT },
  "cursor-cli": { accent: "#0f7e86", glyph: CURSOR },
  "kimi-cli": { accent: "#6b4ef0", glyph: MOON },
  antigravity: { accent: "#9b3fd4", glyph: ORBIT },
};

// resolve maps a binding kind onto a logo, falling back by family then to the
// generic robot — never throws.
function resolve(kind: string): LogoSpec {
  if (LOGOS[kind]) return LOGOS[kind];
  if (kind.startsWith("claude")) return LOGOS["claude-code"];
  if (kind.startsWith("gemini")) return LOGOS["gemini-cli"];
  if (kind.startsWith("codex")) return LOGOS["codex-cli"];
  if (kind.startsWith("cursor")) return LOGOS["cursor-cli"];
  if (kind.startsWith("kimi")) return LOGOS["kimi-cli"];
  return { accent: "var(--color-text-2)", glyph: ROBOT };
}

export const AGENT_LOGO_KINDS = [
  "claude-code",
  "claude-desktop",
  "opencode",
  "gemini-cli",
  "codex-cli",
  "codex-app",
  "cursor-cli",
  "kimi-cli",
  "antigravity",
];

export function AgentLogo({ kind, size = 28 }: { kind: string; size?: number }) {
  const { accent, glyph } = resolve(kind);
  const pad = Math.round(size * 0.18);
  return (
    <span
      data-testid="agent-logo"
      title={kind}
      style={{
        width: size,
        height: size,
        background: `color-mix(in srgb, ${accent} 13%, transparent)`,
        border: `1px solid color-mix(in srgb, ${accent} 26%, transparent)`,
        borderRadius: Math.round(size * 0.28),
        display: "inline-grid",
        placeItems: "center",
        color: accent,
        flexShrink: 0,
      }}
    >
      <svg width={size - pad * 2} height={size - pad * 2} viewBox="0 0 24 24" aria-hidden>
        {glyph}
      </svg>
    </span>
  );
}
