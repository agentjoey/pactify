export type JobsFetch = (path: string, signal: AbortSignal) => Promise<Response>;

export interface JobsSource {
  jobsPath: string;
  costPath: string;
  fetch: JobsFetch;
}

export const dashboardJobsSource: JobsSource = {
  jobsPath: "/api/sub/jobs",
  costPath: "/api/sub/cost",
  async fetch(path, signal) {
    const response = await fetch(path, { signal, credentials: "same-origin" });
    if (!response.ok) throw new Error(`Failed to load (${response.status}). Retrying shortly.`);
    return response;
  },
};

// fetch supports Authorization and aborting the body, unlike EventSource.
// Do not reconnect automatically: that would replay the initial tail.
export async function readJobLogs(
  fetcher: JobsFetch,
  path: string,
  signal: AbortSignal,
  line: (text: string) => void,
  status: (text: string) => void,
) {
  const response = await fetcher(path, signal);
  if (!response.body) throw new Error("Log stream unavailable");
  const reader = response.body.getReader();
  const abort = () => { void reader.cancel(); };
  signal.addEventListener("abort", abort, { once: true });
  if (signal.aborted) abort();
  const decoder = new TextDecoder();
  let pending = "";
  status("Connected");
  try {
    while (!signal.aborted) {
      const { value, done } = await reader.read();
      if (done) break;
      pending += decoder.decode(value, { stream: true });
      let boundary: number;
      while ((boundary = pending.indexOf("\n\n")) >= 0) {
        const frame = pending.slice(0, boundary);
        pending = pending.slice(boundary + 2);
        let event = "message";
        const data: string[] = [];
        for (const field of frame.split("\n")) {
          if (field.startsWith("event:")) event = field.slice(6).trim();
          if (field.startsWith("data:")) data.push(field.slice(5).trimStart());
        }
        if (event === "done") { status("Finished"); return; }
        if (event === "log-error") { status("Failed to read logs. Reload to retry."); return; }
        if (event === "message" && data.length) {
          try {
            const text: unknown = JSON.parse(data.join("\n"));
            if (typeof text === "string") line(text);
          } catch { status("Could not parse log entry"); }
        }
      }
      if (pending.length > 1 << 20) throw new Error("Log entry exceeds stream limit");
    }
    if (!signal.aborted) status("Connection lost. Reload to retry.");
  } finally {
    signal.removeEventListener("abort", abort);
    await reader.cancel();
    reader.releaseLock();
  }
}
