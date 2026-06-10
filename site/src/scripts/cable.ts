// Hero "cable" animation — left smoothstep converge + right tight braid + flow.
// Ported faithfully from docs/superpowers/mockups/site-v2-hero-cable.html.
// Colors are read from CSS role tokens at runtime so the design tokens stay the
// single source of truth.

const NS = 'http://www.w3.org/2000/svg';
const MX = 400; // merge point x
const CY = 150; // merge point y / braid axis
const START_Y = [55, 150, 245];

function roleColors(): [string, string, string] {
  const cs = getComputedStyle(document.documentElement);
  const get = (name: string, fallback: string) =>
    (cs.getPropertyValue(name).trim() || fallback);
  return [
    get('--role-product', '#ffd479'),
    get('--role-design', '#8ab4ff'),
    get('--role-dev', '#6ee7a0'),
  ];
}

function mkPath(svg: SVGSVGElement, d: string, color: string, width: string): SVGPathElement {
  const p = document.createElementNS(NS, 'path');
  p.setAttribute('d', d);
  p.setAttribute('fill', 'none');
  p.setAttribute('stroke', color);
  p.setAttribute('stroke-width', width);
  p.setAttribute('stroke-linecap', 'round');
  svg.appendChild(p);
  return p;
}

function leftPathData(i: number): string {
  let d = '';
  for (let x = -10; x <= MX; x += 8) {
    const t01 = (x + 10) / (MX + 10);
    const ease = t01 * t01 * (3 - 2 * t01); // smoothstep
    const y = START_Y[i] + (CY - START_Y[i]) * ease;
    d += (x === -10 ? 'M' : 'L') + x + ' ' + y.toFixed(1);
  }
  return d;
}

function rightPathData(i: number): string {
  const phase = (i * Math.PI * 2) / 3;
  let d = '';
  for (let x = MX; x <= 812; x += 4) {
    const t01 = Math.min(1, (x - MX) / 50); // wrap-up taper: 0 -> full twist in 50px
    const amp = 9 * t01;
    const y = CY + amp * Math.sin((x - MX) / 12.5 + phase);
    d += (x === MX ? 'M' : 'L') + x + ' ' + y.toFixed(1);
  }
  return d;
}

export function initCable(svg: SVGSVGElement): void {
  const colors = roleColors();
  const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;

  // TOP guard: build everything fully drawn, no dash tricks, no animations.
  if (reduce) {
    colors.forEach((c, i) => {
      const p = mkPath(svg, leftPathData(i), c, '1.6');
      p.style.opacity = '.75';
    });
    colors.forEach((c, i) => {
      const p = mkPath(svg, rightPathData(i), c, '1.8');
      p.style.opacity = '.9';
    });
    return;
  }

  // LEFT: three loose converging curves — draw in, then gentle dash flow.
  colors.forEach((c, i) => {
    const p = mkPath(svg, leftPathData(i), c, '1.6');
    const len = p.getTotalLength();
    p.style.strokeDasharray = String(len);
    p.style.strokeDashoffset = String(len);
    p.animate(
      [{ strokeDashoffset: len }, { strokeDashoffset: 0 }],
      { duration: 1800, delay: 200 + i * 150, fill: 'forwards', easing: 'ease-out' },
    );
    setTimeout(() => {
      p.style.strokeDasharray = '12 10';
      p.style.opacity = '.75';
      p.animate(
        [{ strokeDashoffset: 0 }, { strokeDashoffset: -110 }],
        { duration: 5000, iterations: Infinity, easing: 'linear' },
      );
    }, 2300 + i * 150);
  });

  // RIGHT: tight braid — stripped-cable strands twisting around the axis.
  colors.forEach((c, i) => {
    const p = mkPath(svg, rightPathData(i), c, '1.8');
    p.style.opacity = '0';
    p.animate([{ opacity: 0 }, { opacity: 0.9 }], { duration: 600, delay: 2000, fill: 'forwards' });
    const initial = (19.6 * i * 2) / 3;
    p.style.strokeDasharray = '19.6 19.6'; // half the 39.3px wavelength -> over/under rhythm
    p.style.strokeDashoffset = String(initial);
    p.animate(
      [{ strokeDashoffset: initial }, { strokeDashoffset: initial - 78.5 }],
      { duration: 3000, iterations: Infinity, easing: 'linear', delay: 2000 },
    );
  });
}
