// Ant colony avatar system — caste mapping + per-seat pad gradients.
//
// Eight castes mirror the delivery chain. The protocol-hard roles
// (orchestrator/reviewer/worker) map first by a fixed priority; free-form
// roster roles then match by case-insensitive substring; anything unmatched
// falls back to the worker caste (builder). See spec §2.

export type Caste =
  | "queen"
  | "guard"
  | "builder"
  | "catcher"
  | "brewer"
  | "painter"
  | "scout"
  | "keeper";

// Keyword → caste, checked in order against each role string (substring,
// case-insensitive). The first role that matches any keyword wins.
const KEYWORD_CASTES: ReadonlyArray<readonly [string, Caste]> = [
  ["qa", "catcher"],
  ["test", "catcher"],
  ["product", "brewer"],
  ["pm", "brewer"],
  ["design", "painter"],
  ["research", "scout"],
  // "devops" needs no entry: any string containing it already matches "ops".
  ["ops", "keeper"],
  ["operation", "keeper"],
];

// casteForRoles resolves a seat's roles to its colony caste.
//
// Protocol-hard roles take precedence over any free-form keyword match — a
// seat tagged ["worker","qa"] is a builder, not a catcher. Among protocol-hard
// roles the priority is orchestrator > reviewer > worker.
export function casteForRoles(roles: string[]): Caste {
  if (roles.includes("orchestrator")) return "queen";
  if (roles.includes("reviewer")) return "guard";
  if (roles.includes("worker")) return "builder";

  for (const role of roles) {
    const lower = role.toLowerCase();
    for (const [keyword, caste] of KEYWORD_CASTES) {
      if (lower.includes(keyword)) return caste;
    }
  }
  return "builder";
}

// Per-caste base pad gradients (lifted from board2-avatars-v6-ants.html `.pad`
// background values). These are the locked hue families; seat individuality is
// layered on top as a small lightness jitter.
const CASTE_PADS: Record<Caste, { from: string; to: string }> = {
  queen: { from: "#3D3520", to: "#221E14" },
  guard: { from: "#232C44", to: "#171B28" },
  builder: { from: "#1F3329", to: "#16231C" },
  catcher: { from: "#1E3A3C", to: "#142122" },
  brewer: { from: "#3F311E", to: "#211B12" },
  painter: { from: "#2E2B49", to: "#1A1828" },
  scout: { from: "#372B42", to: "#1E1825" },
  keeper: { from: "#402E1E", to: "#211912" },
};

// Deterministic 32-bit string hash (FNV-1a). Stable across calls/processes.
function hash(s: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return h >>> 0;
}

function clamp8(n: number): number {
  return Math.max(0, Math.min(255, Math.round(n)));
}

// Scale a hex color's lightness by `factor`, preserving hue (per-channel
// multiply keeps the channel ratios — i.e. the hue family — intact).
function scaleLightness(hex: string, factor: number): string {
  const r = parseInt(hex.slice(1, 3), 16);
  const g = parseInt(hex.slice(3, 5), 16);
  const b = parseInt(hex.slice(5, 7), 16);
  const to2 = (n: number) => clamp8(n * factor).toString(16).padStart(2, "0");
  return `#${to2(r)}${to2(g)}${to2(b)}`;
}

// padGradient gives a seat its own pad while staying within its caste's hue
// family. The seat id hashes to a lightness factor in roughly ±6%, so two
// same-caste seats read differently; stable for a given (seatId, caste).
export function padGradient(seatId: string, caste: Caste): { from: string; to: string } {
  const base = CASTE_PADS[caste];
  // map hash → [-1, 1] → ±6% lightness
  const jitter = ((hash(seatId) % 1000) / 999) * 2 - 1; // [-1, 1]
  const factor = 1 + jitter * 0.06;
  return {
    from: scaleLightness(base.from, factor),
    to: scaleLightness(base.to, factor),
  };
}
