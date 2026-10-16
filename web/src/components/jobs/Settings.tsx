import { useCallback, useEffect, useState } from "react";
import { Button } from "../ui/Button";
import type { JobsSource } from "./source";
import { responseError } from "./data";
import { Routing } from "./Routing";
interface Default {
  model?: string;
  effort?: string;
}
interface Harness {
  name: string;
  installed: boolean;
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
  const [harnesses, setHarnesses] = useState<Harness[]>([]);
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [defaults, setDefaults] = useState<Record<string, Default>>({});
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [conflict, setConflict] = useState(false);
  const [errors, setErrors] = useState<FieldError[]>([]);
  const [busy, setBusy] = useState(false);
  const writable = !!source.write && !readOnly;
  const harnessesPath =
    source.harnessesPath || source.jobsPath.replace(/\/jobs$/, "/harnesses");
  const load = useCallback(
    async (signal: AbortSignal) => {
      const [h, r] = await Promise.all([
        source.fetch(harnessesPath, signal).then((r) => r.json()),
        source.fetch(source.routerPath!, signal).then((r) => r.json()),
      ]);
      if (signal.aborted) return;
      if (
        !Array.isArray(h.harnesses) ||
        !r.config ||
        typeof r.version !== "string"
      )
        throw Error("Harnesses unavailable");
      setHarnesses(h.harnesses);
      setSnapshot(r);
      setDefaults(structuredClone(r.config.defaults || {}));
      setError("");
      setErrors([]);
      setConflict(false);
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
    setError("");
    try {
      const response = await source.fetch(
        harnessesPath + "?refresh=1",
        new AbortController().signal,
      );
      const next = await response.json();
      if (!Array.isArray(next.harnesses)) throw Error("Harnesses unavailable");
      setHarnesses(next.harnesses);
    } catch (e) {
      setError((e as Error).message);
    } finally {
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
    <div className="settings-content">
      {readOnly && (
        <p className="panel-caption">
          Edit settings from the dashboard or the CLI.
        </p>
      )}
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
        {notice && <p role="status">{notice}</p>}
        {conflict && (
          <p role="alert" className="panel-error">
            Config changed elsewhere — reload to see the latest
          </p>
        )}
        {writable && (conflict || error) && (
          <Button variant="ghost" disabled={busy} onClick={() => void reload()}>
            Reload defaults
          </Button>
        )}
        {!snapshot && !error && <p>Loading harnesses…</p>}
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
                    {h.models.error ? (
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
          key={snapshot?.version || "initial"}
          source={source}
          readOnly={readOnly}
          expanded
        />
      </section>
    </div>
  );
}
