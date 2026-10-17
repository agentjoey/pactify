import type { JobsSource } from "./source";
import { readJobStream } from "./source";

// Shared by every Live surface. Abort releases a slot only after its reader has
// stopped, so overlapping mounts and navigation never exceed four connections.
const waiting: (() => void)[] = [];
let active = 0;
function pump() {
  while (active < 4 && waiting.length) waiting.shift()!();
}
export function liveStream(
  source: JobsSource,
  id: string,
  event: (e: unknown) => void,
  status: (s: string) => void,
) {
  const controller = new AbortController();
  let canceled = false;
  const start = () => {
    if (canceled) return;
    active++;
    status("Connecting");
    void readJobStream(
      source.fetch,
      `${source.jobsPath}/${encodeURIComponent(id)}/events`,
      controller.signal,
      event,
      status,
    )
      .catch(() => {
        if (!canceled) status("Stream unavailable · summary only");
      })
      .finally(() => {
        active--;
        pump();
      });
  };
  status("Summary only · waiting for stream slot");
  waiting.push(start);
  pump();
  return () => {
    canceled = true;
    const i = waiting.indexOf(start);
    if (i >= 0) waiting.splice(i, 1);
    controller.abort();
  };
}
