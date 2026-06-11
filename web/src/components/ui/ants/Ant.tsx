// Ant colony avatar — one stamp-style ant per caste, rotated 45° clockwise.
//
// SVG paths are lifted verbatim from the approved mockup boards:
//   - full variants (size > 18) ← board2-avatars-v6-ants.html
//   - simplified bold-stroke variants (size ≤ 18) ← board3-kanban.html (builder,
//     guard) + the same simplification rule applied to the other six (stroke
//     1.6/2, legs dropped, head/thorax/abdomen + abdomen bands + caste prop kept)
//
// Body is dark charcoal #2A2C3A with white-alpha outline; the caste color shows
// ONLY on the two abdomen bands and the caste prop. See spec §2 (LOCKED).

import type { JSX } from "react";
import type { Caste } from "../../../lib/ants";

// Canonical render sizes used across the dashboard (kanban chip / seat / pad /
// caste card). Any px works — `size` is a number — but these are the four the
// design uses; ≤ 18 triggers the simplified glyph.
export type AntSize = 14 | 22 | 34 | 72;

// Below (and including) this px size we swap to the bold-stroke simplified glyph.
const SMALL_MAX = 18;

// ── Full variants (board2) ───────────────────────────────────────────────────
const FULL: Record<Caste, JSX.Element> = {
  queen: (
    <g transform="rotate(45 24 24)" strokeLinecap="round">
      <path d="M19 5.5 L21.5 9 L24 5 L26.5 9 L29 5.5 L28 10.5 L20 10.5 Z" fill="#ECC678" />
      <path d="M20.5 12 C18 10, 17 8.5, 16.5 7 M27.5 12 C30 10, 31 8.5, 31.5 7" stroke="rgba(255,255,255,.35)" strokeWidth="1.2" fill="none" />
      <circle cx="24" cy="15.5" r="5" fill="#2A2C3A" stroke="rgba(255,255,255,.32)" strokeWidth="1.2" />
      <ellipse cx="13.5" cy="25.5" rx="8" ry="3.6" fill="rgba(236,198,120,.16)" stroke="rgba(236,198,120,.42)" strokeWidth=".8" transform="rotate(-24 13.5 25.5)" />
      <ellipse cx="34.5" cy="25.5" rx="8" ry="3.6" fill="rgba(236,198,120,.16)" stroke="rgba(236,198,120,.42)" strokeWidth=".8" transform="rotate(24 34.5 25.5)" />
      <ellipse cx="24" cy="24.5" rx="4" ry="5" fill="#2A2C3A" stroke="rgba(255,255,255,.32)" strokeWidth="1.2" />
      <ellipse cx="24" cy="37" rx="6.6" ry="8" fill="#2A2C3A" stroke="rgba(255,255,255,.32)" strokeWidth="1.2" />
      <path d="M18.4 34 C20 35, 28 35, 29.6 34 M18 38.5 C20 39.5, 28 39.5, 30 38.5" stroke="#ECC678" strokeWidth="1.6" fill="none" />
    </g>
  ),
  guard: (
    <g transform="rotate(45 24 24)" strokeLinecap="round">
      <path d="M18.5 11 C16 8.5, 15.5 7, 15.5 5.5 M29.5 11 C32 8.5, 32.5 7, 32.5 5.5" stroke="rgba(255,255,255,.35)" strokeWidth="1.2" fill="none" />
      <circle cx="24" cy="16.5" r="7" fill="#2A2C3A" stroke="rgba(255,255,255,.32)" strokeWidth="1.2" />
      <path d="M17.5 20 C14.5 22, 13.5 24.5, 14 27 M30.5 20 C33.5 22, 34.5 24.5, 34 27" stroke="rgba(255,255,255,.42)" strokeWidth="1.6" fill="none" />
      <rect x="17.5" y="14.1" width="13" height="3.6" rx="1.8" fill="#93B4F2" opacity=".92" />
      <ellipse cx="24" cy="27.5" rx="3.6" ry="4.4" fill="#2A2C3A" stroke="rgba(255,255,255,.32)" strokeWidth="1.2" />
      <ellipse cx="24" cy="38.5" rx="5.6" ry="6.6" fill="#2A2C3A" stroke="rgba(255,255,255,.32)" strokeWidth="1.2" />
      <path d="M19.2 36.5 C21 37.5, 27 37.5, 28.8 36.5 M19.6 40.5 C21.5 41.3, 26.5 41.3, 28.4 40.5" stroke="#93B4F2" strokeWidth="1.6" fill="none" />
    </g>
  ),
  builder: (
    <g transform="rotate(45 24 24)" strokeLinecap="round">
      <rect x="18" y="3" width="12" height="9" rx="2" fill="rgba(123,216,160,.22)" stroke="#7BD8A0" strokeWidth="1.3" />
      <circle cx="24" cy="17.5" r="5" fill="#2A2C3A" stroke="rgba(255,255,255,.32)" strokeWidth="1.2" />
      <path d="M20.8 13.8 L19.2 11.6 M27.2 13.8 L28.8 11.6" stroke="rgba(255,255,255,.4)" strokeWidth="1.3" />
      <ellipse cx="24" cy="26" rx="3.8" ry="4.6" fill="#2A2C3A" stroke="rgba(255,255,255,.32)" strokeWidth="1.2" />
      <path d="M20.4 23.5 C16 22, 14 23.5, 12.5 26 M27.6 23.5 C32 22, 34 23.5, 35.5 26 M20.6 27.5 C17 28.5, 15.5 30.5, 15 33 M27.4 27.5 C31 28.5, 32.5 30.5, 33 33" stroke="rgba(255,255,255,.35)" strokeWidth="1.2" fill="none" />
      <ellipse cx="24" cy="37.5" rx="5.8" ry="7" fill="#2A2C3A" stroke="rgba(255,255,255,.32)" strokeWidth="1.2" />
      <path d="M18.8 35.5 C20.5 36.5, 27.5 36.5, 29.2 35.5 M19.2 39.8 C21 40.6, 27 40.6, 28.8 39.8" stroke="#7BD8A0" strokeWidth="1.6" fill="none" />
    </g>
  ),
  catcher: (
    <g transform="rotate(45 24 24)" strokeLinecap="round">
      <circle cx="31" cy="7.5" r="4.6" fill="rgba(95,200,195,.12)" stroke="#5FC8C3" strokeWidth="1.6" />
      <path d="M27.8 10.8 L26 13" stroke="#5FC8C3" strokeWidth="1.8" />
      <ellipse cx="31" cy="7.5" rx="2" ry="1.5" fill="#D96B66" stroke="#15120E" strokeWidth=".8" />
      <path d="M29.6 6.2 L28.8 5.2 M32.4 6.2 L33.2 5.2" stroke="rgba(255,255,255,.5)" strokeWidth=".9" />
      <path d="M20.8 13.5 L19.2 11.2" stroke="rgba(255,255,255,.4)" strokeWidth="1.3" />
      <circle cx="24" cy="17.5" r="5" fill="#2A2C3A" stroke="rgba(255,255,255,.32)" strokeWidth="1.2" />
      <ellipse cx="24" cy="26" rx="3.8" ry="4.6" fill="#2A2C3A" stroke="rgba(255,255,255,.32)" strokeWidth="1.2" />
      <path d="M20.4 23.5 C16 22, 14 23.5, 12.5 26 M20.6 27.5 C17 28.5, 15.5 30.5, 15 33 M27.4 27.5 C31 28.5, 32.5 30.5, 33 33" stroke="rgba(255,255,255,.35)" strokeWidth="1.2" fill="none" />
      <ellipse cx="24" cy="37.5" rx="5.8" ry="7" fill="#2A2C3A" stroke="rgba(255,255,255,.32)" strokeWidth="1.2" />
      <path d="M18.8 35.5 C20.5 36.5, 27.5 36.5, 29.2 35.5 M19.2 39.8 C21 40.6, 27 40.6, 28.8 39.8" stroke="#5FC8C3" strokeWidth="1.6" fill="none" />
    </g>
  ),
  brewer: (
    <g transform="rotate(45 24 24)" strokeLinecap="round">
      <rect x="17.5" y="2.5" width="13" height="10" rx="1.6" fill="#FFF4DC" stroke="#15120E" strokeWidth="1" />
      <path d="M20 5.5 L23.5 5.5 M20 8 L27.5 8 M20 10.2 L26 10.2" stroke="#C9A86C" strokeWidth="1.1" />
      <path d="M25.2 4.6 L26.2 5.8 L28 3.8" stroke="#E8A85C" strokeWidth="1.2" fill="none" />
      <circle cx="24" cy="17.5" r="5" fill="#2A2C3A" stroke="rgba(255,255,255,.32)" strokeWidth="1.2" />
      <path d="M20.8 13.8 L19.2 11.6 M27.2 13.8 L28.8 11.6" stroke="rgba(255,255,255,.4)" strokeWidth="1.3" />
      <ellipse cx="24" cy="26" rx="3.8" ry="4.6" fill="#2A2C3A" stroke="rgba(255,255,255,.32)" strokeWidth="1.2" />
      <path d="M20.4 23.5 C16 22, 14 23.5, 12.5 26 M27.6 23.5 C32 22, 34 23.5, 35.5 26 M20.6 27.5 C17 28.5, 15.5 30.5, 15 33 M27.4 27.5 C31 28.5, 32.5 30.5, 33 33" stroke="rgba(255,255,255,.35)" strokeWidth="1.2" fill="none" />
      <ellipse cx="24" cy="37.5" rx="5.8" ry="7" fill="#2A2C3A" stroke="rgba(255,255,255,.32)" strokeWidth="1.2" />
      <path d="M18.8 35.5 C20.5 36.5, 27.5 36.5, 29.2 35.5 M19.2 39.8 C21 40.6, 27 40.6, 28.8 39.8" stroke="#E8A85C" strokeWidth="1.6" fill="none" />
    </g>
  ),
  painter: (
    <g transform="rotate(45 24 24)" strokeLinecap="round">
      <path d="M16.5 8.5 C16.5 4.5, 21 2, 25.5 3.5 C30 5, 31 9.5, 28 12 C26.5 13.2, 24.8 12.6, 24.3 11.2 C23.8 9.8, 22.3 9.4, 21 9.8 C19.5 10.3, 16.5 10.5, 16.5 8.5 Z" fill="#FFF4DC" stroke="#15120E" strokeWidth="1" />
      <circle cx="21" cy="6" r="1.2" fill="#ECC678" />
      <circle cx="25" cy="6.2" r="1.2" fill="#93B4F2" />
      <circle cx="27.4" cy="8.6" r="1.2" fill="#7BD8A0" />
      <circle cx="24" cy="17.5" r="5" fill="#2A2C3A" stroke="rgba(255,255,255,.32)" strokeWidth="1.2" />
      <path d="M20.8 13.8 L19.2 11.6 M27.2 13.8 L28.8 11.6" stroke="rgba(255,255,255,.4)" strokeWidth="1.3" />
      <ellipse cx="24" cy="26" rx="3.8" ry="4.6" fill="#2A2C3A" stroke="rgba(255,255,255,.32)" strokeWidth="1.2" />
      <path d="M20.4 23.5 C16 22, 14 23.5, 12.5 26 M27.6 23.5 C32 22, 34 23.5, 35.5 26 M20.6 27.5 C17 28.5, 15.5 30.5, 15 33 M27.4 27.5 C31 28.5, 32.5 30.5, 33 33" stroke="rgba(255,255,255,.35)" strokeWidth="1.2" fill="none" />
      <ellipse cx="24" cy="37.5" rx="5.8" ry="7" fill="#2A2C3A" stroke="rgba(255,255,255,.32)" strokeWidth="1.2" />
      <path d="M18.8 35.5 C20.5 36.5, 27.5 36.5, 29.2 35.5 M19.2 39.8 C21 40.6, 27 40.6, 28.8 39.8" stroke="#A89BF0" strokeWidth="1.6" fill="none" />
    </g>
  ),
  scout: (
    <g transform="rotate(45 24 24)" strokeLinecap="round">
      <path d="M19.5 12.5 C16.5 9.5, 15.8 7, 16 4.5 M28.5 12.5 C31.5 9.5, 32.2 7, 32 4.5" stroke="#C99BD8" strokeWidth="1.4" fill="none" />
      <circle cx="16" cy="3.8" r="1.4" fill="#C99BD8" />
      <circle cx="32" cy="3.8" r="1.4" fill="#C99BD8" />
      <path d="M20 6.5 C22.5 5, 25.5 5, 28 6.5 M18.6 9.3 C21.8 7.4, 26.2 7.4, 29.4 9.3" stroke="rgba(201,155,216,.5)" strokeWidth="1" fill="none" />
      <circle cx="24" cy="17.5" r="5" fill="#2A2C3A" stroke="rgba(255,255,255,.32)" strokeWidth="1.2" />
      <circle cx="22" cy="17" r="1.9" fill="none" stroke="rgba(255,255,255,.55)" strokeWidth="1" />
      <circle cx="26.6" cy="17" r="1.9" fill="none" stroke="rgba(255,255,255,.55)" strokeWidth="1" />
      <path d="M23.9 17 L24.7 17" stroke="rgba(255,255,255,.55)" strokeWidth="1" />
      <ellipse cx="24" cy="26" rx="3.8" ry="4.6" fill="#2A2C3A" stroke="rgba(255,255,255,.32)" strokeWidth="1.2" />
      <path d="M20.4 23.5 C16 22, 14 23.5, 12.5 26 M27.6 23.5 C32 22, 34 23.5, 35.5 26 M20.6 27.5 C17 28.5, 15.5 30.5, 15 33 M27.4 27.5 C31 28.5, 32.5 30.5, 33 33" stroke="rgba(255,255,255,.35)" strokeWidth="1.2" fill="none" />
      <ellipse cx="24" cy="37.5" rx="5.8" ry="7" fill="#2A2C3A" stroke="rgba(255,255,255,.32)" strokeWidth="1.2" />
      <path d="M18.8 35.5 C20.5 36.5, 27.5 36.5, 29.2 35.5 M19.2 39.8 C21 40.6, 27 40.6, 28.8 39.8" stroke="#C99BD8" strokeWidth="1.6" fill="none" />
    </g>
  ),
  keeper: (
    <g transform="rotate(45 24 24)" strokeLinecap="round">
      <g>
        <circle cx="24" cy="7" r="4" fill="rgba(238,154,107,.15)" stroke="#EE9A6B" strokeWidth="1.5" />
        <circle cx="24" cy="7" r="1.3" fill="#EE9A6B" />
        <path d="M24 1.8 L24 0.6 M24 12.2 L24 13.4 M18.8 7 L17.6 7 M29.2 7 L30.4 7 M20.3 3.3 L19.4 2.4 M27.7 10.7 L28.6 11.6 M20.3 10.7 L19.4 11.6 M27.7 3.3 L28.6 2.4" stroke="#EE9A6B" strokeWidth="1.4" />
      </g>
      <circle cx="24" cy="17.5" r="5" fill="#2A2C3A" stroke="rgba(255,255,255,.32)" strokeWidth="1.2" />
      <path d="M20.8 13.8 L19.2 11.6 M27.2 13.8 L28.8 11.6" stroke="rgba(255,255,255,.4)" strokeWidth="1.3" />
      <ellipse cx="24" cy="26" rx="3.8" ry="4.6" fill="#2A2C3A" stroke="rgba(255,255,255,.32)" strokeWidth="1.2" />
      <path d="M20.4 23.5 C16 22, 14 23.5, 12.5 26 M27.6 23.5 C32 22, 34 23.5, 35.5 26 M20.6 27.5 C17 28.5, 15.5 30.5, 15 33 M27.4 27.5 C31 28.5, 32.5 30.5, 33 33" stroke="rgba(255,255,255,.35)" strokeWidth="1.2" fill="none" />
      <ellipse cx="24" cy="37.5" rx="5.8" ry="7" fill="#2A2C3A" stroke="rgba(255,255,255,.32)" strokeWidth="1.2" />
      <path d="M18.8 35.5 C20.5 36.5, 27.5 36.5, 29.2 35.5 M19.2 39.8 C21 40.6, 27 40.6, 28.8 39.8" stroke="#EE9A6B" strokeWidth="1.6" fill="none" />
    </g>
  ),
};

// ── Simplified variants (board3, size ≤ 18) ──────────────────────────────────
// builder + guard lifted verbatim from board3-kanban.html (14px aava SVGs).
// The other six follow the same rule: bold strokes (1.6/2), legs dropped, the
// three body segments + abdomen band + caste prop retained.
const SMALL: Record<Caste, JSX.Element> = {
  queen: (
    <g transform="rotate(45 24 24)">
      <path d="M19 5.5 L21.5 9 L24 5 L26.5 9 L29 5.5 L28 10.5 L20 10.5 Z" fill="#ECC678" />
      <circle cx="24" cy="15.5" r="5.5" fill="#2A2C3A" stroke="rgba(255,255,255,.4)" strokeWidth="1.6" />
      <ellipse cx="24" cy="25" rx="4.4" ry="5.4" fill="#2A2C3A" stroke="rgba(255,255,255,.4)" strokeWidth="1.6" />
      <ellipse cx="24" cy="37.5" rx="6.6" ry="7.8" fill="#2A2C3A" stroke="rgba(255,255,255,.4)" strokeWidth="1.6" />
      <path d="M18.6 35.5 C20.5 36.5, 27.5 36.5, 29.4 35.5" stroke="#ECC678" strokeWidth="2" fill="none" />
    </g>
  ),
  guard: (
    <g transform="rotate(45 24 24)">
      <circle cx="24" cy="16.5" r="7.5" fill="#2A2C3A" stroke="rgba(255,255,255,.4)" strokeWidth="1.6" />
      <rect x="17" y="13.8" width="14" height="4.2" rx="2.1" fill="#93B4F2" />
      <ellipse cx="24" cy="28" rx="4" ry="4.8" fill="#2A2C3A" stroke="rgba(255,255,255,.4)" strokeWidth="1.6" />
      <ellipse cx="24" cy="39" rx="6" ry="7" fill="#2A2C3A" stroke="rgba(255,255,255,.4)" strokeWidth="1.6" />
      <path d="M19.2 37 C21 38, 27 38, 28.8 37" stroke="#93B4F2" strokeWidth="2" fill="none" />
    </g>
  ),
  builder: (
    <g transform="rotate(45 24 24)">
      <rect x="18" y="3" width="12" height="9" rx="2" fill="rgba(123,216,160,.25)" stroke="#7BD8A0" strokeWidth="1.6" />
      <circle cx="24" cy="17.5" r="5.5" fill="#2A2C3A" stroke="rgba(255,255,255,.4)" strokeWidth="1.6" />
      <ellipse cx="24" cy="27" rx="4.2" ry="5" fill="#2A2C3A" stroke="rgba(255,255,255,.4)" strokeWidth="1.6" />
      <ellipse cx="24" cy="38.5" rx="6.4" ry="7.5" fill="#2A2C3A" stroke="rgba(255,255,255,.4)" strokeWidth="1.6" />
      <path d="M18.8 36.5 C20.5 37.5, 27.5 37.5, 29.2 36.5" stroke="#7BD8A0" strokeWidth="2" fill="none" />
    </g>
  ),
  catcher: (
    <g transform="rotate(45 24 24)">
      <circle cx="31" cy="7.5" r="4.6" fill="rgba(95,200,195,.14)" stroke="#5FC8C3" strokeWidth="2" />
      <ellipse cx="31" cy="7.5" rx="2" ry="1.5" fill="#D96B66" />
      <circle cx="24" cy="17.5" r="5.5" fill="#2A2C3A" stroke="rgba(255,255,255,.4)" strokeWidth="1.6" />
      <ellipse cx="24" cy="27" rx="4.2" ry="5" fill="#2A2C3A" stroke="rgba(255,255,255,.4)" strokeWidth="1.6" />
      <ellipse cx="24" cy="38.5" rx="6.4" ry="7.5" fill="#2A2C3A" stroke="rgba(255,255,255,.4)" strokeWidth="1.6" />
      <path d="M18.8 36.5 C20.5 37.5, 27.5 37.5, 29.2 36.5" stroke="#5FC8C3" strokeWidth="2" fill="none" />
    </g>
  ),
  brewer: (
    <g transform="rotate(45 24 24)">
      <rect x="17.5" y="2.5" width="13" height="10" rx="1.6" fill="#FFF4DC" stroke="#15120E" strokeWidth="1.2" />
      <path d="M20 6 L27 6 M20 9 L26 9" stroke="#C9A86C" strokeWidth="1.4" />
      <circle cx="24" cy="17.5" r="5.5" fill="#2A2C3A" stroke="rgba(255,255,255,.4)" strokeWidth="1.6" />
      <ellipse cx="24" cy="27" rx="4.2" ry="5" fill="#2A2C3A" stroke="rgba(255,255,255,.4)" strokeWidth="1.6" />
      <ellipse cx="24" cy="38.5" rx="6.4" ry="7.5" fill="#2A2C3A" stroke="rgba(255,255,255,.4)" strokeWidth="1.6" />
      <path d="M18.8 36.5 C20.5 37.5, 27.5 37.5, 29.2 36.5" stroke="#E8A85C" strokeWidth="2" fill="none" />
    </g>
  ),
  painter: (
    <g transform="rotate(45 24 24)">
      <path d="M16.5 8.5 C16.5 4.5, 21 2, 25.5 3.5 C30 5, 31 9.5, 28 12 C26.5 13.2, 24.8 12.6, 24.3 11.2 C23.8 9.8, 22.3 9.4, 21 9.8 C19.5 10.3, 16.5 10.5, 16.5 8.5 Z" fill="#FFF4DC" stroke="#15120E" strokeWidth="1.2" />
      <circle cx="21" cy="6" r="1.3" fill="#ECC678" />
      <circle cx="25.2" cy="6.4" r="1.3" fill="#93B4F2" />
      <circle cx="27.4" cy="8.8" r="1.3" fill="#7BD8A0" />
      <circle cx="24" cy="17.5" r="5.5" fill="#2A2C3A" stroke="rgba(255,255,255,.4)" strokeWidth="1.6" />
      <ellipse cx="24" cy="27" rx="4.2" ry="5" fill="#2A2C3A" stroke="rgba(255,255,255,.4)" strokeWidth="1.6" />
      <ellipse cx="24" cy="38.5" rx="6.4" ry="7.5" fill="#2A2C3A" stroke="rgba(255,255,255,.4)" strokeWidth="1.6" />
      <path d="M18.8 36.5 C20.5 37.5, 27.5 37.5, 29.2 36.5" stroke="#A89BF0" strokeWidth="2" fill="none" />
    </g>
  ),
  scout: (
    <g transform="rotate(45 24 24)">
      <path d="M19.5 12.5 C16.5 9.5, 15.8 7, 16 4.5 M28.5 12.5 C31.5 9.5, 32.2 7, 32 4.5" stroke="#C99BD8" strokeWidth="2" fill="none" strokeLinecap="round" />
      <circle cx="16" cy="3.8" r="1.6" fill="#C99BD8" />
      <circle cx="32" cy="3.8" r="1.6" fill="#C99BD8" />
      <circle cx="24" cy="17.5" r="5.5" fill="#2A2C3A" stroke="rgba(255,255,255,.4)" strokeWidth="1.6" />
      <ellipse cx="24" cy="27" rx="4.2" ry="5" fill="#2A2C3A" stroke="rgba(255,255,255,.4)" strokeWidth="1.6" />
      <ellipse cx="24" cy="38.5" rx="6.4" ry="7.5" fill="#2A2C3A" stroke="rgba(255,255,255,.4)" strokeWidth="1.6" />
      <path d="M18.8 36.5 C20.5 37.5, 27.5 37.5, 29.2 36.5" stroke="#C99BD8" strokeWidth="2" fill="none" />
    </g>
  ),
  keeper: (
    <g transform="rotate(45 24 24)">
      <circle cx="24" cy="7" r="4" fill="rgba(238,154,107,.18)" stroke="#EE9A6B" strokeWidth="2" />
      <circle cx="24" cy="7" r="1.4" fill="#EE9A6B" />
      <circle cx="24" cy="17.5" r="5.5" fill="#2A2C3A" stroke="rgba(255,255,255,.4)" strokeWidth="1.6" />
      <ellipse cx="24" cy="27" rx="4.2" ry="5" fill="#2A2C3A" stroke="rgba(255,255,255,.4)" strokeWidth="1.6" />
      <ellipse cx="24" cy="38.5" rx="6.4" ry="7.5" fill="#2A2C3A" stroke="rgba(255,255,255,.4)" strokeWidth="1.6" />
      <path d="M18.8 36.5 C20.5 37.5, 27.5 37.5, 29.2 36.5" stroke="#EE9A6B" strokeWidth="2" fill="none" />
    </g>
  ),
};

export interface AntProps {
  caste: Caste;
  // Pixel size; canonical values are AntSize (14|22|34|72). ≤ 18 → simplified.
  size: number;
  // When given, the ant is an accessible image with this label; otherwise it is
  // decorative (aria-hidden) — pads/cards already carry the seat's name in text.
  title?: string;
}

// Ant renders a single caste glyph. At size ≤ 18px it uses the bold-stroke
// simplified variant so the silhouette survives at thumbnail scale.
export function Ant({ caste, size, title }: AntProps) {
  const glyph = size <= SMALL_MAX ? SMALL[caste] : FULL[caste];
  const labelled = title != null;
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 48 48"
      style={{ display: "block" }}
      role={labelled ? "img" : undefined}
      aria-hidden={labelled ? undefined : true}
    >
      {labelled ? <title>{title}</title> : null}
      {glyph}
    </svg>
  );
}
