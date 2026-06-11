import { useEffect, useState } from "react";
import type { SeatInfo } from "../../lib/types";
import { getSeats } from "../../lib/api";
import { roleColorVar } from "../../lib/canvas";
import { relativeTime } from "../../lib/ops";

// Seats renders the roster table with join provenance. It is read-only (the
// point of provenance is observability), so it shows in observe-only mode too.
// Role chips are colored via the brand role-color token (shared with the canvas).
export function Seats({ project, refreshKey }: { project: string; refreshKey?: number }) {
  const [seats, setSeats] = useState<SeatInfo[]>([]);
  const [err, setErr] = useState("");

  useEffect(() => {
    if (!project) {
      setSeats([]);
      return;
    }
    let alive = true;
    getSeats(project)
      .then((s) => { if (alive) { setSeats(s); setErr(""); } })
      .catch((e) => { if (alive) setErr(e instanceof Error ? e.message : String(e)); });
    return () => { alive = false; };
  }, [project, refreshKey]);

  return (
    <section data-testid="ops-seats" className="mb-4">
      <h2 className="text-[10px] font-semibold text-gray-500 uppercase mb-2">Seats · provenance</h2>
      {err && <p className="text-[11px] text-[#f85149] whitespace-pre-wrap">{err}</p>}
      {!err && seats.length === 0 && <p className="text-[11px] text-gray-600">no seats</p>}
      {seats.length > 0 && (
        <table className="w-full text-[11px]">
          <thead>
            <tr className="text-left text-gray-500">
              <th className="font-normal py-1 pr-3">seat</th>
              <th className="font-normal py-1 pr-3">roles</th>
              <th className="font-normal py-1">last join</th>
            </tr>
          </thead>
          <tbody>
            {seats.map((s) => (
              <tr key={s.id} className="border-t border-gray-800 align-top">
                <td className="py-1.5 pr-3">
                  <span className="font-semibold text-[#e6edf3]">{s.id}</span>
                  {s.clientChanged && (
                    <span
                      data-testid={`seat-warn-${s.id}`}
                      title="client changed between joins"
                      className="ml-1.5 inline-block h-2 w-2 rounded-full bg-[#d29922] align-middle"
                    />
                  )}
                </td>
                <td className="py-1.5 pr-3">
                  <span className="flex flex-wrap gap-1">
                    {s.roles.map((r) => (
                      <span
                        key={r}
                        className="rounded px-1 py-0.5 text-[10px]"
                        style={{ background: "#21262d", color: `var(${roleColorVar([r])})` }}
                      >
                        {r}
                      </span>
                    ))}
                  </span>
                </td>
                <td className="py-1.5 text-gray-400 font-mono">
                  {s.lastJoin ? (
                    <span>
                      {s.lastJoin.client} v{s.lastJoin.version}
                      {" · "}
                      {relativeTime(s.lastJoin.ts)}
                    </span>
                  ) : (
                    <span className="text-gray-600">never joined</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}
