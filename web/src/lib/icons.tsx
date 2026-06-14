// Pactify icon library — a curated mapping of Pactify concepts onto a clean,
// consistent line-icon set (lucide-react). One <Icon name="…"/> everywhere keeps
// role/kind/state/action/view glyphs uniform across cards, nodes, the top bar,
// and the gallery. Add a concept here, not a raw lucide import in a component.
import {
  Waypoints,
  Eye,
  Hammer,
  Sparkles,
  SquareTerminal,
  Gem,
  Braces,
  MousePointer2,
  Bot,
  FileText,
  Loader,
  Hourglass,
  CircleCheck,
  Ship,
  TriangleAlert,
  Moon,
  RotateCcw,
  Plus,
  GitMerge,
  Send,
  Play,
  SquareKanban,
  Network,
  SlidersHorizontal,
  Radio,
  ListChecks,
  Wand2,
  BookOpen,
  type LucideIcon,
} from "lucide-react";

export const ICONS: Record<string, LucideIcon> = {
  // roles
  "role-orchestrator": Waypoints,
  "role-reviewer": Eye,
  "role-worker": Hammer,
  // agent kinds
  "kind-claude-code": Sparkles,
  "kind-opencode": SquareTerminal,
  "kind-gemini-cli": Gem,
  "kind-codex-cli": Braces,
  "kind-cursor-cli": MousePointer2,
  "kind-agent": Bot,
  // task
  "node-task": FileText,
  // pact states
  "state-in_progress": Loader,
  "state-awaiting_review": Hourglass,
  "state-changes_requested": RotateCcw,
  "state-accepted": CircleCheck,
  "state-shipped": Ship,
  "state-escalated": TriangleAlert,
  "state-idle": Moon,
  // actions
  "action-connect": Plus,
  "action-merge": GitMerge,
  "action-dispatch": Send,
  "action-run": Play,
  // dashboard views
  "view-kanban": SquareKanban,
  "view-canvas": Network,
  "view-ops": SlidersHorizontal,
  "view-live": Radio,
  "view-plan": ListChecks,
  "view-setup": Wand2,
  "view-recipes": BookOpen,
};

export type IconName = keyof typeof ICONS;

export function Icon({
  name,
  size = 16,
  className,
  strokeWidth = 2,
}: {
  name: string;
  size?: number;
  className?: string;
  strokeWidth?: number;
}) {
  const C = ICONS[name];
  if (!C) return null;
  return <C size={size} className={className} strokeWidth={strokeWidth} aria-hidden />;
}
