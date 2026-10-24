import { useId } from "react";

// The official pactify mark (Pactify-Original-Refined symbol): three cables that interlace
// once and converge. Masks cut the transparent seams where one cable passes over another.
const PRODUCT = "M4 6 C31 6 42 14 60 22 C69 26 75 27.5 84 27.5";
const DESIGN = "M4 22 L84 22";
const DEV = "M4 38 C31 38 42 30 60 22 C69 18 75 16.5 84 16.5";

export function CableMark() {
  const id = useId().replace(/:/g, "");
  const cut = (paths: string[]) => (
    <>
      <rect x="-8" y="-8" width="104" height="60" fill="#fff" />
      {paths.map((d) => <path key={d} d={d} fill="none" stroke="#000" strokeWidth="6.2" strokeLinecap="round" />)}
    </>
  );
  return (
    <svg width="27" height="12" viewBox="0 2.4 88 39.2" aria-hidden="true">
      <defs>
        <mask id={`${id}-a`} maskUnits="userSpaceOnUse" x="-8" y="-8" width="104" height="60">{cut([PRODUCT, DEV])}</mask>
        <mask id={`${id}-b`} maskUnits="userSpaceOnUse" x="-8" y="-8" width="104" height="60">{cut([PRODUCT])}</mask>
      </defs>
      <path d={DESIGN} fill="none" stroke="#8ab4ff" strokeWidth="4.8" strokeLinecap="round" mask={`url(#${id}-a)`} />
      <path d={DEV} fill="none" stroke="#6ee7a0" strokeWidth="4.8" strokeLinecap="round" mask={`url(#${id}-b)`} />
      <path d={PRODUCT} fill="none" stroke="#ffd479" strokeWidth="4.8" strokeLinecap="round" />
    </svg>
  );
}
