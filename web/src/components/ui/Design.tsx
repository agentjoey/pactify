import { useEffect, useState, type ReactNode } from "react";
import { setTheme, type Theme } from "../../theme";

export function SectionLabel({ children }: { children: ReactNode }) {
  return <span className="section-label">{children}</span>;
}

export function StatusPill({ status, running = false }: {
  status: string;
  running?: boolean;
}) {
  const tone = running ? "info"
    : /fail|quota|cancel/.test(status) ? "err"
    : /ready|pending|warn/.test(status) ? "warn"
    : /succeed|applied|shipped/.test(status) ? "ok" : "muted";
  return (
    <span className={`status-pill tone-${tone}`}>
      <span className={`status-dot ${running ? "breathing" : ""}`} aria-hidden="true" />
      {status}
    </span>
  );
}

export function Notice({ kind, children }: {
  kind: "warn" | "ok";
  children: ReactNode;
}) {
  return (
    <div className={`notice tone-${kind}`} role={kind === "warn" ? "alert" : "status"}>
      <span aria-hidden="true">{kind === "warn" ? "⚠" : "✓"}</span>
      <span className="notice-content">{children}</span>
    </div>
  );
}

export function Segmented<T extends string>({ label, value, options, onChange }: {
  label: string;
  value: T;
  options: readonly (readonly [T, string])[];
  onChange: (value: T) => void;
}) {
  return (
    <div className="segmented" role="group" aria-label={label}>
      {options.map(([key, title]) => (
        <button type="button" key={key} aria-pressed={key === value} onClick={() => onChange(key)}>
          {title}
        </button>
      ))}
    </div>
  );
}

const currentTheme = (): Theme => document.documentElement.dataset.theme === "light" ? "light" : "dark";
export function ThemeControl({ expanded = false }: { expanded?: boolean }) {
  const [theme, update] = useState<Theme>(currentTheme);
  useEffect(() => {
    const changed = () => update(currentTheme());
    window.addEventListener("pactify-theme-change", changed);
    return () => window.removeEventListener("pactify-theme-change", changed);
  }, []);
  const nextTheme = theme === "dark" ? "light" : "dark";
  return expanded ? (
    <Segmented label="Theme" value={theme} options={[["dark", "Dark"], ["light", "Light"]]} onChange={setTheme} />
  ) : (
    <button className="icon-control" type="button" aria-label={`Switch to ${nextTheme} theme`} onClick={() => setTheme(nextTheme)}>
      ◐
    </button>
  );
}

export function LiveIndicator({ error, updated, now }: {
  error: string;
  updated: Date | null;
  now: number;
}) {
  return (
    <span className={`live-indicator ${!error && updated ? "is-live" : ""}`} role="status">
      <span className="status-dot" aria-hidden="true" />
      {error ? "Update unavailable" : updated
        ? <>live <span>· {Math.max(0, Math.floor((now - +updated) / 1000))}s ago</span></>
        : "Updating…"}
    </span>
  );
}
