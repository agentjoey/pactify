import { useEffect, useState } from "react";
import { useDataSource } from "../lib/datasource";
import type { Machine } from "../lib/types";
import { Badge } from "./ui/Badge";
import { relTime } from "../lib/reltime";

export function Machines() {
  const src = useDataSource();
  const [machines, setMachines] = useState<Machine[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState("");

  useEffect(() => {
    if (!src.getMachines) {
      setMachines(null);
      return;
    }
    let alive = true;
    setLoading(true);
    setErr("");
    src
      .getMachines()
      .then((ms) => {
        if (alive) setMachines(ms);
      })
      .catch((e) => {
        if (alive) setErr(e instanceof Error ? e.message : String(e));
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [src]);

  if (!src.getMachines) {
    return (
      <div data-testid="machines-local" className="text-xs text-[var(--color-text-2)]">
        Machines view is only available in hosted mode.
      </div>
    );
  }

  if (loading) {
    return (
      <div data-testid="machines-loading" className="text-xs text-[var(--color-text-3)]">
        Loading machines…
      </div>
    );
  }
  if (err) {
    return (
      <div data-testid="machines-error" className="text-xs text-[var(--color-danger)]">
        {err}
      </div>
    );
  }
  if (!machines || machines.length === 0) {
    return (
      <div data-testid="machines-empty" className="text-xs text-[var(--color-text-3)]">
        No machines online.
      </div>
    );
  }

  return (
    <div data-testid="machines-list" className="flex flex-col gap-2">
      {machines.map((m) => {
        const label = m.host || m.machineId.slice(0, 8);
        return (
          <div
            key={m.machineId}
            data-testid="machine-row"
            className="flex items-center gap-3 rounded-xl border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-3 py-2.5"
          >
            <span
              data-testid="machine-online-indicator"
              className="h-2.5 w-2.5 shrink-0 rounded-full"
              style={{
                background: m.online ? "var(--color-success)" : "var(--color-text-3)",
              }}
              title={m.online ? "online" : "offline"}
            />
            <div className="min-w-0 flex-1">
              <div className="truncate text-xs font-medium text-[var(--color-text-1)]">{label}</div>
              <div className="mt-1 flex flex-wrap items-center gap-1.5">
                {m.agentKinds.map((kind) => (
                  <Badge key={kind} color="role-dev">
                    {kind}
                  </Badge>
                ))}
              </div>
            </div>
            <span className="mono whitespace-nowrap text-[10px] text-[var(--color-text-3)]">
              {relTime(new Date(m.lastSeenAt).toISOString())}
            </span>
          </div>
        );
      })}
    </div>
  );
}
