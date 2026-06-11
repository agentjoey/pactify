// Pure helpers for the ops view. Kept framework-free so they unit-test cleanly.

// SLUG mirrors the protocol's client/seat slug grammar: lowercase alnum start,
// then alnum or hyphen. Used to validate the seat id typed into the wire modal
// before a POST (the server re-validates authoritatively).
export const SLUG = /^[a-z0-9][a-z0-9-]*$/;

export const isSlug = (s: string): boolean => SLUG.test(s);

// relativeTime renders an ISO timestamp as a compact "Ns/Nm/Nh/Nd ago" string,
// relative to now (or an injected nowMs for deterministic tests). An empty/absent
// ts yields "never"; an unparseable ts yields the raw string so nothing is lost.
export function relativeTime(ts?: string, nowMs: number = Date.now()): string {
  if (!ts) return "never";
  const t = Date.parse(ts);
  if (Number.isNaN(t)) return ts;
  const sec = Math.max(0, Math.round((nowMs - t) / 1000));
  if (sec < 60) return `${sec}s ago`;
  const min = Math.round(sec / 60);
  if (min < 60) return `${min}m ago`;
  const hr = Math.round(min / 60);
  if (hr < 24) return `${hr}h ago`;
  const day = Math.round(hr / 24);
  return `${day}d ago`;
}
