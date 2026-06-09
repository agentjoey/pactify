import type { ProjectMeta } from "../lib/types";
export function TopBar({ projects, current, onSelect, live }: {
  projects: ProjectMeta[]; current: string; onSelect: (id: string) => void; live: boolean;
}) {
  return (
    <div className="flex items-center gap-3 px-3 py-2 border-b border-gray-800 bg-[#0f1419]">
      <select className="bg-gray-800 border border-gray-700 rounded px-2 py-1 text-sm" value={current} onChange={(e) => onSelect(e.target.value)}>
        {projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
      </select>
      <span className="ml-auto text-xs text-gray-500">{projects.length} project(s)</span>
      <span className={`text-xs ${live ? "text-green-400" : "text-gray-500"}`}>● {live ? "live" : "offline"}</span>
    </div>
  );
}
