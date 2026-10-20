import { readJobLogs } from "../src/components/jobs/source";

const sessionKey = "pactify-panel-session";

// Only the one-time launch response contains this metadata. A refreshed shell
// retains the tab's sessionStorage and gets no secret from the server.
export function initializeSession() {
  const meta = document.querySelector<HTMLMetaElement>('meta[name="pactify-session"]');
  if (meta) {
    sessionStorage.setItem(sessionKey, meta.content);
    meta.remove();
    const url = new URL(location.href);
    url.searchParams.delete("token");
    history.replaceState(null, "", url.pathname + url.search + url.hash);
  }
}

export async function panelFetch(path: string, signal: AbortSignal) {
  const response = await fetch(path, {
    signal,
    credentials: "omit",
    headers: { Authorization: `Bearer ${sessionStorage.getItem(sessionKey) || ""}` },
  });
  if (!response.ok) {
    if (response.status === 401) {
      window.dispatchEvent(new Event("pactify-panel-session-ended"));
      throw Object.assign(new Error("Session ended"), { status: 401 });
    }
    throw new Error(`Failed to load (${response.status}). Retrying shortly.`);
  }
  return response;
}

// Keep the standalone API for existing callers; parsing is shared with Jobs.
export function streamLogs(
  path: string,
  signal: AbortSignal,
  line: (text: string) => void,
  status: (text: string) => void,
) {
  return readJobLogs(panelFetch, path, signal, line, status);
}
