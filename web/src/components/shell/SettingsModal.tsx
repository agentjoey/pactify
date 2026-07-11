import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { useDataSource } from "../../lib/datasource";
import { AgentRoster } from "../ops/AgentRoster";
import { CustomAgentForm } from "../ops/CustomAgentForm";
import { AgentConfig } from "../ops/AgentConfig";
import { Wiring } from "../ops/Wiring";
import { Seats } from "../ops/Seats";
import { Machines } from "../Machines";
import { AccountPanel } from "../AccountPanel";
import {
  Users,
  Plugs,
  ShieldCheck,
  TreeStructure,
  SquaresFour,
  SlidersHorizontal,
  ClockCounterClockwise,
  UserCircle,
  Desktop,
  Palette,
  Keyboard,
  MagnifyingGlass,
  X,
} from "@phosphor-icons/react";

type Scope = "project" | "machine" | "account";

const SCOPE_COLOR: Record<Scope, string> = {
  project: "#ffd479",
  machine: "#6ee7a0",
  account: "#8ab4ff",
};

type NavItem = { id: string; label: string; icon: React.ComponentType<any> };
type NavGroup = {
  scope: Scope;
  label: string;
  items: NavItem[];
};

const FOCUSABLE =
  'a[href],button:not([disabled]),textarea:not([disabled]),input:not([disabled]),select:not([disabled]),[tabindex]:not([tabindex="-1"])';

// SettingsModal — scoped two-pane sheet. Left nav groups config by scope
// (PROJECT / MACHINE / ACCOUNT); right pane shows the selected panel plus a
// scope banner that cross-links the related scope.
export function SettingsModal({
  project,
  author,
  focusSeat,
  onClose,
  onLogout,
  viewMode,
}: {
  project: string;
  author: boolean;
  focusSeat?: string | null;
  onClose: () => void;
  onLogout?: () => void;
  viewMode?: boolean;
}) {
  // Hosted (relay) mode has no local serve behind /api: the PROJECT and
  // MACHINE panels would render dead fetches, so they are hidden and the
  // modal opens on the ACCOUNT section instead.
  const src = useDataSource();
  const hosted = src.capabilities.multiMachine;
  const [activeId, setActiveId] = useState(hosted ? "account" : "agent-configs");
  const [query, setQuery] = useState("");
  const titleId = useId();
  const panelRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (focusSeat) setActiveId("seats");
  }, [focusSeat]);

  useEffect(() => {
    if (viewMode) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose();
        return;
      }
      if (e.key === "Tab") {
        const panel = panelRef.current;
        if (!panel) return;
        const items = Array.from(panel.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
          (el) =>
            el.tabIndex !== -1 &&
            !el.hasAttribute("hidden") &&
            el.getAttribute("aria-hidden") !== "true",
        );
        if (items.length === 0) {
          e.preventDefault();
          return;
        }
        const first = items[0];
        const last = items[items.length - 1];
        const active = document.activeElement as HTMLElement | null;
        if (e.shiftKey) {
          if (active === first || !panel.contains(active)) {
            e.preventDefault();
            last.focus();
          }
        } else {
          if (active === last || !panel.contains(active)) {
            e.preventDefault();
            first.focus();
          }
        }
      }
    };
    document.addEventListener("keydown", onKey, true);
    return () => document.removeEventListener("keydown", onKey, true);
  }, [onClose, viewMode]);

  useEffect(() => {
    if (viewMode) return;
    const prev = document.activeElement as HTMLElement | null;
    const panel = panelRef.current;
    if (panel) {
      const first = Array.from(panel.querySelectorAll<HTMLElement>(FOCUSABLE)).find(
        (el) => el.tabIndex !== -1,
      );
      first?.focus();
    }
    return () => prev?.focus();
  }, [viewMode]);

  const navGroups: NavGroup[] = [
    // PROJECT/MACHINE panels drive the LOCAL serve's /api; hosted mode has no
    // such backend, so only the ACCOUNT group is offered there.
    ...(hosted
      ? []
      : ([
          {
            scope: "project",
            label: `PROJECT · ${project}`,
            items: [
              { id: "seats", label: "Seats & roles", icon: Users },
              { id: "wiring", label: "Agent wiring", icon: Plugs },
              { id: "review-gate", label: "Review gate", icon: ShieldCheck },
              { id: "worktrees", label: "Worktrees", icon: TreeStructure },
            ],
          },
          {
            scope: "machine",
            label: "MACHINE · this computer",
            items: [
              { id: "registered-agents", label: "Registered agents", icon: SquaresFour },
              { id: "agent-configs", label: "Agent configs", icon: SlidersHorizontal },
              { id: "sessions", label: "Sessions", icon: ClockCounterClockwise },
            ],
          },
        ] as NavGroup[])),
    {
      scope: "account",
      label: "ACCOUNT",
      items: [
        { id: "account", label: "Account", icon: UserCircle },
        { id: "machines", label: "Machines", icon: Desktop },
        { id: "appearance", label: "Appearance", icon: Palette },
        { id: "shortcuts", label: "Shortcuts", icon: Keyboard },
      ],
    },
  ];

  const filteredGroups = navGroups
    .map((g) => ({
      ...g,
      items: g.items.filter((i) => i.label.toLowerCase().includes(query.toLowerCase())),
    }))
    .filter((g) => g.items.length > 0);

  const allItems = navGroups.flatMap((g) => g.items);
  const activeItem = allItems.find((i) => i.id === activeId) ?? allItems[0];
  const activeScope = navGroups.find((g) => g.items.some((i) => i.id === activeId))?.scope ?? "machine";

  const scopeBannerLabel =
    activeScope === "project"
      ? `PROJECT · ${project}`
      : activeScope === "machine"
        ? "MACHINE · all projects"
        : "ACCOUNT";

  const scopeRowLabel =
    activeScope === "project"
      ? `PROJECT · ${project}`
      : activeScope === "machine"
        ? "MACHINE · this computer"
        : "ACCOUNT";

  function renderPanel(): ReactNode {
    switch (activeId) {
      case "seats":
        return (
          <section data-testid="settings-project-seats">
            {focusSeat && (
              <div className="mb-2 text-xs text-[var(--color-text-2)]">
                Focused seat · <span className="text-[var(--color-text-1)]">{focusSeat}</span>
              </div>
            )}
            <Seats project={project} />
          </section>
        );
      case "wiring":
        return <Wiring project={project} author={author} />;
      case "registered-agents":
        return (
          <>
            <AgentRoster author={author} refreshKey={0} onChanged={undefined} />
            <CustomAgentForm author={author} onCreated={() => {}} />
          </>
        );
      case "agent-configs":
        return <AgentConfig />;
      case "machines":
        return <Machines />;
      case "account":
        return <AccountPanel onLogout={() => { onClose(); onLogout?.(); }} />;
      case "review-gate":
      case "worktrees":
      case "sessions":
      case "appearance":
      case "shortcuts":
        return (
          <PlaceholderPanel
            scope={activeScope}
            label={activeItem.label}
            project={project}
          />
        );
      default:
        return <AgentConfig />;
    }
  }

  const panel = (
    <div
      ref={panelRef}
      data-testid={viewMode ? "settings-view" : "settings-modal"}
      role={viewMode ? undefined : "dialog"}
      aria-modal={viewMode ? undefined : "true"}
      aria-labelledby={titleId}
      className={`flex flex-col overflow-hidden border-[var(--color-border-strong)] bg-[var(--color-bg-inset)] ${
        viewMode
          ? "flex-1 border-r"
          : "rounded-[18px] border shadow-[var(--shadow-overlay)]"
      }`}
      style={viewMode ? undefined : { width: "min(1000px, calc(100vw - 48px))", height: "min(660px, calc(100vh - 48px))" }}
      onClick={viewMode ? undefined : (e) => e.stopPropagation()}
    >
      <div className="flex min-h-0 flex-1">
        {/* Left nav rail */}
        <nav
          data-testid="settings-nav"
          className="flex w-[250px] flex-none flex-col overflow-hidden border-r border-[var(--border-2)] bg-[var(--bg-panel)]"
        >
          <div className="px-[18px] pb-[3px] pt-[17px]">
            <span id={titleId} className="text-[17px] font-bold tracking-[-0.01em] text-[var(--text)]">
              Settings
            </span>
          </div>

          <div className="px-[13px] pb-[5px] pt-[11px]">
            <div className="flex items-center gap-[7px] rounded-lg border border-[var(--border-2)] bg-[var(--bg-input)] px-[9px] py-[7px]">
              <MagnifyingGlass size={13} className="shrink-0 text-[var(--text-4)]" />
              <input
                type="text"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Search settings"
                className="min-w-0 flex-1 bg-transparent text-[12px] text-[var(--text)] placeholder:text-[var(--text-4)] outline-none"
              />
            </div>
          </div>

          <div className="flex-1 overflow-y-auto px-[11px] pb-[14px] pt-[4px]">
            {filteredGroups.map((group) => (
              <div key={group.scope} data-testid="settings-nav-group" className="mb-1">
                <div className="flex items-center gap-[6px] px-[8px] pb-[5px] pt-[13px]">
                  <span
                    className="h-[6px] w-[6px] rounded-[2px]"
                    style={{ background: SCOPE_COLOR[group.scope] }}
                  />
                  <span className="font-mono text-[9px] font-semibold uppercase tracking-[0.16em] text-[var(--text-4)]">
                    {group.label}
                  </span>
                </div>
                {group.items.map((item) => {
                  const active = item.id === activeId;
                  const Icon = item.icon;
                  return (
                    <button
                      key={item.id}
                      type="button"
                      data-testid={`nav-${item.id}`}
                      aria-current={active ? "true" : "false"}
                      onClick={() => setActiveId(item.id)}
                      className={[
                        "flex w-full items-center gap-[9px] rounded-lg border px-[9px] py-[6px] text-left transition-colors duration-[var(--motion-micro)]",
                        active
                          ? "border-[var(--accent-line)] bg-[var(--accent-2)]"
                          : "border-transparent hover:bg-[var(--bg-elev2)]",
                      ].join(" ")}
                    >
                      <Icon
                        size={15}
                        weight="light"
                        className={active ? "text-[var(--accent)]" : "text-[var(--text-4)]"}
                      />
                      <span
                        className={[
                          "text-[12.5px]",
                          active ? "font-semibold text-[var(--text)]" : "font-medium text-[var(--text-2)]",
                        ].join(" ")}
                      >
                        {item.label}
                      </span>
                    </button>
                  );
                })}
              </div>
            ))}
          </div>
        </nav>

        {/* Right content pane */}
        <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
          {/* Pane header */}
          <div className="flex shrink-0 flex-col gap-[6px] border-b border-[var(--border-2)] px-[26px] py-[22px]">
            <div className="flex items-center justify-between">
              <span className="font-mono text-[9.5px] uppercase tracking-[0.2em] text-[var(--text-4)]">
                {scopeRowLabel}
              </span>
              {!viewMode && (
                <button
                  type="button"
                  onClick={onClose}
                  aria-label="close"
                  className="ml-3 inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-lg border border-[rgba(255,255,255,0.10)] text-sm text-[var(--color-text-3)] transition-colors duration-[var(--motion-micro)] hover:text-[var(--color-text-1)]"
                >
                  <X size={14} />
                </button>
              )}
            </div>
            <div className="flex flex-wrap items-center gap-3">
              <h2 className="text-[22px] font-bold tracking-[-0.015em] text-[var(--text)]">
                {activeItem.label}
              </h2>
              <span
                data-testid="settings-scope-banner"
                className="inline-flex items-center gap-[6px] rounded-full border px-[10px] py-[3px] text-[10px] font-medium"
                style={{
                  color: SCOPE_COLOR[activeScope],
                  background: `${SCOPE_COLOR[activeScope]}1F`,
                  borderColor: `${SCOPE_COLOR[activeScope]}47`,
                }}
              >
                <span
                  className="h-[5px] w-[5px] rounded-[2px]"
                  style={{ background: SCOPE_COLOR[activeScope] }}
                />
                {scopeBannerLabel}
              </span>
            </div>
          </div>

          <div className="flex-1 overflow-y-auto px-[26px] py-5">
            <ScopeExplainer scope={activeScope} activeItem={activeItem.id} project={project} />
            <div className="mt-5">{renderPanel()}</div>
          </div>
        </div>
      </div>
    </div>
  );

  if (viewMode) {
    return (
      <div data-testid="settings-view-wrapper" className="flex min-w-0 flex-1 overflow-hidden">
        {panel}
      </div>
    );
  }

  return (
    <div
      data-testid="settings-modal-overlay"
      className="fixed inset-0 z-50 flex items-center justify-center bg-[rgba(6,9,13,0.6)] backdrop-blur-[3px]"
      onClick={onClose}
    >
      {panel}
    </div>
  );
}

function ScopeExplainer({
  scope,
  activeItem,
  project,
}: {
  scope: Scope;
  activeItem: string;
  project: string;
}) {
  if (scope === "machine" && activeItem === "agent-configs") {
    return (
      <p className="max-w-xl text-[12.5px] leading-[1.6] text-[var(--text-3)]">
        Model and permission posture per registered agent. These live in your machine registry and
        apply to every project — seat assignments are set per project under{" "}
        <span className="text-[#ffd479]">Project · Seats &amp; roles</span>.
      </p>
    );
  }
  if (scope === "project" && activeItem === "seats") {
    return (
      <p className="max-w-xl text-[12.5px] leading-[1.6] text-[var(--text-3)]">
        Seats and roles for <span className="text-[#ffd479]">{project}</span>. Agent models and
        permissions are configured machine-wide under{" "}
        <span className="text-[#6ee7a0]">Machine · Agent configs</span>.
      </p>
    );
  }
  if (scope === "project" && activeItem === "wiring") {
    return (
      <p className="max-w-xl text-[12.5px] leading-[1.6] text-[var(--text-3)]">
        Wire registered agents into <span className="text-[#ffd479]">{project}</span>. Wiring is
        project-local; agents must first be registered on this machine.
      </p>
    );
  }
  if (scope === "machine" && activeItem === "registered-agents") {
    return (
      <p className="max-w-xl text-[12.5px] leading-[1.6] text-[var(--text-3)]">
        Scan, register and remove agent kinds on this computer. Once registered, configure models
        and permissions under <span className="text-[#6ee7a0]">Machine · Agent configs</span>.
      </p>
    );
  }
  if (scope === "account" && activeItem === "machines") {
    return (
      <p className="max-w-xl text-[12.5px] leading-[1.6] text-[var(--text-3)]">
        Account-wide machine roster. In local mode this list is empty because there is no relay
        presence to aggregate.
      </p>
    );
  }
  return (
    <p className="max-w-xl text-[12.5px] leading-[1.6] text-[var(--text-3)]">
      {scope === "project"
        ? `Project-scoped setting for ${project}. These values stay inside this repo and do not leak to other projects.`
        : scope === "machine"
          ? "Machine-scoped setting. These values live in your local agent registry and apply to every project on this computer."
          : "Account-scoped setting. Changes apply to your signed-in Pactify account."}
    </p>
  );
}

function PlaceholderPanel({
  scope,
  label,
  project,
}: {
  scope: Scope;
  label: string;
  project: string;
}) {
  return (
    <div
      data-testid="settings-placeholder"
      className="flex flex-col gap-3 rounded-xl border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] p-4"
    >
      <h3 className="text-sm font-medium text-[var(--color-text-1)]">{label}</h3>
      <p className="text-xs text-[var(--color-text-2)]">
        {scope === "project"
          ? `${label} is project-scoped for ${project}. This panel is coming soon — no backend changes were made for this task.`
          : scope === "machine"
            ? `${label} is machine-scoped. This panel is coming soon — no backend changes were made for this task.`
            : `${label} is account-scoped. This panel is coming soon — no backend changes were made for this task.`}
      </p>
    </div>
  );
}
