// replayUrl — pure helpers for the `?at=N` replay deep link (spec §6 item 6).
//
// These operate on the location SEARCH string only (e.g. "?view=canvas&at=3");
// the component layer stays URL-agnostic. App reads ?at on mount to enter replay
// at N, writes it via history.replaceState while scrubbing, and clears it on LIVE.

// readAt parses the `at` param to a non-negative integer, or null when absent or
// malformed (empty / non-numeric / negative / non-integer). Accepts a search
// string with or without the leading "?".
export function readAt(search: string): number | null {
  const params = new URLSearchParams(search.startsWith("?") ? search.slice(1) : search);
  const raw = params.get("at");
  if (raw === null || raw === "") return null;
  // Reject anything that is not a run of digits (URLSearchParams would happily
  // parseInt "2.5" or "3x"); also reject negatives implicitly (no leading -).
  if (!/^\d+$/.test(raw)) return null;
  return Number(raw);
}

// writeAt returns a new search string with `at` set to the given value, or with
// `at` removed when value is null. All other params are preserved in order; an
// existing `at` is updated in place. Returns "" (never a bare "?") when the
// result is empty.
export function writeAt(currentSearch: string, at: number | null): string {
  const params = new URLSearchParams(
    currentSearch.startsWith("?") ? currentSearch.slice(1) : currentSearch,
  );
  if (at === null) {
    params.delete("at");
  } else {
    params.set("at", String(at));
  }
  const out = params.toString();
  return out ? `?${out}` : "";
}
