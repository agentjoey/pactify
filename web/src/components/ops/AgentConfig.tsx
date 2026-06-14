import { useEffect, useState } from "react";
import { getAgents, getAgentConfig, setAgentConfig, type AgentConfig as Config } from "../../lib/api";
import { Badge } from "../ui/Badge";
import { Button } from "../ui/Button";
import { Input } from "../ui/Input";
import { Alert } from "../ui/Alert";
import { EmptyState } from "../ui/EmptyState";
import { Spinner } from "../ui/Spinner";

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
      <h2 className="text-[10px] font-semibold text-[var(--color-text-3)] uppercase mb-2">
        Agent config · model + 权限姿态
      </h2>
      {error && (
        <Alert tone="danger" onRetry={load}>
          {error}
        </Alert>
      )}
      {!error && kinds && kinds.length === 0 && (
        <EmptyState
          title="没有已注册的 agent"
          hint="注册 agent 后可在这里配置 model 与权限姿态：pactify agent register <kind>。"
        />
      )}
      {!error && kinds && kinds.length > 0 && (
        <div className="flex flex-col gap-2">
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

  function apply(c: Config) {
    setCfg(c);
    setModel(c.model);
    setRestricted(c.restricted);
    setTools((c.allowed_tools ?? []).join(", "));
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

  return (
    <div
      data-testid={`agent-config-${kind}`}
      style={{ animationDelay: `${delay}ms` }}
      className="rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-3 py-2.5 fade-rise"
    >
      <div className="flex items-center gap-2">
        <span className="mono text-[12px] font-medium text-[var(--color-text-1)]">{kind}</span>
        {cfg && (
          <Badge color={cfg.drivable ? "role-dev" : "role-design"}>
            {cfg.drivable ? "drivable" : "manual"}
          </Badge>
        )}
        {cfg && (
          <span className="text-[10.5px] text-[var(--color-text-3)]">
            有效: {cfg.effective_model}
            {cfg.effective_scoped ? " · scoped" : ""}
          </span>
        )}
        {!cfg && <Spinner size="xs" />}
      </div>

      {cfg && (
        <div className="mt-2 flex flex-wrap items-center gap-2">
          <label className="text-[10.5px] text-[var(--color-text-3)]">model</label>
          <Input
            data-testid={`model-${kind}`}
            value={model}
            onChange={(e) => setModel(e.target.value)}
            placeholder="默认"
            className="w-52 text-[11px]"
          />
          <button
            type="button"
            data-testid={`scoped-${kind}`}
            aria-pressed={restricted}
            onClick={() => setRestricted((s) => !s)}
            className={[
              "rounded px-2 py-0.5 text-[10.5px] press transition-colors duration-[var(--motion-micro)] outline-none focus-visible:ring-2",
              restricted
                ? "bg-[color-mix(in_srgb,var(--color-warn)_22%,transparent)] text-[var(--color-text-1)]"
                : "bg-[var(--color-bg-raised)] text-[var(--color-text-3)] hover:text-[var(--color-text-2)]",
            ].join(" ")}
            title="scoped = 用 allowed-tools 白名单替代全量自动批准"
          >
            {restricted ? "scoped" : "blanket"}
          </button>
          {restricted && (
            <Input
              data-testid={`tools-${kind}`}
              value={tools}
              onChange={(e) => setTools(e.target.value)}
              placeholder="Read, Edit, Bash"
              className="w-44 text-[11px]"
            />
          )}
          <Button size="sm" loading={saving} onClick={save}>
            {saved ? "已保存 ✓" : "保存"}
          </Button>
        </div>
      )}

      {err && (
        <div className="mt-1.5">
          <Alert tone="danger">{err}</Alert>
        </div>
      )}
    </div>
  );
}
