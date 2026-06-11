import { useEffect, useState } from "react";
import type { ProjectMeta } from "../lib/types";
import { getActingSeat } from "../lib/api";

export type View = "kanban" | "canvas" | "ops";

export function TopBar({ projects, current, onSelect, live, view, onView }: {
  projects: ProjectMeta[];
  current: string;
  onSelect: (id: string) => void;
  live: boolean;
  view: View;
  onView: (v: View) => void;
}) {
  const [seat, setSeat] = useState<string>("");

  useEffect(() => {
    getActingSeat().then((r) => setSeat(r?.seat ?? "")).catch(() => setSeat(""));
  }, []);

  return (
    <div className="flex items-center gap-3 px-3 py-2 border-b border-gray-800 bg-[#0f1419]">
      <select className="bg-gray-800 border border-gray-700 rounded px-2 py-1 text-sm" value={current} onChange={(e) => onSelect(e.target.value)}>
        {projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
      </select>

      <div className="flex rounded border border-gray-700 overflow-hidden text-xs" role="group" aria-label="view toggle">
        {(["kanban", "canvas", "ops"] as const).map((v) => (
          <button
            key={v}
            onClick={() => onView(v)}
            aria-pressed={view === v}
            className={`px-2.5 py-1 ${view === v ? "bg-gray-700 text-white" : "bg-transparent text-gray-400"}`}
          >
            {v}
          </button>
        ))}
      </div>

      {seat
        ? <span className="text-xs rounded px-2 py-0.5 bg-[#21262d] border border-gray-700 text-[#3fb950]">acting as {seat}</span>
        : <span className="text-xs text-gray-600">observe-only</span>}

      <span className="ml-auto text-xs text-gray-500">{projects.length} project(s)</span>
      <span className={`text-xs ${live ? "text-green-400" : "text-gray-500"}`}>● {live ? "live" : "offline"}</span>
    </div>
  );
}
