import { useCallback, useEffect, useState } from "react";
import { Button } from "../ui/Button";
import type { JobsSource } from "./source";
import { CopyCommand } from "./NextSteps";
import { responseError, useResource } from "./data";
import { Routing } from "./Routing";
import { panelBuild } from "../../buildTarget";
import { ThemeControl, SectionLabel, Notice } from "../ui/Design";
import { Storage } from "./Storage";
interface DoctorSnapshot {
  lastRun: string | null;
  harnesses: { name: string; installed: boolean; version: string }[];
}
interface Default {
  model?: string;
  effort?: string;
}
interface Harness {
  name: string;
  installed: boolean;
  stale?: boolean;
  version: string;
  verifiedVersion: string;
  experimental: boolean;
  readOnly: boolean;
  models: { items: string[]; error: string };
  efforts: string[];
  default: Default;
}
interface Snapshot {
  version: string;
  config: {
    defaults?: Record<string, Default>;
    aliases: Record<string, unknown>;
    fallback: Record<string, string[]>;
    quotaGroups: Record<string, unknown>;
  };
}
interface FieldError {
  field: string;
  message: string;
}
export function Settings({
  source,
  readOnly = false,
}: {
  source: JobsSource;
  readOnly?: boolean;
}) {
  const sections = [["machine","Harnesses / Models & routing"],["storage","Storage"],["doctor","Doctor"],["appearance","Appearance"],["shortcuts","Shortcuts"]] as const;
  const [section, setSection] = useState("machine");
  const doctor = useResource<DoctorSnapshot>(
    source,
    source.jobsPath.replace(/\/jobs$/, "/doctor"),
  );
  const [harnesses, setHarnesses] = useState<Harness[]>([]);
  const [harnessLoading, setHarnessLoading] = useState(true);
  const [harnessError, setHarnessError] = useState("");
  const [routingKey, setRoutingKey] = useState(0);
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [defaults, setDefaults] = useState<Record<string, Default>>({});
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [conflict, setConflict] = useState(false);
  const [errors, setErrors] = useState<FieldError[]>([]);
  const [busy, setBusy] = useState(false);
  const writable = !panelBuild && !!source.write && !readOnly;
  const harnessesPath =
    source.harnessesPath || source.jobsPath.replace(/\/jobs$/, "/harnesses");
  const load = useCallback(
    async (signal: AbortSignal) => {
      await Promise.all([
        (async () => {
          try {
            const response = await source.fetch(harnessesPath, signal);
            if (!response.ok) throw Error(await responseError(response));
            const h = await response.json();
            if (!Array.isArray(h.harnesses)) throw Error("Harnesses unavailable");
            if (!signal.aborted) {
              setHarnesses(h.harnesses);
              setHarnessError("");
            }
          } catch (e) {
            if (!signal.aborted) setHarnessError((e as Error).message);
          } finally {
            if (!signal.aborted) setHarnessLoading(false);
          }
        })(),
        (async () => {
          try {
            const response = await source.fetch(source.routerPath!, signal);
            if (!response.ok) throw Error(await responseError(response));
            const r = await response.json();
            if (!r.config || typeof r.version !== "string")
              throw Error("Defaults unavailable");
            if (signal.aborted) return;
            setSnapshot(r);
            setDefaults(structuredClone(r.config.defaults || {}));
            setError("");
            setErrors([]);
            setConflict(false);
          } catch (e) {
            if (!signal.aborted) setError((e as Error).message);
          }
        })(),
      ]);
    },
    [source, harnessesPath],
  );
  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal).catch((e) => {
      if (!controller.signal.aborted) setError((e as Error).message);
    });
    return () => controller.abort();
  }, [load]);
  async function refreshModels() {
    setBusy(true);
    setHarnessError("");
    try {
      const response = await source.fetch(
        harnessesPath + "?refresh=1",
        new AbortController().signal,
      );
      if (!response.ok) throw Error(await responseError(response));
      const next = await response.json();
      if (!Array.isArray(next.harnesses)) throw Error("Harnesses unavailable");
      setHarnesses(next.harnesses);
    } catch (e) {
      setHarnessError((e as Error).message);
    } finally {
      setHarnessLoading(false);
      setBusy(false);
    }
  }
  async function reload() {
    setBusy(true);
    try {
      await load(new AbortController().signal);
      setNotice("");
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  async function save() {
    if (!snapshot || !source.write) return;
    setBusy(true);
    setError("");
    setErrors([]);
    setNotice("");
    try {
      const response = await source.write(source.routerPath!, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          version: snapshot.version,
          config: { ...snapshot.config, defaults },
        }),
      });
      if (response.status === 409) {
        setConflict(true);
        return;
      }
      if (response.status === 422) {
        const body = await response.json();
        setErrors(body.errors || []);
        return;
      }
      if (!response.ok) throw Error(await responseError(response));
      await load(new AbortController().signal);
      setRoutingKey((key) => key + 1);
      setNotice("Defaults saved");
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  function change(name: string, key: keyof Default, value: string) {
    setDefaults((current) => {
      const next = { ...current, [name]: { ...current[name] } };
      if (value) next[name][key] = value;
      else delete next[name][key];
      if (!Object.keys(next[name]).length) delete next[name];
      return next;
    });
    setNotice("");
  }
  return (
    <div className="settings-layout">
      <aside className="settings-nav">
        <nav aria-label="Settings sections">
          {[{title: "Machine", items: sections.slice(0, 3)}, {title: "Preferences", items: sections.slice(3)}].map(group => (
            <div className="settings-nav-group" key={group.title}>
              <SectionLabel>{group.title}</SectionLabel>
              {group.items.map(([key, title]) => (
                <button type="button" key={key} aria-current={section === key ? "page" : undefined} onClick={() => setSection(key)}>
                  {title}
                </button>
              ))}
            </div>
          ))}
        </nav>
      </aside>
      <label className="settings-select">Settings section
        <select aria-label="Settings section" value={section} onChange={event => setSection(event.target.value)}>
          {sections.map(([key, title]) => <option key={key} value={key}>{title}</option>)}
        </select>
      </label>
      <div className="settings-content">
        {section === "appearance" && (
          <section className="panel-section" aria-label="Appearance">
            <h3>Theme</h3><p>Choose a palette for this browser. Dark is the default.</p><ThemeControl expanded />
          </section>
        )}
        {section === "shortcuts" && (
          <section className="panel-section" aria-label="Shortcuts">
            <h3>Keyboard shortcuts</h3>
            <dl className="shortcut-list">
              <dt>/ or ⌘K</dt><dd>Search jobs</dd>
              <dt>j / k</dt><dd>Next / previous job</dd>
              <dt>h / l</dt><dd>Previous / next station</dd>
              <dt>Enter</dt><dd>Open details</dd>
              <dt>Escape</dt><dd>Close details</dd>
              <dt>?</dt><dd>Show shortcut help</dd>
            </dl>
          </section>
        )}
      {readOnly && (
        <p className="panel-caption">
          Edit settings with <code>pactify serve</code> (dashboard) or the CLI.
        </p>
      )}
      {section === "storage" && <Storage source={source} readOnly={readOnly} embedded />}
      {section === "doctor" && <section className="panel-section" aria-label="Doctor status">
        <h2>Doctor status</h2>
        {doctor.error ? (
          <p role="alert">{doctor.error}</p>
        ) : doctor.data ? (
          <>
            <p>Last doctor run: {doctor.data.lastRun || "unknown"}</p>
            <p className="panel-caption">Stored harness versions; no checks are run by this page.</p>
            {(doctor.data.harnesses || []).map(h => (
              <p key={h.name}>{h.name} · {h.installed ? h.version || "version unknown" : "not installed"}</p>
            ))}
          </>
        ) : <p>Loading doctor status…</p>}
        <CopyCommand command="pactify sub doctor" />
      </section>}
      <div hidden={section !== "machine"}>
      <section aria-label="Harnesses">
        <div className="section-heading">
          <h2>Harnesses</h2>
          {writable && snapshot && (
            <Button
              loading={busy}
              disabled={conflict}
              onClick={() => void save()}
            >
              Save defaults
            </Button>
          )}
        </div>
        {error && (
          <p role="alert" className="panel-error">
            {error}
          </p>
        )}
        {notice && <Notice kind="ok">{notice}</Notice>}
        {conflict && (
          <p role="alert" className="panel-error">
            Config changed elsewhere — reload to see the latest
          </p>
        )}
        {writable && (conflict || error || harnessError) && (
          <Button variant="ghost" disabled={busy} onClick={() => void reload()}>
            Reload defaults
          </Button>
        )}
        {harnessLoading && <p>Loading harnesses…</p>}
        {harnessError && (
          <p role="alert" className="panel-error">{harnessError}</p>
        )}
        <div className="harness-grid">
          {harnesses.map((h) => (
            <article
              className="panel-section harness-card"
              key={h.name}
              aria-label={h.name}
            >
              <div className="section-heading">
                <h3>{h.name}</h3>
                {h.experimental && (
                  <span className="experimental">experimental</span>
                )}
              </div>
              <p>
                {h.installed ? "Installed" : "Not installed"}
                {h.version && ` · ${h.version}`}
              </p>
              <p className="panel-caption">
                verified {h.verifiedVersion || "unknown"}
              </p>
              {h.version &&
                h.verifiedVersion &&
                h.version !== h.verifiedVersion && (
                  <p className="version-warning">
                    Installed version differs from verified version
                  </p>
                )}
              <p className="panel-caption">
                Read-only: {h.readOnly ? "Supported" : "Not supported"}
              </p>
              {writable ? (
                <fieldset disabled={busy}>
                  {h.models.error && (
                    <p role="alert" className="panel-error">
                      {h.models.error}
                    </p>
                  )}
                  <label className="routing-field">
                    Default model
                    {!(h.models.items || []).length ? (
                      <input
                        aria-label={`${h.name} Default model`}
                        placeholder="Harness default"
                        value={defaults[h.name]?.model || ""}
                        onChange={(e) =>
                          change(h.name, "model", e.target.value)
                        }
                      />
                    ) : (
                      <select
                        aria-label={`${h.name} Default model`}
                        value={defaults[h.name]?.model || ""}
                        onChange={(e) =>
                          change(h.name, "model", e.target.value)
                        }
                      >
                        <option value="">Harness default</option>
                        {[
                          ...new Set([
                            ...(h.models.items || []),
                            ...(defaults[h.name]?.model
                              ? [defaults[h.name].model!]
                              : []),
                          ]),
                        ].map((model) => (
                          <option key={model}>{model}</option>
                        ))}
                      </select>
                    )}
                  </label>
                  <label className="routing-field">
                    Effort
                    <select
                      aria-label={`${h.name} Effort`}
                      value={defaults[h.name]?.effort || ""}
                      onChange={(e) => change(h.name, "effort", e.target.value)}
                    >
                      <option value="">Harness default</option>
                      {[
                        ...new Set([
                          ...(h.efforts || []),
                          ...(defaults[h.name]?.effort
                            ? [defaults[h.name].effort!]
                            : []),
                        ]),
                      ].map((effort) => (
                        <option key={effort}>{effort}</option>
                      ))}
                    </select>
                  </label>
                  {errors
                    .filter((e) => e.field.startsWith(`defaults.${h.name}`))
                    .map((e) => (
                      <p role="alert" className="panel-error" key={e.field}>
                        {e.message}
                      </p>
                    ))}
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => void refreshModels()}
                  >
                    Refresh models
                  </Button>
                  {h.stale && <span className="panel-caption"> cached</span>}
                </fieldset>
              ) : (
                <>
                  <p>
                    Default model:{" "}
                    {defaults[h.name]?.model || "Harness default"}
                  </p>
                  <p>Effort: {defaults[h.name]?.effort || "Harness default"}</p>
                  {h.models.error && <p role="alert">{h.models.error}</p>}
                  <p className="panel-caption">
                    Models: {h.models.items?.join(", ") || "Unavailable"}
                  </p>
                </>
              )}
            </article>
          ))}
        </div>
        {errors
          .filter(
            (e) =>
              !harnesses.some((h) => e.field.startsWith(`defaults.${h.name}`)),
          )
          .map((e) => (
            <p role="alert" key={e.field}>
              {e.field}: {e.message}
            </p>
          ))}
      </section>
      <section aria-label="Routing">
        <Routing
          key={routingKey}
          source={source}
          readOnly={readOnly}
          expanded
          modelsLoading={harnessLoading}
          catalog={Object.fromEntries(
            harnesses.map((h) => [
              h.name,
              {
                models: h.models.items || [],
                efforts: h.efforts || [],
              },
            ]),
          )}
        />
      </section>
      </div>
      </div>
    </div>
  );
}
