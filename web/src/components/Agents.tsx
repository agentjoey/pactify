import { useEffect, useState } from "react";
import type { AgentRow } from "../lib/api";
import { getAgents, registerAgent, unregisterAgent } from "../lib/api";
import { Button } from "./ui/Button";
import { Badge } from "./ui/Badge";

export function Agents({ author, onChanged }: { author: boolean; onChanged: () => void }) {
  const [rows, setRows] = useState<AgentRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  function load() {
    setLoading(true);
    getAgents()
      .then(setRows)
      .catch(() => setError("Failed to load agents"))
      .finally(() => setLoading(false));
  }

  useEffect(() => {
    load();
  }, []);

  const hasRegistered = rows.some((a) => a.registered);
  const canAct = author;

  async function toggle(kind: string, current: boolean) {
    if (!canAct) return;
    setRows((prev) =>
      prev.map((a) =>
        a.kind === kind ? { ...a, registered: !current } : a,
      ),
    );
    try {
      if (current) {
        await unregisterAgent(kind);
      } else {
        await registerAgent(kind);
      }
      onChanged();
    } catch {
      load();
    }
  }

  if (loading) return null;
  if (error) return null;

  return (
    <div className="border-b border-gray-800 px-3 py-2">
      {!hasRegistered && (
        <div className="text-xs text-gray-400 mb-2">
          扫描到这些 agent，选择注册以开始
        </div>
      )}
      <div className="space-y-1.5">
        {rows.map((a) => (
          <div
            key={a.kind}
            className={`flex items-center gap-2 text-sm ${!a.installed ? "opacity-40" : ""}`}
          >
            <span className="font-medium min-w-[80px]">{a.kind}</span>
            <Badge color={a.installed ? "success" : "warn"}>
              {a.installed ? "已安装" : "未检测到"}
            </Badge>
            <Button
              variant={a.registered ? "ghost" : "primary"}
              size="sm"
              disabled={!a.installed || !canAct}
              onClick={() => toggle(a.kind, a.registered)}
            >
              {a.registered ? "已注册" : "注册"}
            </Button>
          </div>
        ))}
      </div>
    </div>
  );
}
