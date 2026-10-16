import { useEffect, useRef, type ReactNode } from "react";
import type { Job } from "./Jobs";
import { jobsLink } from "./data";
import { mergeJobChains } from "./board";

// Keep this mounted while
// switching runs; only the detail's data/stream lifetime changes with the id.
export function JobDrawer({
  id,
  jobs,
  children,
}: {
  id: string;
  jobs: Job[];
  children: ReactNode;
}) {
  const panel = useRef<HTMLElement>(null);
  const opener = useRef<HTMLElement | null>(null);
  const latestId = useRef(id);
  const rows = useRef(jobs);
  latestId.current = id;
  rows.current = jobs;
  useEffect(() => {
    const scrollLocks = [document.documentElement, document.body].map(
      (element) => {
        // Preserve explicit longhands and !important as well as the shorthand.
        const saved = Array.from(element.style)
          .filter((name) =>
            ["overflow", "overflow-x", "overflow-y"].includes(name),
          )
          .map((name) => ({
            name,
            value: element.style.getPropertyValue(name),
            priority: element.style.getPropertyPriority(name),
          }));
        element.style.setProperty("overflow", "hidden", "important");
        return () => {
          element.style.removeProperty("overflow");
          for (const { name, value, priority } of saved)
            element.style.setProperty(name, value, priority);
        };
      },
    );
    opener.current = document.activeElement as HTMLElement | null;
    panel.current?.focus();
    function keydown(event: KeyboardEvent) {
      if (
        (event.target as HTMLElement).closest?.('[role="dialog"]') &&
        (event.target as HTMLElement).closest('[role="dialog"]') !==
          panel.current
      )
        return;
      if (event.key === "Escape") {
        event.stopPropagation();
        location.hash = jobsLink();
      }
      if (event.key === "Tab") {
        const elements = [
          ...panel.current!.querySelectorAll<HTMLElement>(
            'a[href], button:not(:disabled), summary, select, textarea, input, [tabindex="0"]',
          ),
        ].filter(
          (element) => element.tabIndex !== -1 && !element.closest("[inert]"),
        );
        const first = elements[0],
          last = elements[elements.length - 1];
        if (
          event.shiftKey &&
          (document.activeElement === first ||
            document.activeElement === panel.current)
        ) {
          event.preventDefault();
          last?.focus();
        } else if (
          !event.shiftKey &&
          (document.activeElement === last ||
            document.activeElement === panel.current)
        ) {
          event.preventDefault();
          first?.focus();
        }
      }
    }
    window.addEventListener("keydown", keydown, true);
    return () => {
      window.removeEventListener("keydown", keydown, true);
      for (const restore of scrollLocks) restore();
      const chain = mergeJobChains(rows.current).find((chain) =>
        chain.runs.some((job) => job.id === latestId.current),
      );
      const link = [
        ...document.querySelectorAll<HTMLAnchorElement>(".panel-job h3 a"),
      ].find(
        (link) =>
          link.hash.split("?")[0] ===
          `#/jobs/${encodeURIComponent(chain?.latest.id || latestId.current)}`,
      );
      if (opener.current?.isConnected && opener.current !== document.body)
        opener.current.focus();
      else link?.focus();
    };
  }, []);
  return (
    <>
      <div
        className="jobs-scrim fixed inset-0 z-40 bg-black/55"
        data-testid="job-drawer-scrim"
        onClick={() => {
          location.hash = jobsLink();
        }}
      />
      <aside
        ref={panel}
        tabIndex={-1}
        role="dialog"
        aria-modal="true"
        aria-label="Job details"
        className="jobs-drawer fixed z-50 flex flex-col overflow-y-auto rounded-2xl border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] shadow-[var(--shadow-raised)]"
      >
        <a
          href={jobsLink()}
          className="jobs-drawer-close"
          aria-label="Close job details"
        >
          × Close
        </a>
        {children}
      </aside>
    </>
  );
}
