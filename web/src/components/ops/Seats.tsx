import { useEffect, useState } from "react";
import type { SeatInfo } from "../../lib/types";
import { getSeats } from "../../lib/api";
import { roleColorVar } from "../../lib/canvas";
import { relativeTime } from "../../lib/ops";
import { Badge } from "../ui/Badge";
import { Alert } from "../ui/Alert";
import { EmptyState } from "../ui/EmptyState";
import { Spinner } from "../ui/Spinner";

// Seats renders the roster table with join provenance. It is read-only (the
// point of provenance is observability), so it shows in observe-only mode too.
// Role chips are colored via the brand role-color token (shared with the canvas).
export function Seats({ project, refreshKey }: { project: string; refreshKey?: number }) {
  const [seats, setSeats] = useState<SeatInfo[]>([]);
  const [err, setErr] = useState("");
  const [loading, setLoading] = useState(false);
  const [tick, setTick] = useState(0);

  useEffect(() => {
    if (!project) {
      setSeats([]);
      return;
    }
    let alive = true;
    setLoading(true);
    getSeats(project)
      .then((s) => { if (alive) { setSeats(s); setErr(""); } })
      .catch((e) => { if (alive) setErr(e instanceof Error ? e.message : String(e)); })
      .finally(() => { if (alive) setLoading(false); });
    return () => { alive = false; };
  }, [project, refreshKey, tick]);

  return (
    <section data-testid="ops-seats" className="mb-4">
      <h2 className="flex items-center gap-2 text-[10px] font-semibold text-[var(--color-text-3)] uppercase mb-2">
        Seats · provenance {loading && <Spinner size="xs" />}
      </h2>
      {err && (
        <Alert tone="danger" onRetry={() => setTick((t) => t + 1)}>
          {err}
        </Alert>
      )}
      {!err && !loading && seats.length === 0 && (
        <EmptyState title="No seats yet" hint="Seats are registered when an agent joins (pactify join <seat>), or configure them in the Setup view." />
      )}
      {seats.length > 0 && (
        <table className="w-full text-[11px]">
          <thead>
            <tr className="text-left text-[var(--color-text-3)]">
              <th className="font-normal py-1 pr-3">seat</th>
              <th className="font-normal py-1 pr-3">roles</th>
              <th className="font-normal py-1">last join</th>
            </tr>
          </thead>
          <tbody>
            {seats.map((s) => (
              <tr key={s.id} className="border-t border-[var(--color-border-subtle)] align-top">
                <td className="py-1.5 pr-3">
                  <span className="font-semibold text-[#1a1d27]">{s.id}</span>
                  {s.clientChanged && (
                    <span
                      data-testid={`seat-warn-${s.id}`}
                      title={`client changed: ${s.lastJoin?.prevClient} → ${s.lastJoin?.client}`}
                      className="ml-1.5 inline-block h-2 w-2 rounded-full bg-[#d29922] align-middle"
                    />
                  )}
                </td>
                <td className="py-1.5 pr-3">
                  <span className="flex flex-wrap gap-1">
                    {s.roles.map((r) => (
                      <Badge key={r} colorVar={`var(${roleColorVar([r])})`}>{r}</Badge>
                    ))}
                  </span>
                </td>
                <td className="py-1.5 text-[var(--color-text-2)] font-mono">
                  {s.lastJoin ? (
                    s.lastJoin.client ? (
                      <span>
                        {s.lastJoin.client} v{s.lastJoin.version}
                        {" · "}
                        {relativeTime(s.lastJoin.ts)}
                      </span>
                    ) : (
                      // A join with no self-reported client: show a dim placeholder
                      // and skip the "v ·" version artifact.
                      <span className="text-[var(--color-text-3)]">
                        unknown client
                        {" · "}
                        {relativeTime(s.lastJoin.ts)}
                      </span>
                    )
                  ) : (
                    <span className="text-[var(--color-text-3)]">never joined</span>
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
