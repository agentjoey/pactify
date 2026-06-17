import { useEffect, useState } from "react";
import { browseFs } from "../../lib/api";
import type { FsBrowseEntry } from "../../lib/types";
import { Icon } from "../../lib/icons";

// FolderPicker — directory browser for the "add project" wizard.
//
// Drives GET /api/fs/browse, renders the current path + "up" control, and lists
// sub-directories with multi-select checkboxes. Entries carrying hasPact are
// flagged as already-initialized pactify projects; isGit flags git repos. The
// selected absolute paths accumulate across navigation so the user can pick
// folders from several directories.
export function FolderPicker({
  selected,
  onChange,
}: {
  selected: string[];
  onChange: (selected: string[], entries: FsBrowseEntry[]) => void;
}) {
  const [current, setCurrent] = useState<string>("");
  const [parent, setParent] = useState<string>("");
  const [entries, setEntries] = useState<FsBrowseEntry[]>([]);
  const [knownEntries, setKnownEntries] = useState<Map<string, FsBrowseEntry>>(new Map());
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  function load(path?: string) {
    setLoading(true);
    setError("");
    browseFs(path)
      .then((r) => {
        setCurrent(r.path);
        setParent(r.parent);
        setEntries(r.entries);
        setKnownEntries((prev) => {
          const next = new Map(prev);
          for (const e of r.entries) next.set(e.path, e);
          return next;
        });
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setLoading(false));
  }

  useEffect(() => {
    load();
  }, []);

  function toggle(entry: FsBrowseEntry) {
    const nextSelected = selected.includes(entry.path)
      ? selected.filter((p) => p !== entry.path)
      : [...selected, entry.path];
    const nextEntries = nextSelected.map((p) => knownEntries.get(p)).filter(Boolean) as FsBrowseEntry[];
    onChange(nextSelected, nextEntries);
    setKnownEntries((prev) => new Map(prev).set(entry.path, entry));
  }

  return (
    <div className="flex flex-col gap-2" data-testid="folder-picker">
      <div className="flex items-center gap-2">
        <button
          type="button"
          data-testid="folder-picker-up"
          disabled={!parent || parent === current}
          onClick={() => load(parent)}
          className="inline-flex items-center gap-1 rounded-md border border-[var(--color-border-subtle)] px-2 py-1 text-[11px] text-[var(--color-text-2)] transition-colors hover:border-[var(--color-border-strong)] hover:text-[var(--color-text-1)] disabled:opacity-40"
        >
          <Icon name="chevron-up" size={12} /> Up
        </button>
        <span
          data-testid="folder-picker-path"
          className="flex-1 truncate rounded-md bg-[var(--color-bg-inset)] px-2 py-1 text-[11px] text-[var(--color-text-2)] mono"
          title={current}
        >
          {current || "home"}
        </span>
        <span className="text-[10px] text-[var(--color-text-3)]">
          {selected.length} selected
        </span>
      </div>

      {error && (
        <div className="rounded-md border border-[var(--color-danger)]/30 bg-[var(--color-danger)]/10 px-2 py-1 text-[11px] text-[var(--color-danger)]">
          {error}
        </div>
      )}

      <div
        data-testid="folder-picker-list"
        className="max-h-[240px] min-h-[120px] overflow-y-auto rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)]"
      >
        {loading && entries.length === 0 && (
          <div className="flex h-full items-center justify-center py-8 text-[11px] text-[var(--color-text-3)]">
            Loading…
          </div>
        )}

        {!loading && entries.length === 0 && (
          <div className="flex h-full items-center justify-center py-8 text-[11px] text-[var(--color-text-3)]">
            No folders here
          </div>
        )}

        {entries.map((entry) => {
          const checked = selected.includes(entry.path);
          return (
            <div
              key={entry.path}
              data-testid={`folder-entry-${entry.name}`}
              className="group flex items-center gap-2 border-b border-[var(--color-border-subtle)] px-2 py-1.5 last:border-b-0 hover:bg-[var(--color-bg-inset)]"
            >
              <input
                type="checkbox"
                data-testid={`folder-entry-${entry.name}-checkbox`}
                checked={checked}
                onChange={() => toggle(entry)}
                aria-label={`select ${entry.name}`}
                className="h-3.5 w-3.5 accent-[var(--color-role-design)]"
              />
              <button
                type="button"
                data-testid={`folder-entry-${entry.name}-navigate`}
                onClick={() => load(entry.path)}
                className="flex flex-1 items-center gap-2 text-left"
              >
                <Icon name="folder" size={14} color="var(--color-text-3)" />
                <span className="flex-1 truncate text-[12px] text-[var(--color-text-1)]">
                  {entry.name}
                </span>
              </button>
              <div className="flex items-center gap-1">
                {entry.isGit && (
                  <span className="rounded px-1 py-0.5 text-[9px] font-medium uppercase tracking-wide text-[var(--color-text-3)] bg-[var(--color-bg-inset)]">
                    git
                  </span>
                )}
                {entry.hasPact && (
                  <span
                    data-testid={`folder-entry-${entry.name}-pact`}
                    className="rounded px-1 py-0.5 text-[9px] font-medium uppercase tracking-wide"
                    style={{
                      color: "var(--color-role-design-ink)",
                      background: "color-mix(in srgb, var(--color-role-design) 14%, transparent)",
                    }}
                  >
                    pactify project
                  </span>
                )}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
