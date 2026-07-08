import { useEffect, useState } from "react";
import { getAgents, getAgentConfig, setAgentConfig, type AgentConfig as Config } from "../../lib/api";
import { Badge } from "../ui/Badge";
import { Button } from "../ui/Button";
import { Input } from "../ui/Input";
import { Select } from "../ui/Select";
import { Alert } from "../ui/Alert";
import { EmptyState } from "../ui/EmptyState";
import { Spinner } from "../ui/Spinner";
import { AgentLogo } from "../../lib/agentLogos";

// AgentConfig (#10 model / #9 permission posture / #4 scoped tools) — per
// registered agent, edit the model pin and the permission posture orchestrate
// drives it under. Author-gated (it mutates the machine agent registry). Each
// registered kind gets a row that loads its config and saves overrides.
export function AgentConfig({ refreshKey }: { refreshKey?: number }) {
  const [kinds, setKinds] = useState<string[] | null>(null);
  const [error, setError] = useState("");

  function load() {
    getAgents()
      .then((rows) => {
        setKinds(rows.filter((r) => r.registered).map((r) => r.kind));
        setError("");
      })
      .catch(() => setError("Failed to load agents"));
  }

  useEffect(() => {
    load();
  }, [refreshKey]); // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <section data-testid="ops-agent-config" className="mb-4">
      <div
        data-testid="agent-config-scope-banner"
        className="mb-3 flex items-center gap-2 rounded-lg border border-[rgba(110,231,160,0.28)] bg-[rgba(110,231,160,0.10)] px-3 py-2"
      >
        <span className="h-1.5 w-1.5 rounded-[2px] bg-[#6ee7a0]" />
        <span className="text-[10px] font-semibold uppercase tracking-[0.6px] text-[#6ee7a0]">
          MACHINE · all projects
        </span>
        <span className="text-[10.5px] text-[rgba(234,238,245,0.55)]">
          Model and permissions are machine-level — seat assignments live under Project · Seats &amp; roles.
        </span>
      </div>

      {error && (
        <Alert tone="danger" onRetry={load}>
          {error}
        </Alert>
      )}
      {!error && kinds && kinds.length === 0 && (
        <EmptyState
          title="No agents registered"
          hint="Register an agent to configure its model and permission posture here: pactify agent register <kind>."
        />
      )}
      {!error && kinds && kinds.length > 0 && (
        <div className="flex flex-col gap-3">
          {kinds.map((k, i) => (
            <AgentConfigRow key={k} kind={k} delay={i * 40} />
          ))}
        </div>
      )}
    </section>
  );
}

function AgentConfigRow({ kind, delay = 0 }: { kind: string; delay?: number }) {
  const [cfg, setCfg] = useState<Config | null>(null);
  const [model, setModel] = useState("");
  const [restricted, setRestricted] = useState(false);
  const [tools, setTools] = useState("");
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState("");
  const [saved, setSaved] = useState(false);
  // customMode: the model dropdown's "custom…" branch is active — the loaded
  // model isn't one of the curated candidates (or the user picked custom),
  // so the free-text field is shown.
  const [customMode, setCustomMode] = useState(false);

  function apply(c: Config) {
    setCfg(c);
    setModel(c.model);
    setRestricted(c.restricted);
    setTools((c.allowed_tools ?? []).join(", "));
    const candidates = c.candidate_models ?? [];
    setCustomMode(c.model !== "" && !candidates.includes(c.model));
  }

  useEffect(() => {
    getAgentConfig(kind).then(apply).catch(() => setErr("load failed"));
  }, [kind]);

  const save = async () => {
    setSaving(true);
    setErr("");
    try {
      const c = await setAgentConfig(kind, {
        model: model.trim(),
        restricted,
        allowed_tools: tools
          .split(",")
          .map((t) => t.trim())
          .filter(Boolean),
      });
      apply(c);
      setSaved(true);
      setTimeout(() => setSaved(false), 1500);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "save failed");
    } finally {
      setSaving(false);
    }
  };

  const parsedTools = tools
    .split(",")
    .map((t) => t.trim())
    .filter(Boolean);

  const dim = cfg ? !cfg.drivable : false;

  return (
    <div
      data-testid={`agent-config-${kind}`}
      style={{ animationDelay: `${delay}ms` }}
      className={[
        "rounded-xl border px-4 py-3.5 fade-rise",
        dim
          ? "border-[rgba(255,255,255,0.07)] bg-[var(--color-bg-inset)] opacity-[.62]"
          : "border-[rgba(255,255,255,0.08)] bg-[var(--color-bg-surface)]",
      ].join(" ")}
    >
      <div className="flex items-center gap-3">
        <AgentLogo kind={kind} size={30} />
        <div>
          <div className="font-mono text-[13px] font-[650] text-[var(--color-text-1)]">{kind}</div>
          {cfg && (
            <div className="text-[10px] text-[var(--color-text-3)]">
              effective {cfg.effective_model}
              {cfg.effective_scoped ? " · scoped" : ""}
            </div>
          )}
        </div>
        {cfg && (
          <Badge color={cfg.drivable ? "role-dev" : "role-design"}>
            {cfg.drivable ? "drivable" : "manual"}
          </Badge>
        )}
        {!cfg && <Spinner size="xs" />}
      </div>

      {cfg && cfg.drivable && (
        <div className="mt-3.5 flex flex-wrap gap-4">
          <label className="flex min-w-[230px] flex-1 flex-col gap-1.5">
            <span className="text-[10px] font-medium text-[var(--color-text-3)]">Model</span>
            {(cfg.candidate_models ?? []).length > 0 ? (
              <>
                <Select
                  data-testid={`model-select-${kind}`}
                  value={customMode ? "__custom__" : model}
                  onChange={(e) => {
                    const v = e.target.value;
                    if (v === "__custom__") {
                      setCustomMode(true);
                    } else {
                      setCustomMode(false);
                      setModel(v);
                    }
                  }}
                  className="w-full text-xs"
                >
                  <option value="">default</option>
                  {(cfg.candidate_models ?? []).map((m) => (
                    <option key={m} value={m}>
                      {m}
                    </option>
                  ))}
                  <option value="__custom__">custom…</option>
                </Select>
                {customMode && (
                  <Input
                    data-testid={`model-${kind}`}
                    value={model}
                    onChange={(e) => setModel(e.target.value)}
                    placeholder="model id"
                    className="w-full text-xs"
                  />
                )}
              </>
            ) : (
              <Input
                data-testid={`model-${kind}`}
                value={model}
                onChange={(e) => setModel(e.target.value)}
                placeholder="default"
                className="w-full text-xs"
              />
            )}
          </label>

          <label className="flex flex-col gap-1.5">
            <span className="text-[10px] font-medium text-[var(--color-text-3)]">Permission posture</span>
            <div className="inline-flex rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] p-0.5">
              <button
                type="button"
                data-testid={`posture-blanket-${kind}`}
                aria-pressed={!restricted}
                onClick={() => setRestricted(false)}
                className={[
                  "rounded-md px-3.5 py-1.5 text-[11px] font-medium transition-all duration-[var(--motion-micro)]",
                  !restricted
                    ? "bg-[#222b3a] text-[var(--color-text-1)] shadow-[0_1px_2px_rgba(0,0,0,.4)]"
                    : "text-[var(--color-text-3)] hover:text-[var(--color-text-2)]",
                ].join(" ")}
              >
                Blanket
              </button>
              <button
                type="button"
                data-testid={`posture-scoped-${kind}`}
                aria-pressed={restricted}
                onClick={() => setRestricted(true)}
                className={[
                  "rounded-md px-3.5 py-1.5 text-[11px] font-medium transition-all duration-[var(--motion-micro)]",
                  restricted
                    ? "bg-[#222b3a] text-[var(--color-text-1)] shadow-[0_1px_2px_rgba(0,0,0,.4)]"
                    : "text-[var(--color-text-3)] hover:text-[var(--color-text-2)]",
                ].join(" ")}
              >
                Scoped
              </button>
            </div>
          </label>
        </div>
      )}

      {cfg && cfg.drivable && restricted && (
        <div className="mt-3">
          <span className="text-[10px] font-medium text-[var(--color-text-3)]">Allowed tools</span>
          <div className="mt-1.5 flex flex-wrap items-center gap-2">
            {parsedTools.map((t) => (
              <span
                key={t}
                data-testid="allowed-tool-chip"
                className="inline-flex items-center gap-1 rounded-md border px-2.5 py-1 text-[10.5px] font-medium"
                style={{
                  color: "var(--color-role-design)",
                  background: "color-mix(in srgb, var(--color-role-design) 10%, transparent)",
                  borderColor: "color-mix(in srgb, var(--color-role-design) 28%, transparent)",
                }}
              >
                {t}
              </span>
            ))}
            <Input
              data-testid={`tools-${kind}`}
              value={tools}
              onChange={(e) => setTools(e.target.value)}
              placeholder="Read, Edit, Bash"
              className="min-w-[140px] flex-1 border-dashed border-[rgba(255,255,255,0.16)] bg-transparent text-xs placeholder:text-[var(--color-text-3)]"
            />
          </div>
        </div>
      )}

      {cfg && cfg.drivable && (
        <div className="mt-3 flex items-center gap-2">
          <Button size="sm" loading={saving} onClick={save}>
            {saved ? "Saved ✓" : "Save"}
          </Button>
          {/* Keep the legacy scoped toggle so existing tests that target
              `scoped-${kind}` continue to flip the posture. */}
          <button
            type="button"
            data-testid={`scoped-${kind}`}
            aria-pressed={restricted}
            onClick={() => setRestricted((s) => !s)}
            className="sr-only"
            tabIndex={-1}
          >
            {restricted ? "scoped" : "blanket"}
          </button>
        </div>
      )}

      {err && (
        <div className="mt-2">
          <Alert tone="danger">{err}</Alert>
        </div>
      )}
    </div>
  );
}
