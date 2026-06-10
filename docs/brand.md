# Pactify Brand — Color & Role System

> Status: v1 · 2026-06-10 · applies to pactify.dev, the serve dashboard, and the Squad canvas.

## One palette, two semantic layers

The three accent colors carry meaning on two layers that never conflict: the **brand
layer** names the three personas of a dev squad; the **protocol layer** colors terminal
output and log semantics. Both layers resolve to the same three hex values.

| Color | Hex | Brand layer (personas) | Protocol layer (output) |
|---|---|---|---|
| Yellow | `#ffd479` | **Product** — assigns, accepts, owns the spec | seat/identity, §-clause numbering |
| Blue | `#8ab4ff` | **Design** — reviews structure, owns the blueprint | ✓ confirmations, verified states |
| Green | `#6ee7a0` | **Dev** — builds, checkpoints, ships | `$` actions, execution, "tests green" |

Mapping rationale: Product = spotlight/decision yellow (and the contract clauses it
owns); Design = blueprint blue (and the structural ✓); Dev = terminal green (and the
`$` prompt it lives in).

## Tokens

Defined in `site/src/styles/global.css` (and mirrored wherever UI ships):

```css
--role-product: #ffd479;   /* alias of --yellow */
--role-design:  #8ab4ff;   /* alias of --blue   */
--role-dev:     #6ee7a0;   /* alias of --green  */
```

Always reference the `--role-*` alias when the meaning is a persona, and the base color
token when the meaning is protocol output. Same value — the name documents intent.

## Usage rules

1. **Seats/personas are always role-colored**: seat cards, persona chips, canvas node
   borders, event-stream actor dots.
2. **Terminal output keeps protocol semantics**: `$` green, `✓` blue, seat names yellow —
   even inside marketing visuals.
3. **Products are NOT role-colored.** Base/Squad/Team use neutral styling with status
   badges (green=available, yellow=in development, dim=planned). Roles ≠ products.
4. **The cable motif** (hero): three role-colored strands converge loose from the left
   and twist into one tight braid to the right — independent agents entering one pact.
   This is the brand's signature visual; reuse the motif, never recolor it.
5. Dark base (`#0c1117` family) + system monospace everywhere. No webfonts.

## Protocol-role correspondence (for reference, not enforcement)

pact's first-class protocol roles are orchestrator/worker/reviewer. Personas are how
humans staff seats: Product typically holds orchestrator+reviewer duties, Dev holds
worker, Design holds reviewer/structural duties. The protocol does not know personas —
the brand layer is presentation only.
