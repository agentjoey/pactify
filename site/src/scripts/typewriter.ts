// Per-character typewriter that PRESERVES inline span coloring.
//
// The terminal lines contain colored child spans (e.g. `<span class="t-green">$</span>`).
// A naive typewriter that re-appends from a stored plain string would destroy the
// coloring. Instead we walk the TEXT nodes of each line, remember each node's full
// string, blank them out, then re-fill character-by-character in document order so
// every character re-appears inside its original colored element.

interface TwOpts {
  cps?: number;
}

interface LineState {
  el: HTMLElement;
  // Ordered text nodes plus their target strings.
  nodes: { node: Text; full: string }[];
}

function caretFor(container: HTMLElement): HTMLElement {
  let caret = container.querySelector<HTMLElement>('.caret, .hero-cur');
  if (!caret) {
    caret = document.createElement('span');
    caret.className = 'caret';
    container.appendChild(caret);
  }
  caret.classList.add('caret');
  caret.classList.remove('hero-cur');
  caret.setAttribute('aria-hidden', 'true');
  return caret;
}

export function initTypewriter(container: HTMLElement, opts: TwOpts = {}): void {
  const cps = opts.cps ?? 30;
  const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;

  // Decorative repetition of the page's headline — don't announce keystrokes.
  container.setAttribute('aria-live', 'off');

  const caret = caretFor(container);

  // Reduced-motion: leave the pre-rendered full text untouched, caret static-visible.
  if (reduce) {
    caret.style.visibility = 'visible';
    return;
  }

  const lines = Array.from(container.querySelectorAll<HTMLElement>('.tw-line'));
  if (lines.length === 0) return;

  // Snapshot each line's text nodes, then blank them. Done in the same frame as
  // init so any flash-of-full-text is minimised.
  const states: LineState[] = lines.map((el) => {
    const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
    const nodes: { node: Text; full: string }[] = [];
    let n: Node | null = walker.nextNode();
    while (n) {
      const t = n as Text;
      nodes.push({ node: t, full: t.data });
      n = walker.nextNode();
    }
    return { el, nodes };
  });
  states.forEach((s) => {
    s.nodes.forEach(({ node }) => { node.data = ''; });
    s.el.style.visibility = 'visible';
  });

  const interval = 1000 / cps;
  let li = 0; // current line index
  let ni = 0; // current text-node index within the line
  let ci = 0; // current char index within the text node

  function placeCaret(line: HTMLElement) {
    line.appendChild(caret);
  }

  function typeStep() {
    if (li >= states.length) {
      // Done — leave caret blinking at the end of the last line.
      caret.style.visibility = 'visible';
      return;
    }
    const state = states[li];
    placeCaret(state.el);

    // Advance past any text nodes already fully typed.
    while (ni < state.nodes.length && ci >= state.nodes[ni].full.length) {
      ni += 1;
      ci = 0;
    }

    if (ni >= state.nodes.length) {
      // Line complete — pause, then move to next line.
      li += 1;
      ni = 0;
      ci = 0;
      setTimeout(typeStep, 250);
      return;
    }

    const target = state.nodes[ni];
    target.node.data = target.full.slice(0, ci + 1);
    ci += 1;
    setTimeout(typeStep, interval);
  }

  caret.style.visibility = 'visible';
  typeStep();
}
