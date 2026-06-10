# pactify.dev v2 — Design

- **Date:** 2026-06-10 · **Status:** approved (4 mockup rounds on the visual companion)
- **Depends on:** site v1 live (M2.4); `docs/brand.md` (color/role system, same commit)
- **References researched:** Supabase (products grid), Neon (long-form skeleton, MCP-clients marquee), pi.dev (philosophy section, install tabs), OpenRouter (feature cards, honest stats)

## Goal

Grow the v1 single-page landing into a full marketing site: 11 sections, brand
color-role system throughout, the "cable" hero motif, and slots that absorb Phase 3
visuals without restructuring. Still 100% static Astro, still no client framework —
animations are vanilla JS + CSS, all gated on `prefers-reduced-motion`.

## Decisions locked (mockup-validated)

1. Structure A (full long-form), 11 sections, order below.
2. Brand: 3 colors ↔ 3 personas per `docs/brand.md`; persona names on seats
   (product agent / design agent / dev agent A / dev agent B / researcher) — never
   vendor names in seat contexts.
3. Hero motif "stripped cable": LEFT = three loose role-colored curves converging
   (smoothstep, draw-in then slow dash-flow); RIGHT of the merge point = tight braid
   (amp 9px tapering up over 50px, wavelength ≈78px, phases 0/120/240°, over/under
   dash rhythm flowing right). JS-generated SVG paths (sampled polylines), vanilla.
4. Terminal typewriter must be REAL per-character typing with the caret following the
   currently-typing line (the v1 line-reveal is not enough) — acceptance criterion.
5. §4 agents = horizontal marquee (Neon-style): seamless loop (track duplicated),
   edge fade masks, hover pauses, card hover lifts + "setup →"; click → `/onboarding#<kind-id>`
   (the onboarding page's existing per-kind heading ids). Logos: official brand assets
   where their guidelines permit compatibility listings; monogram tiles as fallback.
6. §6 reserved visual slot: dashed frame labeled as the live-capture slot — v2 ships a
   role-colored canvas skeleton placeholder; later swapped to a serve-dashboard capture,
   then the Phase 3 canvas recording. Frame stays, content evolves.

## §-by-§ structure (final)

| § | Section | Content (validated in mockup rev3 + cable-v4) |
|---|---|---|
| 1 | Nav | wordmark · products · protocol · onboarding · docs · GitHub button |
| 2 | Hero | h1 "Agents, in agreement." + subline; cable animation behind; terminal (4 lines, persona names, real typewriter); install tabs (curl / go install / Claude plugin) with copy; honest stats strip (v1 frozen · 2 rules · 8 verbs · 5+ agents · MIT) |
| 3 | Seats are roles | h2 "Any agent can take a seat — under two rules."; 5 persona seat cards lighting in their OWN role color (yellow/blue/green/green/yellow), role dot top-right; the two §1/§2 rule cards merged into this section |
| 4 | Bring your own agents | marquee per decision 5; cards: Claude Code (one-click plugin), opencode (auto-wired), Gemini CLI (auto-wired), Codex CLI (config snippet), Claude Desktop (--project), Antigravity (--project) |
| 5 | How it works | 4 steps (Product assigns → Dev joins & builds → Checkpoint with evidence → Review→accept→merge) auto-cycling highlight; right: log.jsonl pane, active line highlighted with the acting role's color as left border; hover pauses the cycle |
| 6 | Watch your squad work | reserved slot (decision 6) |
| 7 | Why pact | 4 OpenRouter-style cards: Git-native ⎇ / Enforced not prompted § / Any agent one protocol ⌁ / Observable ◎; icon chips tinted per role color; each links to spec/onboarding/dashboard docs |
| 8 | The ladder | 3 product cards: Base (badge available, green border) / Squad (badge in development, yellow) / Team (badge planned, dim); neutral card styling per brand rule 3 |
| 9 | What we didn't build | strikethrough philosophy: message bus / central server / prompt-engineered alignment → "two rules, enforced by code. Everything else is your call." |
| 10 | CTA | curl chip (copy) + Star on GitHub + plugin one-liner |
| 11 | Footer | 4 columns: Product / Resources / Community / identity (MIT · protocol v1 frozen) |

## Technical architecture

- Same Astro project (`site/`), same tokens file extended with `--role-*` aliases.
- New `site/src/scripts/` vanilla modules loaded by the landing only:
  `cable.ts` (hero SVG generator + Web Animations), `typewriter.ts` (per-char with
  caret hand-off), `walkthrough.ts` (step cycler, hover pause). Marquee is pure CSS.
  Each module no-ops under `prefers-reduced-motion: reduce` (final static frame:
  cable fully drawn, terminal fully typed, step 1 highlighted, marquee static).
- Components split: `src/components/{Hero,Seats,AgentMarquee,Walkthrough,CanvasSlot,WhyCards,Ladder,Philosophy,Cta}.astro` — index.astro composes them (v1's single file is past its extraction point).
- `/protocol` + `/onboarding` pages unchanged (Doc layout); onboarding gains stable
  per-kind anchor ids if RenderDoc's generated headings don't already provide them
  (verify; adjust the doc generator only if needed — separate commit in the main repo).
- check-dist assertions updated for v2 markup (curl CTA, rules text, persona seat names,
  marquee kinds present). a11y: decorative SVGs aria-hidden; marquee links are real
  `<a>`; typewriter text present in DOM before animation (no JS-only content).
- OG/meta per page (existing); og:image remains deferred.

## Out of scope (unchanged deferrals)

og:image · /protocol TOC · i18n · analytics · webfonts · site job promotion to required.
