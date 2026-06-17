import { useState } from "react";
import { createManifest } from "../../lib/api";
import { Button } from "../ui/Button";
import { Input } from "../ui/Input";
import { Select } from "../ui/Select";
import { Alert } from "../ui/Alert";

// CustomAgentForm — a small TOML assembler for user-declared custom agents.
// The backend parses/validates the manifest and writes it to ~/.pactify/agents.
export function CustomAgentForm({
  onCreated,
  author = true,
}: {
  onCreated: () => void;
  author?: boolean;
}) {
  const [show, setShow] = useState(true);
  const [kind, setKind] = useState("");
  const [binary, setBinary] = useState("");
  const [entry, setEntry] = useState("");
  const [args, setArgs] = useState("");
  const [defaultModel, setDefaultModel] = useState("");
  const [models, setModels] = useState("");
  const [mcpPath, setMcpPath] = useState("");
  const [mcpScope, setMcpScope] = useState("project");
  const [mcpFormat, setMcpFormat] = useState("mcpServers");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  function assembleTOML(): string {
    const lines: string[] = [];
    lines.push(`kind = ${JSON.stringify(kind.trim())}`);
    lines.push(`binary = ${JSON.stringify(binary.trim())}`);
    if (entry.trim()) {
      lines.push(`entry = ${JSON.stringify(entry.trim())}`);
    }

    const argTokens = args
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);
    lines.push("[runner]");
    lines.push(`args = [${argTokens.map((t) => JSON.stringify(t)).join(", ")}]`);
    if (defaultModel.trim()) {
      lines.push(`default_model = ${JSON.stringify(defaultModel.trim())}`);
    }
    const modelTokens = models
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);
    if (modelTokens.length > 0) {
      lines.push(`models = [${modelTokens.map((t) => JSON.stringify(t)).join(", ")}]`);
    }

    if (mcpPath.trim()) {
      lines.push("[mcp]");
      lines.push(`config_path = ${JSON.stringify(mcpPath.trim())}`);
      lines.push(`scope = ${JSON.stringify(mcpScope)}`);
      lines.push(`format = ${JSON.stringify(mcpFormat)}`);
    }

    return lines.join("\n") + "\n";
  }

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!author) return;
    setLoading(true);
    setError("");
    try {
      await createManifest(assembleTOML());
      setKind("");
      setBinary("");
      setEntry("");
      setArgs("");
      setDefaultModel("");
      setModels("");
      setMcpPath("");
      setMcpScope("project");
      setMcpFormat("mcpServers");
      onCreated();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create manifest");
    } finally {
      setLoading(false);
    }
  };

  return (
    <section data-testid="ops-custom-agent" className="mb-4">
      <div className="mb-2 flex items-center gap-2">
        <h2 className="text-[10px] font-semibold uppercase text-[var(--color-text-3)]">
          Custom agent
        </h2>
        <button
          type="button"
          data-testid="custom-agent-toggle"
          aria-label="Toggle custom agent form"
          onClick={() => setShow((s) => !s)}
          className="press text-[11px] text-[var(--color-text-3)] hover:text-[var(--color-text-1)]"
        >
          {show ? "▾" : "▸"}
        </button>
      </div>

      {show && (
        <form
          onSubmit={submit}
          className="rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] p-3"
        >
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <label className="flex flex-col gap-1">
              <span className="text-[10.5px] text-[var(--color-text-3)]">kind</span>
              <Input
                data-testid="ca-kind"
                value={kind}
                onChange={(e) => setKind(e.target.value)}
                placeholder="my-agent"
                disabled={!author}
                required
              />
            </label>
            <label className="flex flex-col gap-1">
              <span className="text-[10.5px] text-[var(--color-text-3)]">binary</span>
              <Input
                data-testid="ca-binary"
                value={binary}
                onChange={(e) => setBinary(e.target.value)}
                placeholder="myagent"
                disabled={!author}
                required
              />
            </label>
            <label className="flex flex-col gap-1">
              <span className="text-[10.5px] text-[var(--color-text-3)]">entry file</span>
              <Input
                value={entry}
                onChange={(e) => setEntry(e.target.value)}
                placeholder="AGENTS.md"
                disabled={!author}
              />
            </label>
            <label className="flex flex-col gap-1">
              <span className="text-[10.5px] text-[var(--color-text-3)]">
                args (comma-separated)
              </span>
              <Input
                data-testid="ca-args"
                value={args}
                onChange={(e) => setArgs(e.target.value)}
                placeholder="run,{briefing}"
                disabled={!author}
              />
            </label>
            <label className="flex flex-col gap-1">
              <span className="text-[10.5px] text-[var(--color-text-3)]">default model</span>
              <Input
                value={defaultModel}
                onChange={(e) => setDefaultModel(e.target.value)}
                placeholder="claude-4-sonnet"
                disabled={!author}
              />
            </label>
            <label className="flex flex-col gap-1">
              <span className="text-[10.5px] text-[var(--color-text-3)]">
                models (comma-separated)
              </span>
              <Input
                value={models}
                onChange={(e) => setModels(e.target.value)}
                placeholder="claude-4-sonnet,claude-4-opus"
                disabled={!author}
              />
            </label>
            <label className="flex flex-col gap-1">
              <span className="text-[10.5px] text-[var(--color-text-3)]">mcp config path</span>
              <Input
                value={mcpPath}
                onChange={(e) => setMcpPath(e.target.value)}
                placeholder=".myagent/mcp.json"
                disabled={!author}
              />
            </label>
            <div className="grid grid-cols-2 gap-2">
              <label className="flex flex-col gap-1">
                <span className="text-[10.5px] text-[var(--color-text-3)]">mcp scope</span>
                <Select
                  value={mcpScope}
                  onChange={(e) => setMcpScope(e.target.value)}
                  disabled={!author}
                >
                  <option value="project">project</option>
                  <option value="global">global</option>
                </Select>
              </label>
              <label className="flex flex-col gap-1">
                <span className="text-[10.5px] text-[var(--color-text-3)]">mcp format</span>
                <Select
                  value={mcpFormat}
                  onChange={(e) => setMcpFormat(e.target.value)}
                  disabled={!author}
                >
                  <option value="mcpServers">mcpServers</option>
                  <option value="opencode">opencode</option>
                  <option value="toml">toml</option>
                </Select>
              </label>
            </div>
          </div>

          {error && (
            <div className="mt-3">
              <Alert tone="danger">{error}</Alert>
            </div>
          )}

          <div className="mt-3 flex items-center gap-2">
            <Button type="submit" size="sm" loading={loading} disabled={!author}>
              Add custom agent
            </Button>
            {!author && (
              <span className="text-[10.5px] text-[var(--color-text-3)]">Observer mode</span>
            )}
          </div>
        </form>
      )}
    </section>
  );
}
