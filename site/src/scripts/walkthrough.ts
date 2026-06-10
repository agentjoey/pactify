// Walkthrough auto-cycler — advances the active step every 2.8s and moves the
// ledger highlight to the paired row(s). Step→row pairing:
//   step1→row1, step2→row2, step3→row3, step4→rows 4 then 5 (sub-sequence).
// The role color is NOT duplicated here: each .step already carries its color via
// inline style, so we read getComputedStyle(step).color and apply it as --rc on
// the highlighted row. Design tokens stay the single source of truth.

const CYCLE_MS = 2800; // dwell per step
const SUB_MS = 800; // step-4: delay before highlight moves row4 -> row5

// Rows highlighted per step (1-indexed step -> 1-indexed row(s)).
const PAIRS: Record<number, number[]> = { 1: [1], 2: [2], 3: [3], 4: [4, 5] };

export function initWalkthrough(root: HTMLElement): void {
  const steps = Array.from(root.querySelectorAll<HTMLElement>('.step[data-step]'));
  const rows = Array.from(root.querySelectorAll<HTMLElement>('.row[data-row]'));
  if (steps.length < 4 || rows.length < 5) return;

  // Reduced-motion: leave the static markup as-is (step 1 on, row 1 highlighted).
  if (matchMedia('(prefers-reduced-motion: reduce)').matches) return;

  const stepEl = (n: number) => steps.find((s) => s.dataset.step === String(n));
  const rowEl = (n: number) => rows.find((r) => r.dataset.row === String(n));

  function clearAll(): void {
    for (const s of steps) s.classList.remove('on');
    for (const r of rows) {
      r.classList.remove('hl');
      r.style.removeProperty('--rc');
    }
  }

  function highlightRow(n: number, color: string): void {
    const r = rowEl(n);
    if (!r) return;
    r.classList.add('hl');
    r.style.setProperty('--rc', color);
  }

  // setInterval handle + the nested step-4 sub-step timeout. Both are cleared on
  // pause so we never leak overlapping highlights.
  let interval: ReturnType<typeof setInterval> | null = null;
  let subTimer: ReturnType<typeof setTimeout> | null = null;
  let active = 1; // start state matches the static markup (step 1)

  function render(step: number): void {
    if (subTimer) { clearTimeout(subTimer); subTimer = null; }
    clearAll();

    const s = stepEl(step);
    if (!s) return;
    s.classList.add('on');
    // Read the step's own color (set via inline style) — no mapping duplicated.
    const color = getComputedStyle(s).color;

    const [first, second] = PAIRS[step];
    highlightRow(first, color);
    if (second !== undefined) {
      // Step 4: hold row4, then slide the highlight to row5 within the same dwell.
      subTimer = setTimeout(() => {
        const r4 = rowEl(first);
        if (r4) { r4.classList.remove('hl'); r4.style.removeProperty('--rc'); }
        highlightRow(second, color);
      }, SUB_MS);
    }
  }

  function tick(): void {
    active = (active % 4) + 1; // 1->2->3->4->1
    render(active);
  }

  function start(): void {
    if (interval) return;
    interval = setInterval(tick, CYCLE_MS);
  }

  function stop(): void {
    if (interval) { clearInterval(interval); interval = null; }
    if (subTimer) { clearTimeout(subTimer); subTimer = null; }
  }

  // Pause on hover / keyboard focus so a reader can dwell; resume on leave.
  root.addEventListener('mouseenter', stop);
  root.addEventListener('mouseleave', start);
  root.addEventListener('focusin', stop);
  root.addEventListener('focusout', start);

  // Static page: this widget lives for the lifetime of the document, so we never
  // unmount it and intentionally register no teardown (per S2 review precedent).
  start();
}
