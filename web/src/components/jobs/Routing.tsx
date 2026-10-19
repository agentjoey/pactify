import { useCallback, useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { Button } from "../ui/Button";
import type { JobsSource } from "./source";

const harnesses = ["codex", "kimi", "agy", "opencode"];
interface Route {
  harness: string;
  model?: string;
  effort?: string;
}
interface Config {
  defaults?: Record<string, { model?: string; effort?: string }>;
  aliases: Record<string, Route>;
  fallback: Record<string, string[]>;
  quotaGroups: Record<string, { harness: string; models: string[] }>;
}
interface Snapshot {
  path: string;
  exists: boolean;
  version: string;
  config: Config;
  exhaustedRoutes: {
    key: string;
    harness: string;
    model?: string;
    resetAt: string | null;
  }[];
}
interface FieldError {
  field: string;
  message: string;
}
interface Draft {
  aliases: { name: string; harness: string; model: string; effort: string }[];
  fallback: Record<string, string[]>;
  groups: { name: string; harness: string; models: string[] }[];
}
function draftOf(config: Config): Draft {
  return {
    aliases: Object.entries(config.aliases).map(([name, r]) => ({
      name,
      harness: r.harness,
      model: r.model || "",
      effort: r.effort || "",
    })),
    fallback: Object.fromEntries(
      Object.entries(config.fallback).map(([h, routes]) => [h, [...routes]]),
    ),
    groups: Object.entries(config.quotaGroups).map(([name, g]) => ({
      name,
      harness: g.harness,
      models: [...g.models],
    })),
  };
}
function resetCountdown(resetAt: string | null, now: number) {
  if (!resetAt || !Number.isFinite(Date.parse(resetAt)))
    return "resets in unknown";
  const minutes = Math.max(0, Math.ceil((Date.parse(resetAt) - now) / 60000));
  return minutes >= 60
    ? `resets in ${Math.floor(minutes / 60)}h ${minutes % 60}m`
    : `resets in ${minutes}m`;
}
function Field({
  label,
  field,
  errors,
  children,
}: {
  label: string;
  field: string;
  errors: FieldError[];
  children: ReactNode;
}) {
  const error = errors.find((e) => e.field === field);
  return (
    <label className="routing-field">
      {label}
      {children}
      {error && <span className="routing-field-error">{error.message}</span>}
    </label>
  );
}
function Harness({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <select
      aria-label={label}
      value={value}
      onChange={(e) => onChange(e.target.value)}
    >
      {harnesses.map((h) => (
        <option key={h}>{h}</option>
      ))}
    </select>
  );
}
// Models and efforts each harness reports, from /api/sub/harnesses.
export type HarnessCatalog = Record<
  string,
  { models: string[]; efforts: string[] }
>;
// A dropdown of the given choices; free text when the harness reported none.
function Choice({
  label,
  value,
  choices,
  empty,
  loading = false,
  onChange,
}: {
  label: string;
  value: string;
  choices: string[];
  empty: string;
  loading?: boolean;
  onChange: (value: string) => void;
}) {
  if (loading)
    return (
      <select aria-label={label} disabled value="">
        <option value="">Loading models…</option>
      </select>
    );
  if (choices.length === 0)
    return (
      <input
        aria-label={label}
        placeholder={empty}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
    );
  return (
    <select
      aria-label={label}
      value={value}
      onChange={(e) => onChange(e.target.value)}
    >
      <option value="">{empty}</option>
      {[...new Set([...(value ? [value] : []), ...choices])].map((c) => (
        <option key={c}>{c}</option>
      ))}
    </select>
  );
}

export function Routing({
  source,
  readOnly = false,
  expanded = false,
  catalog = {},
  modelsLoading = false,
}: {
  source: JobsSource;
  readOnly?: boolean;
  expanded?: boolean;
  catalog?: HarnessCatalog;
  modelsLoading?: boolean;
}) {
  const [open, setOpen] = useState(expanded);
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [errors, setErrors] = useState<FieldError[]>([]);
  const [message, setMessage] = useState("");
  const [conflict, setConflict] = useState(false);
  const [busy, setBusy] = useState(false);
  const [confirmKey, setConfirmKey] = useState<string | null>(null);
  const [now, setNow] = useState(Date.now);
  // Capture the base version separately from snapshots: an in-flight poll may finish after Edit.
  const editVersion = useRef<string | null>(null);
  const writable = !!source.write && !readOnly;

  const load = useCallback(
    async (signal: AbortSignal) => {
      const response = await source.fetch(source.routerPath!, signal);
      const next: Snapshot = await response.json();
      if (
        !next.config?.aliases ||
        !next.config.fallback ||
        !next.config.quotaGroups ||
        !Array.isArray(next.exhaustedRoutes)
      )
        throw new Error("Invalid routing response");
      if (!signal.aborted) setSnapshot(next);
    },
    [source],
  );
  useEffect(() => {
    if (!open || !source.routerPath) return;
    const controller = new AbortController();
    async function update() {
      if (editVersion.current !== null) return;
      try {
        await load(controller.signal);
      } catch {
        if (!controller.signal.aborted)
          setMessage("Routing unavailable. Reload to retry.");
      }
    }
    void update();
    const timer = setInterval(() => {
      setNow(Date.now());
      void update();
    }, 30000);
    return () => {
      controller.abort();
      clearInterval(timer);
    };
  }, [open, source, load]);

  function cancel() {
    editVersion.current = null;
    setDraft(null);
    setErrors([]);
    setMessage("");
    setConflict(false);
  }
  async function reload() {
    setBusy(true);
    try {
      await load(new AbortController().signal);
      cancel();
      setConfirmKey(null);
    } catch {
      setMessage("Routing unavailable. Reload to retry.");
    } finally {
      setBusy(false);
    }
  }
  async function save() {
    if (!draft || !snapshot || !source.write) return;
    setMessage("");
    setErrors([]);
    setConflict(false);
    if (
      new Set(draft.aliases.map((a) => a.name)).size !== draft.aliases.length
    ) {
      setMessage("Duplicate alias name");
      return;
    }
    if (new Set(draft.groups.map((g) => g.name)).size !== draft.groups.length) {
      setMessage("Duplicate quota group name");
      return;
    }
    const config: Config = {
      ...snapshot.config,
      aliases: Object.fromEntries(
        draft.aliases.map(({ name, ...route }) => [name, route]),
      ),
      fallback: draft.fallback,
      quotaGroups: Object.fromEntries(
        draft.groups.map((g) => [
          g.name,
          { harness: g.harness, models: g.models },
        ]),
      ),
    };
    setBusy(true);
    try {
      const response = await source.write(source.routerPath!, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ version: editVersion.current, config }),
      });
      if (response.status === 422) {
        const body = await response.json();
        setErrors(body.errors);
        return;
      }
      if (response.status === 409) {
        setConflict(true);
        return;
      }
      if (!response.ok) throw new Error(`Save failed (${response.status}).`);
      // End editing only after a successful reload supplies the next content version.
      await load(new AbortController().signal);
      cancel();
    } catch (e) {
      setMessage((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  async function clear() {
    if (confirmKey === null || !source.write) return;
    setBusy(true);
    setMessage("");
    try {
      const path =
        source.routerPath!.replace(/\/router$/, "/quota/") +
        encodeURIComponent(confirmKey);
      const response = await source.write(path, { method: "DELETE" });
      if (!response.ok) throw new Error(`Clear failed (${response.status}).`);
      setConfirmKey(null);
      await load(new AbortController().signal);
    } catch (e) {
      setMessage((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  function setRoutes(h: string, routes: string[]) {
    if (draft)
      setDraft({ ...draft, fallback: { ...draft.fallback, [h]: routes } });
  }
  function move(h: string, index: number, step: number) {
    const routes = [...draft!.fallback[h]];
    [routes[index], routes[index + step]] = [
      routes[index + step],
      routes[index],
    ];
    setRoutes(h, routes);
  }
  const routeOptions = [
    ...harnesses,
    ...(draft?.aliases.map((a) => `@${a.name}`) || []),
  ];
  return (
    <details
      className="panel-section panel-routing"
      open={open}
      onToggle={(e) => setOpen(e.currentTarget.open)}
    >
      <summary
        hidden={expanded}
        onClick={(e) => {
          e.preventDefault();
          setOpen(!open);
        }}
      >
        Routing
      </summary>
      {open && (
        <>
          {message && (
            <p className="panel-error" role="alert">
              {message}
            </p>
          )}
          {errors.length > 0 && (
            <p className="panel-error" role="alert">
              {errors.map((e) => `${e.field}: ${e.message}`).join("; ")}
            </p>
          )}
          {conflict && (
            <p className="panel-error" role="alert">
              Config changed elsewhere — reload to see the latest
            </p>
          )}
          <div className="routing-heading section-heading">
            <div>
              {expanded && <h2>Routing</h2>}
              {snapshot && (
                <p className="panel-caption routing-path">
                  {snapshot.path}
                  {!snapshot.exists && " · Not configured"}
                </p>
              )}
            </div>
            <div className="routing-actions">
              {(message || conflict) && (
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={busy}
                  onClick={() => void reload()}
                >
                  Reload
                </Button>
              )}
              {snapshot && writable && !draft && (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    editVersion.current = snapshot.version;
                    setConfirmKey(null);
                    setDraft(draftOf(snapshot.config));
                    setMessage("");
                  }}
                >
                  Edit
                </Button>
              )}
              {draft && (
                <>
                  <Button size="sm" disabled={busy} onClick={() => void save()}>
                    Save
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={busy}
                    onClick={cancel}
                  >
                    Cancel
                  </Button>
                </>
              )}
            </div>
          </div>
          {!writable && (
            <p className="panel-caption">
              Edit routing from the dashboard or with pactify sub router.
            </p>
          )}
          {!snapshot ? (
            !message && <p className="panel-caption">Loading routing…</p>
          ) : (
            <>
              {draft ? (
                <fieldset disabled={busy} className="routing-editor">
                  <h2>Aliases</h2>
                  {draft.aliases.map((a, i) => {
                    const update = (key: string, value: string) =>
                      setDraft({
                        ...draft,
                        aliases: draft.aliases.map((row, j) =>
                          i === j ? { ...row, [key]: value } : row,
                        ),
                      });
                    const prefix = `Alias ${i + 1}`;
                    return (
                      <div className="routing-entry" key={i}>
                        <Field
                          label="Name"
                          field={`aliases.${a.name}`}
                          errors={errors}
                        >
                          <input
                            aria-label={`${prefix} name`}
                            value={a.name}
                            onChange={(e) => update("name", e.target.value)}
                          />
                        </Field>
                        <Field
                          label="Harness"
                          field={`aliases.${a.name}.harness`}
                          errors={errors}
                        >
                          <Harness
                            label={`${prefix} harness`}
                            value={a.harness}
                            onChange={(v) =>
                              setDraft({
                                ...draft,
                                aliases: draft.aliases.map((row, j) =>
                                  i === j
                                    ? {
                                        ...row,
                                        harness: v,
                                        model: "",
                                        effort: "",
                                      }
                                    : row,
                                ),
                              })
                            }
                          />
                        </Field>
                        <Field
                          label="Model"
                          field={`aliases.${a.name}.model`}
                          errors={errors}
                        >
                          <Choice
                            label={`${prefix} model`}
                            value={a.model}
                            choices={catalog[a.harness]?.models || []}
                            loading={modelsLoading && !catalog[a.harness]}
                            empty="Select a model"
                            onChange={(v) => update("model", v)}
                          />
                        </Field>
                        <Field
                          label="Effort (optional)"
                          field={`aliases.${a.name}.effort`}
                          errors={errors}
                        >
                          <Choice
                            label={`${prefix} effort`}
                            value={a.effort}
                            choices={catalog[a.harness]?.efforts || []}
                            empty="Harness default"
                            onChange={(v) => update("effort", v)}
                          />
                        </Field>
                        <Button
                          variant="ghost"
                          size="sm"
                          aria-label={`Remove alias ${i + 1}`}
                          onClick={() =>
                            setDraft({
                              ...draft,
                              aliases: draft.aliases.filter((_, j) => j !== i),
                            })
                          }
                        >
                          Remove
                        </Button>
                      </div>
                    );
                  })}
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() =>
                      setDraft({
                        ...draft,
                        aliases: [
                          ...draft.aliases,
                          { name: "", harness: "codex", model: "", effort: "" },
                        ],
                      })
                    }
                  >
                    Add alias
                  </Button>
                  <h2>Fallback chains</h2>
                  {harnesses.map((h) => (
                    <div className="routing-chain" key={h}>
                      <h3>{h}</h3>
                      {(draft.fallback[h] || []).map((route, i, routes) => (
                        <div className="routing-route" key={i}>
                          <Field
                            label={`Route ${i + 1}`}
                            field={`fallback.${h}[${i}]`}
                            errors={errors}
                          >
                            <select
                              aria-label={`${h} route ${i + 1}`}
                              value={route}
                              onChange={(e) =>
                                setRoutes(
                                  h,
                                  routes.map((r, j) =>
                                    i === j ? e.target.value : r,
                                  ),
                                )
                              }
                            >
                              {!routeOptions.includes(route) && (
                                <option>{route}</option>
                              )}
                              {routeOptions.map((option, j) => (
                                <option key={j}>{option}</option>
                              ))}
                            </select>
                          </Field>
                          <div className="routing-actions">
                            <Button
                              variant="ghost"
                              size="sm"
                              disabled={i === 0}
                              aria-label={`Move ${h} route ${i + 1} up`}
                              onClick={() => move(h, i, -1)}
                            >
                              ↑
                            </Button>
                            <Button
                              variant="ghost"
                              size="sm"
                              disabled={i === routes.length - 1}
                              aria-label={`Move ${h} route ${i + 1} down`}
                              onClick={() => move(h, i, 1)}
                            >
                              ↓
                            </Button>
                            <Button
                              variant="ghost"
                              size="sm"
                              aria-label={`Remove ${h} route ${i + 1}`}
                              onClick={() =>
                                setRoutes(
                                  h,
                                  routes.filter((_, j) => i !== j),
                                )
                              }
                            >
                              Remove
                            </Button>
                          </div>
                        </div>
                      ))}
                      <Button
                        variant="ghost"
                        size="sm"
                        aria-label={`Add route to ${h}`}
                        onClick={() =>
                          setRoutes(h, [...(draft.fallback[h] || []), "codex"])
                        }
                      >
                        Add route
                      </Button>
                    </div>
                  ))}
                  <h2>Quota groups</h2>
                  {draft.groups.map((g, i) => {
                    const update = (key: string, value: string | string[]) =>
                      setDraft({
                        ...draft,
                        groups: draft.groups.map((row, j) =>
                          i === j ? { ...row, [key]: value } : row,
                        ),
                      });
                    const prefix = `Quota group ${i + 1}`;
                    return (
                      <div className="routing-entry" key={i}>
                        <Field
                          label="Name"
                          field={`quotaGroups.${g.name}`}
                          errors={errors}
                        >
                          <input
                            aria-label={`${prefix} name`}
                            value={g.name}
                            onChange={(e) => update("name", e.target.value)}
                          />
                        </Field>
                        <Field
                          label="Harness"
                          field={`quotaGroups.${g.name}.harness`}
                          errors={errors}
                        >
                          <Harness
                            label={`${prefix} harness`}
                            value={g.harness}
                            onChange={(v) => update("harness", v)}
                          />
                        </Field>
                        <div className="routing-models">
                          {g.models.map((model, m) => (
                            <div className="routing-route" key={m}>
                              <Field
                                label={`Model ${m + 1}`}
                                field={`quotaGroups.${g.name}.models`}
                                errors={errors}
                              >
                                <Choice
                                  label={`${prefix} model ${m + 1}`}
                                  value={model}
                                  choices={catalog[g.harness]?.models || []}
                                  loading={modelsLoading && !catalog[g.harness]}
                                  empty="Select a model"
                                  onChange={(v) =>
                                    update(
                                      "models",
                                      g.models.map((value, j) =>
                                        m === j ? v : value,
                                      ),
                                    )
                                  }
                                />
                              </Field>
                              <Button
                                variant="ghost"
                                size="sm"
                                aria-label={`Remove model ${m + 1} from quota group ${i + 1}`}
                                onClick={() =>
                                  update(
                                    "models",
                                    g.models.filter((_, j) => m !== j),
                                  )
                                }
                              >
                                Remove
                              </Button>
                            </div>
                          ))}
                          {g.models.length === 0 &&
                            errors
                              .filter(
                                (e) =>
                                  e.field === `quotaGroups.${g.name}.models`,
                              )
                              .map((e) => (
                                <p
                                  className="routing-field-error"
                                  key={e.field}
                                >
                                  {e.message}
                                </p>
                              ))}
                          <Button
                            variant="ghost"
                            size="sm"
                            aria-label={`Add model to quota group ${i + 1}`}
                            onClick={() => update("models", [...g.models, ""])}
                          >
                            Add model
                          </Button>
                        </div>
                        <Button
                          variant="ghost"
                          size="sm"
                          aria-label={`Remove quota group ${i + 1}`}
                          onClick={() =>
                            setDraft({
                              ...draft,
                              groups: draft.groups.filter((_, j) => i !== j),
                            })
                          }
                        >
                          Remove
                        </Button>
                      </div>
                    );
                  })}
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() =>
                      setDraft({
                        ...draft,
                        groups: [
                          ...draft.groups,
                          { name: "", harness: "codex", models: [""] },
                        ],
                      })
                    }
                  >
                    Add quota group
                  </Button>
                </fieldset>
              ) : (
                <div className="routing-config">
                  <section>
                    <h2>Aliases</h2>
                    {Object.entries(snapshot.config.aliases).map(
                      ([name, r]) => (
                        <div className="routing-read-row" key={name}>
                          <strong>@{name}</strong>
                          <span>{r.harness}</span>
                          <span>{r.model || "Default model"}</span>
                          {r.effort && <span>{r.effort}</span>}
                        </div>
                      ),
                    )}
                    {Object.keys(snapshot.config.aliases).length === 0 && (
                      <p className="panel-caption">No aliases.</p>
                    )}
                  </section>
                  <section>
                    <h2>Fallback chains</h2>
                    {Object.entries(snapshot.config.fallback).map(
                      ([h, routes]) => (
                        <div className="routing-read-row" key={h}>
                          <strong>{h}</strong>
                          <span>{routes.join(" → ") || "No routes"}</span>
                        </div>
                      ),
                    )}
                    {Object.keys(snapshot.config.fallback).length === 0 && (
                      <p className="panel-caption">No fallback chains.</p>
                    )}
                  </section>
                  <section>
                    <h2>Quota groups</h2>
                    {Object.entries(snapshot.config.quotaGroups).map(
                      ([name, g]) => (
                        <div className="routing-read-row" key={name}>
                          <strong>{name}</strong>
                          <span>{g.harness}</span>
                          <span>{g.models.join(", ")}</span>
                        </div>
                      ),
                    )}
                    {Object.keys(snapshot.config.quotaGroups).length === 0 && (
                      <p className="panel-caption">No quota groups.</p>
                    )}
                  </section>
                </div>
              )}
              <section className="routing-quotas">
                <h2>Exhausted routes</h2>
                {snapshot.exhaustedRoutes.length === 0 && (
                  <p className="panel-caption">No exhausted routes.</p>
                )}
                {snapshot.exhaustedRoutes.map((q) => (
                  <div className="routing-quota" key={q.key}>
                    <div className="routing-read-row">
                      <strong>{q.key}</strong>
                      <span>{resetCountdown(q.resetAt, now)}</span>
                      {q.resetAt && (
                        <time dateTime={q.resetAt}>
                          {new Date(q.resetAt).toLocaleString()}
                        </time>
                      )}
                      {writable && (
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={busy || !!draft}
                          onClick={() => setConfirmKey(q.key)}
                        >
                          Clear
                        </Button>
                      )}
                    </div>
                    {confirmKey === q.key && (
                      <div
                        className="routing-confirm"
                        role="dialog"
                        aria-label={`Clear quota for ${q.key}`}
                      >
                        <p>
                          Clear quota exhaustion for {q.key}? This only clears
                          local memory; the provider limit may still apply.
                        </p>
                        <div className="routing-actions">
                          <Button
                            variant="danger"
                            size="sm"
                            disabled={busy}
                            onClick={() => void clear()}
                          >
                            Clear
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            disabled={busy}
                            onClick={() => setConfirmKey(null)}
                          >
                            Cancel
                          </Button>
                        </div>
                      </div>
                    )}
                  </div>
                ))}
              </section>
            </>
          )}
        </>
      )}
    </details>
  );
}
