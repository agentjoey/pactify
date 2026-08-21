import { Badge } from "./Badge";

// TierBadge — the execution-tier badge shared by DispatchPanel / PlanDock /
// RunRail (exec-tiering-ui). Design constraints (rev 3, do not relax):
//
// - All four tiers use the SAME muted color ("text-2") and are distinguished
//   by TEXT only. Tier magnitude is information, not health — warn/danger are
//   health tokens in this repo ("Not detected", "gate failed"), so a correct,
//   healthy L3 must never be painted red.
// - warn is reserved for the one real anomaly: TierMissing → NO TIER (the
//   planner skipped the mandatory tier line).
// - Because color carries no signal, the badge's x position must be FIXED by
//   the caller (a `w-[34px] shrink-0` slot as the row's first element) — that
//   fixed column is what makes `L1 L1 L3 L1` scannable instead of line-by-line.
export const TIER_TITLE: Record<string, string> = {
  L0: "L0 — 便宜档：小改动、低风险",
  L1: "L1 — 默认档",
  L2: "L2 — 复杂档：多文件 / 跨模块",
  L3: "L3 — 高风险档：架构、迁移、不可逆操作",
};

// Visible legend for the plan-review list: badge `title`s are mouse-only, so
// keyboard/touch users need the meanings spelled out on screen.
export const TIER_LEGEND = "L0 便宜 · L1 默认 · L2 复杂 · L3 高风险";

const NO_TIER_NOTE = "未标注 tier —— 引擎将按 L1 运行";

export function TierBadge({
  tier,
  missing,
  conflict,
}: {
  tier?: string;
  // The spec has no tier line at all (planner missed the mandatory tier) →
  // NO TIER in warn with an accessible name (Badge adds role="img"; a bare
  // span's aria-label is name-prohibited in ARIA 1.2).
  missing?: boolean;
  // manifest↔spec disagreement note from the backend; when present it becomes
  // the badge title (it already names both values and which one wins).
  conflict?: string;
}) {
  if (missing) {
    return (
      // whitespace-nowrap: the slot is sized for L0..L3; NO TIER is wider and
      // its row's slot grows (see TierSlot) rather than wrapping mid-word.
      <Badge color="warn" className="whitespace-nowrap" title={NO_TIER_NOTE} ariaLabel={NO_TIER_NOTE} data-testid="tier-badge">
        NO TIER
      </Badge>
    );
  }
  if (!tier) return null;
  return (
    <Badge color="text-2" className="whitespace-nowrap" title={conflict || TIER_TITLE[tier] || `${tier} 执行档位`} data-testid="tier-badge">
      {tier}
    </Badge>
  );
}

// TierSlot — the badge's fixed-width column slot for plan-review rows. The
// four tiers get the spec-pinned `w-[34px] shrink-0` slot so the L-column sits
// at one x position regardless of id length (that fixed column is what makes
// the list scannable). NO TIER is wider than 34px; its row's slot grows
// (w-auto) so the badge never overlaps the id — the L-column is unaffected
// because anomaly rows carry no L badge.
export function TierSlot({
  tier,
  missing,
  conflict,
}: {
  tier?: string;
  missing?: boolean;
  conflict?: string;
}) {
  return (
    <span className={`${missing ? "w-auto" : "w-[34px]"} shrink-0`}>
      <TierBadge tier={tier} missing={missing} conflict={conflict} />
    </span>
  );
}
