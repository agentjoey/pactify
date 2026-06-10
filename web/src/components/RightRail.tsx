import { useState } from "react";
import type { State, PactEvent } from "../lib/types";
import { findTask, canMergeFeature } from "../lib/derive";
import { postVerb } from "../lib/api";

export function RightRail({
  state,
  events,
  selected,
  project,
  author,
}: {
  state: State;
  events: PactEvent[];
  selected: string;
  project?: string;
  author?: boolean;
}) {
  const bt = selected ? findTask(state, selected) : undefined;
  const [reason, setReason] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setErr("");
    try {
      await fn();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const reviewable = bt?.task.status === "awaiting_review";
  const mergeable = bt ? canMergeFeature(state, bt.feature) : false;

  return (
    <div className="w-[320px] p-3 border-l border-gray-800 overflow-y-auto">
      {bt && (
        <div className="bg-[#161b22] border border-gray-700 rounded p-2 mb-3">
          <div className="text-[10px] font-semibold text-gray-500 uppercase">Evidence · {bt.task.id} ({bt.task.status})</div>
          <pre className="text-[10px] text-green-400 bg-[#0d1117] rounded p-2 mt-1 whitespace-pre-wrap">{bt.task.evidence || "(none yet)"}</pre>

          {author && project && reviewable && (
            <div className="mt-2">
              <div className="text-[10px] font-semibold text-gray-500 uppercase mb-1">Review</div>
              <div className="flex gap-1.5">
                <button
                  className="rounded bg-[#238636] px-2 py-0.5 text-[11px] font-semibold text-white disabled:opacity-50"
                  disabled={busy}
                  onClick={() => run(() => postVerb(project, "accept", { task: bt.task.id }))}
                >
                  Accept
                </button>
                <button
                  className="rounded border border-[#f85149] px-2 py-0.5 text-[11px] text-[#f85149] disabled:opacity-50"
                  disabled={busy || !reason.trim()}
                  onClick={() =>
                    run(async () => {
                      await postVerb(project, "changes", { task: bt.task.id, reason: reason.trim() });
                      setReason("");
                    })
                  }
                >
                  Request changes
                </button>
              </div>
              <textarea
                aria-label="changes reason"
                className="mt-1.5 h-16 w-full resize-y rounded border border-gray-700 bg-[#0d1117] px-1.5 py-1 text-[11px] text-[#e6edf3]"
                placeholder="reason (required to request changes)"
                value={reason}
                onChange={(e) => setReason(e.target.value)}
              />
            </div>
          )}

          {author && project && (
            <div className="mt-2">
              <div className="text-[10px] font-semibold text-gray-500 uppercase mb-1">Feature · {bt.feature}</div>
              <button
                className="rounded bg-[#1f6feb] px-2 py-0.5 text-[11px] font-semibold text-white disabled:opacity-50"
                disabled={busy || !mergeable}
                title={mergeable ? "" : "all tasks must be accepted"}
                onClick={() => run(() => postVerb(project, "merge", { feature: bt.feature }))}
              >
                Merge {bt.feature}
              </button>
            </div>
          )}

          {err && <p className="mt-2 whitespace-pre-wrap text-[11px] text-[#f85149]">{err}</p>}
        </div>
      )}
      <div className="text-[10px] font-semibold text-gray-500 uppercase mb-1">Live event stream</div>
      {events.slice().reverse().slice(0, 50).map((e) => (
        <div key={e.event_id} className="text-[10px] font-mono text-gray-300">{e.ts.slice(11, 19)} {e.agent_id} → {e.event_type} {e.task_id}</div>
      ))}
    </div>
  );
}
