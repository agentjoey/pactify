import type { State } from "../lib/types";
import { boardColumns, COLUMNS } from "../lib/derive";
export function Board({ state, selected, onSelect }: { state: State; selected: string; onSelect: (id: string) => void }) {
  const cols = boardColumns(state);
  return (
    <div className="flex gap-2 p-3 flex-1 overflow-x-auto">
      {COLUMNS.map((c) => (
        <div key={c} className="flex-1 min-w-[140px]">
          <div className="text-[10px] font-semibold tracking-wide text-gray-500 mb-1.5 uppercase">{c.replace("_", " ")}</div>
          {cols[c].map((bt) => (
            <button key={bt.task.id} onClick={() => onSelect(bt.task.id)}
              className={`w-full text-left bg-[#161b22] border rounded p-2 mb-1.5 text-xs ${selected === bt.task.id ? "border-yellow-500" : "border-gray-700"}`}>
              <div>{bt.task.id}</div>
              <div className="text-[10px] text-gray-500">{bt.task.owner}</div>
            </button>
          ))}
        </div>
      ))}
    </div>
  );
}
