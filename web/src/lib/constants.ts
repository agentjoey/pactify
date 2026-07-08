// Shared frontend constants — values are intentionally frozen so tests and
// consumers see the same figures without redefining them.

/** Stale threshold: a task sitting in_progress longer than this gets an amber dot. */
export const STALE_MS = 30 * 60 * 1000;

/** Retained-events cap: the SSE stream is trimmed to the most recent events. */
export const EVENTS_CAP = 2000;

/** Consecutive mid-session state-fetch failures before the stale indicator shows. */
export const FETCH_FAIL_THRESHOLD = 3;

/** Cockpit status poll fallback when the SSE stream is quiet. */
export const COCKPIT_STATUS_POLL_MS = 5000;
