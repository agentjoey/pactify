import { useEffect, useRef, useState } from "react";
import { getAgents, getAgentConfig, setAgentConfig, type AgentConfig as Config } from "../../lib/api";
import { Badge } from "../ui/Badge";
import { Input } from "../ui/Input";
import { Select } from "../ui/Select";
import { Alert } from "../ui/Alert";
import { EmptyState } from "../ui/EmptyState";
import { Spinner } from "../ui/Spinner";

function arraysEqual(a: string[], b: string[]) {
  if (a.length !== b.length) return false;
  return a.every((v, i) => v === b[i]);
}

// AgentConfig (#10 model / #9 permission posture / #4 scoped tools) — per
// registered agent, edit the model pin and the permission posture orchestrate
// drives it under. Author-gated (it mutates the machine agent registry). Each
// registered kind gets a row that loads its config and saves overrides.
// AgentConfigBody 是 AgentConfigRow 的 embedded 形态，供 Agents 合并页在展开态
// 复用同一份配置逻辑（自动保存 / 模型下拉 / 权限档 / Save）。
export function AgentConfigBody({ kind, initial }: { kind: string; initial?: Config }) {
  return <AgentConfigRow kind={kind} embedded initial={initial} />;
}

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
  }, [refreshKey]);

  return (
    <section data-testid="ops-agent-config" className="mb-4">
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

function initials(kind: string): string {
  return kind.slice(0, 2).toLowerCase();
}

function Monogram({ kind, drivable }: { kind: string; drivable: boolean }) {
  return (
    <span
      className="grid shrink-0 place-items-center rounded-[10px] font-mono text-[13px] font-bold"
      style={{
        width: 36,
        height: 36,
        color: drivable ? "var(--accent)" : "var(--text-3)",
        background: drivable ? "var(--accent-2)" : "var(--bg-elev2)",
        border: `1px solid ${drivable ? "var(--accent-line)" : "var(--border-2)"}`,
      }}
    >
      {initials(kind)}
    </span>
  );
}

// AgentConfigRow 有两种形态：
//   standalone（默认）—— 自带卡片头部与边框
//   embedded —— 只渲染配置体，供 Agents 合并页在展开态复用
// 抽 prop 而不是另写一份配置 UI：自动保存 / 模型下拉 / 权限档这套逻辑与其测试只能有一份。
// initial 让父组件把已拉到的 config 传下来，避免展开时重复请求。
function AgentConfigRow({
  kind,
  delay = 0,
  embedded = false,
  initial,
}: {
  kind: string;
  delay?: number;
  embedded?: boolean;
  initial?: Config;
}) {
  const [cfg, setCfg] = useState<Config | null>(null);
  const [model, setModel] = useState("");
  const [restricted, setRestricted] = useState(false);
  const [tools, setTools] = useState("");
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState("");
  const [saved, setSaved] = useState(false);
  const [savedVisible, setSavedVisible] = useState(false);
  const [hydrated, setHydrated] = useState(false);
  // customMode: the model dropdown's "custom…" branch is active — the loaded
  // model isn't one of the curated candidates (or the user picked custom),
  // so the free-text field is shown.
  const [customMode, setCustomMode] = useState(false);
  const autosaveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  function apply(c: Config) {
    setCfg(c);
    setModel(c.model);
    setRestricted(c.restricted);
    setTools((c.allowed_tools ?? []).join(", "));
    const candidates = c.candidate_models ?? [];
    setCustomMode(c.model !== "" && !candidates.includes(c.model));
  }

  useEffect(() => {
    if (initial) {
      apply(initial);
      setHydrated(true);
      return;
    }
    getAgentConfig(kind)
      .then((c) => {
        apply(c);
        setHydrated(true);
      })
      .catch(() => setErr("load failed"));
  }, [kind, initial]);

  const save = async (payload: { model: string; restricted: boolean; allowed_tools: string[] }) => {
    setSaving(true);
    setErr("");
    try {
      const c = await setAgentConfig(kind, payload);
      apply(c);
      setSaved(true);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "save failed");
    } finally {
      setSaving(false);
    }
  };

  useEffect(() => {
    if (!saved) return;
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setSavedVisible(true);
    const fade = setTimeout(() => setSavedVisible(false), 1500);
    const hide = setTimeout(() => setSaved(false), 2000);
    return () => {
      clearTimeout(fade);
      clearTimeout(hide);
    };
  }, [saved]);

  useEffect(() => {
    if (!hydrated || !cfg) return;
    const payload = {
      model: model.trim(),
      restricted,
      allowed_tools: tools
        .split(",")
        .map((t) => t.trim())
        .filter(Boolean),
    };
    const unchanged =
      payload.model === cfg.model &&
      payload.restricted === cfg.restricted &&
      arraysEqual(payload.allowed_tools, cfg.allowed_tools ?? []);
    if (unchanged) return;

    autosaveTimer.current = setTimeout(() => {
      autosaveTimer.current = null;
      save(payload);
    }, 600);
    return () => {
      if (autosaveTimer.current) {
        clearTimeout(autosaveTimer.current);
        autosaveTimer.current = null;
      }
    };
  }, [hydrated, cfg, model, restricted, tools]); // eslint-disable-line react-hooks/exhaustive-deps

  const flushSave = () => {
    if (autosaveTimer.current) {
      clearTimeout(autosaveTimer.current);
      autosaveTimer.current = null;
      save({
        model: model.trim(),
        restricted,
        allowed_tools: tools
          .split(",")
          .map((t) => t.trim())
          .filter(Boolean),
      });
    }
  };

  const saveNow = () => {
    if (autosaveTimer.current) {
      clearTimeout(autosaveTimer.current);
      autosaveTimer.current = null;
    }
    save({
      model: model.trim(),
      restricted,
      allowed_tools: tools
        .split(",")
        .map((t) => t.trim())
        .filter(Boolean),
    });
  };

  const parsedTools = tools
    .split(",")
    .map((t) => t.trim())
    .filter(Boolean);

  const dim = cfg ? !cfg.drivable : false;

  function removeTool(t: string) {
    setTools(parsedTools.filter((x) => x !== t).join(", "));
  }

  function addTool(t: string) {
    const list = parsedTools;
    const v = t.trim();
    if (!v || list.includes(v)) return;
    setTools([...list, v].join(", "));
  }

  const effectiveSub =
    cfg &&
    `effective ${cfg.effective_model}${
      cfg.effective_scoped ? " · scoped" : " · blanket"
    }${cfg.effective_scoped && (cfg.allowed_tools?.length ?? 0) ? ` · ${cfg.allowed_tools!.length} tools` : ""}`;

  // configBody = 模型 / 权限档 / Save / legacy scoped 隐藏按钮 / 错误条。
  // standalone 与 embedded 共用同一份 —— 各写一份必然漂移。
  const configBody = (
    <>
      {cfg && cfg.drivable && (
        <div className="border-t border-[var(--border-2)] bg-[var(--bg)] px-4 py-3.5">
          <div className="flex flex-wrap gap-4">
            <label className="flex min-w-[230px] flex-1 flex-col gap-1.5">
              <span className="font-mono text-[9.5px] uppercase tracking-[0.16em] text-[var(--color-text-3)]">
                Model · built-in list
              </span>
              {(cfg.candidate_models ?? []).length > 0 ? (
                <>
                  <div className="flex items-center gap-2 rounded-lg border border-[var(--border)] bg-[var(--bg-input)] px-3 py-2">
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
                      className="flex-1 border-0 bg-transparent px-0 py-0 font-mono text-[12.5px] text-[var(--text)]"
                    >
                      <option value="">default</option>
                      {(cfg.candidate_models ?? []).map((m) => (
                        <option key={m} value={m}>
                          {m}
                        </option>
                      ))}
                      <option value="__custom__">custom…</option>
                    </Select>
                    <span className="text-[9px] text-[var(--text-4)]">▾</span>
                  </div>
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
              <span className="font-mono text-[10.5px] text-[var(--color-text-4)]">
                pinned · overrides the machine default
              </span>
            </label>

            <label className="flex flex-col gap-1.5">
              <span className="font-mono text-[9.5px] uppercase tracking-[0.16em] text-[var(--color-text-3)]">
                Permission posture
              </span>
              <div className="inline-flex gap-[3px] rounded-lg border border-[var(--border-2)] bg-[var(--bg-input)] p-[3px]">
                <button
                  type="button"
                  data-testid={`posture-blanket-${kind}`}
                  aria-pressed={!restricted}
                  onClick={() => setRestricted(false)}
                  className={[
                    "rounded-md px-[15px] py-[7px] text-[11.5px] font-semibold transition-all duration-[var(--motion-micro)]",
                    !restricted
                      ? "text-[var(--accent-ink)]"
                      : "text-[var(--color-text-3)] hover:text-[var(--color-text-2)]",
                  ].join(" ")}
                  style={!restricted ? { background: "var(--accent)" } : undefined}
                >
                  Blanket
                </button>
                <button
                  type="button"
                  data-testid={`posture-scoped-${kind}`}
                  aria-pressed={restricted}
                  onClick={() => setRestricted(true)}
                  className={[
                    "rounded-md px-[15px] py-[7px] text-[11.5px] font-semibold transition-all duration-[var(--motion-micro)]",
                    restricted
                      ? "text-[var(--accent-ink)]"
                      : "text-[var(--color-text-3)] hover:text-[var(--color-text-2)]",
                  ].join(" ")}
                  style={restricted ? { background: "var(--accent)" } : undefined}
                >
                  Scoped
                </button>
              </div>
            </label>
          </div>

          {restricted && (
            <div className="mt-3">
              <span className="font-mono text-[9.5px] uppercase tracking-[0.16em] text-[var(--color-text-3)]">
                Allowed tools
              </span>
              <div className="mt-1.5 flex flex-wrap items-center gap-2">
                {parsedTools.map((t) => (
                  <span
                    key={t}
                    data-testid="allowed-tool-chip"
                    className="inline-flex items-center gap-1 rounded-lg border px-[11px] py-[5px] font-mono text-[11px] font-medium"
                    style={{
                      color: "var(--accent)",
                      background: "var(--accent-2)",
                      borderColor: "var(--accent-line)",
                    }}
                  >
                    {t}
                    <button
                      type="button"
                      onClick={() => removeTool(t)}
                      className="ml-0.5 text-[10px] text-[var(--accent)] hover:text-[var(--text)]"
                      aria-label={`remove ${t}`}
                    >
                      ×
                    </button>
                  </span>
                ))}
                <Input
                  data-testid={`tools-${kind}`}
                  value={tools}
                  onChange={(e) => setTools(e.target.value)}
                  onBlur={flushSave}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      e.preventDefault();
                      const val = (e.target as HTMLInputElement).value
                        .split(",")
                        .map((s) => s.trim())
                        .filter(Boolean)
                        .pop();
                      if (val) addTool(val);
                    }
                  }}
                  placeholder="Read, Edit, Bash"
                  className="min-w-[140px] flex-1 border-dashed border-[rgba(255,255,255,0.16)] bg-transparent font-mono text-[11px] text-[var(--text)] placeholder:text-[var(--text-4)]"
                />
              </div>
            </div>
          )}

          <div className="mt-4 flex justify-end">
            <button
              type="button"
              data-testid={`save-${kind}`}
              onClick={saveNow}
              disabled={saving}
              className="rounded-lg bg-[var(--accent)] px-[18px] py-2 text-[12.5px] font-semibold text-[var(--accent-ink)] transition-colors hover:brightness-110 disabled:opacity-50"
            >
              {saving ? "Saving…" : "Save"}
            </button>
          </div>
        </div>
      )}

      {cfg && cfg.drivable && (
        // Keep the legacy scoped toggle so existing tests that target
        // `scoped-${kind}` continue to flip the posture.
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
      )}

      {err && (
        <div className="px-4 pb-3.5">
          <Alert tone="danger">{err}</Alert>
        </div>
      )}
    </>
  );

  if (embedded) {
    if (!cfg) return <div className="px-4 py-3"><Spinner size="xs" /></div>;
    if (!cfg.drivable) {
      return (
        <div className="px-4 py-3 text-[11.5px] text-[var(--color-text-2)]">
          not drivable — no model or posture to configure
        </div>
      );
    }
    return <div data-testid={`agent-config-${kind}`}>{configBody}</div>;
  }

  return (
    <div
      data-testid={`agent-config-${kind}`}
      style={{ animationDelay: `${delay}ms` }}
      className={[
        "fade-rise overflow-hidden rounded-xl border",
        dim
          ? "border-[rgba(255,255,255,0.07)] bg-[var(--color-bg-inset)] opacity-[.62]"
          : "border-[rgba(255,255,255,0.08)] bg-[var(--color-bg-surface)]",
      ].join(" ")}
    >
      <div className="flex items-center gap-3 px-4 py-3.5">
        <Monogram kind={kind} drivable={cfg?.drivable ?? false} />
        <div className="min-w-0 flex-1">
          <div className="flex items-baseline gap-2">
            <span className="text-[14px] font-semibold text-[var(--color-text-1)]">{kind}</span>
            {cfg && <span className="text-[12px] text-[var(--color-text-3)]">{kind}</span>}
          </div>
          {cfg && (
            <div className="font-mono text-[10.5px] text-[var(--color-text-4)]">
              {dim ? "not drivable — no model or posture to configure" : effectiveSub}
            </div>
          )}
        </div>
        {cfg && (
          <Badge color={cfg.drivable ? "role-dev" : "role-design"}>
            {cfg.drivable ? "drivable" : "manual"}
          </Badge>
        )}
        {!cfg && <Spinner size="xs" />}
        {cfg && cfg.drivable && (
          <div data-testid="autosave-state" className="text-[11px] font-medium">
            {err ? (
              <span className="text-red-400">{err}</span>
            ) : saving ? (
              <span className="text-[var(--color-text-3)]">Saving…</span>
            ) : saved ? (
              <span
                className={[
                  "text-[#6ee7a0] transition-opacity duration-500",
                  savedVisible ? "opacity-100" : "opacity-0",
                ].join(" ")}
              >
                Saved ✓
              </span>
            ) : null}
          </div>
        )}
      </div>

      {configBody}
    </div>
  );
}
