// Pactify icon library — Phosphor duotone, role-colored per concept. One
// <Icon name="…"/> everywhere keeps the bold/rounded/two-tone glyphs and their
// semantic colors uniform across cards, nodes, the top bar, and the gallery.
// Add a concept here (icon + color), not a raw phosphor import in a component.
import {
  Compass,
  Eye,
  Hammer,
  Sparkle,
  TerminalWindow,
  Diamond,
  Code,
  Cursor,
  Robot,
  FileText,
  Spinner,
  Hourglass,
  ArrowCounterClockwise,
  CheckCircle,
  RocketLaunch,
  Warning,
  Moon,
  Plus,
  GitMerge,
  PaperPlaneRight,
  Play,
  Kanban,
  Graph,
  FadersHorizontal,
  Broadcast,
  ListChecks,
  MagicWand,
  ChatTeardropText,
  type Icon as PhIcon,
} from "@phosphor-icons/react";

type Entry = { I: PhIcon; color: string };

// color uses the -ink companion where the vivid hue is too light for a small
// glyph; otherwise the semantic token.
const MAP: Record<string, Entry> = {
  // roles
  "role-orchestrator": { I: Compass, color: "var(--color-role-product-ink)" },
  "role-reviewer": { I: Eye, color: "var(--color-role-design-ink)" },
  "role-worker": { I: Hammer, color: "var(--color-role-dev-ink)" },
  // agent kinds (each a distinct hue)
  "kind-claude-code": { I: Sparkle, color: "var(--color-role-design-ink)" },
  "kind-opencode": { I: TerminalWindow, color: "var(--color-role-dev-ink)" },
  "kind-gemini-cli": { I: Diamond, color: "var(--color-role-research)" },
  "kind-codex-cli": { I: Code, color: "var(--color-role-ops)" },
  "kind-cursor-cli": { I: Cursor, color: "var(--color-role-qa)" },
  "kind-agent": { I: Robot, color: "var(--color-text-2)" },
  // task
  "node-task": { I: FileText, color: "var(--color-text-1)" },
  // pact states
  "state-in_progress": { I: Spinner, color: "var(--color-role-design-ink)" },
  "state-awaiting_review": { I: Hourglass, color: "var(--color-warn)" },
  "state-changes_requested": { I: ArrowCounterClockwise, color: "var(--color-role-ops)" },
  "state-accepted": { I: CheckCircle, color: "var(--color-success)" },
  "state-shipped": { I: RocketLaunch, color: "var(--color-role-dev-ink)" },
  "state-escalated": { I: Warning, color: "var(--color-danger)" },
  "state-idle": { I: Moon, color: "var(--color-text-3)" },
  // actions
  "action-connect": { I: Plus, color: "var(--color-role-design-ink)" },
  "action-merge": { I: GitMerge, color: "var(--color-role-design-ink)" },
  "action-dispatch": { I: PaperPlaneRight, color: "var(--color-role-design-ink)" },
  "action-run": { I: Play, color: "var(--color-success)" },
  // dashboard views
  "view-kanban": { I: Kanban, color: "var(--color-text-2)" },
  "view-canvas": { I: Graph, color: "var(--color-text-2)" },
  "view-ops": { I: FadersHorizontal, color: "var(--color-text-2)" },
  "view-live": { I: Broadcast, color: "var(--color-text-2)" },
  "view-plan": { I: ListChecks, color: "var(--color-text-2)" },
  "view-setup": { I: MagicWand, color: "var(--color-text-2)" },
  "view-recipes": { I: ChatTeardropText, color: "var(--color-text-2)" },
};

export type IconName = keyof typeof MAP;
export const ICON_NAMES = Object.keys(MAP);

export function Icon({
  name,
  size = 16,
  className,
  color,
  weight = "duotone",
}: {
  name: string;
  size?: number;
  className?: string;
  color?: string;
  weight?: "thin" | "light" | "regular" | "bold" | "fill" | "duotone";
}) {
  const e = MAP[name];
  if (!e) return null;
  const C = e.I;
  return <C size={size} weight={weight} color={color ?? e.color} className={className} aria-hidden />;
}
