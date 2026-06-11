import { relativeTime } from "./ops";

// relTime is the tight card/desk variant of ops.relativeTime: same bucketing
// (<60s / minutes / hours / days) but WITHOUT the " ago" suffix and rendering
// "now" instead of "Ns" for sub-minute ages, so it fits a card chip. Empty or
// unparseable input yields "" (the chip is simply omitted). Built on top of
// relativeTime to keep one bucketing rule — see plan T4.
export function relTime(tsISO: string, now: number = Date.now()): string {
  if (!tsISO) return "";
  const base = relativeTime(tsISO, now);
  if (base === "never" || base === tsISO) return ""; // unparseable → no chip
  if (base.endsWith("s ago")) return "now";
  return base.replace(" ago", "");
}
