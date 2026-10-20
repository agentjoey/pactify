import { readJobLogs } from "../src/components/jobs/source";

const sessionKey = "pactify-panel-session";

// Only the one-time launch response contains this metadata. A refreshed shell
// retains the tab's sessionStorage and gets no secret from the server.
let sessionChannel: BroadcastChannel | undefined;

export function initializeSession(): Promise<boolean> {
  const meta = document.querySelector<HTMLMetaElement>('meta[name="pactify-session"]');
  if (meta) {
    const url = new URL(location.href);
    // A reload restores this tab immediately. Only an explicit launch exchange
    // may replace an existing session with newly supplied bootstrap metadata.
    if (!sessionStorage.getItem(sessionKey) || url.searchParams.has("token")) {
      sessionStorage.setItem(sessionKey, meta.content);
    }
    meta.remove();
    url.searchParams.delete("token");
    history.replaceState(null, "", url.pathname + url.search + url.hash);
  }
  const hasSession = !!sessionStorage.getItem(sessionKey);
  sessionChannel?.close();
  try {
    const channel = new BroadcastChannel("pactify-panel-session");
    sessionChannel = channel;
    const nonce = hasSession ? undefined : crypto.randomUUID();
    return new Promise((resolve) => {
      let pending = !hasSession;
      const timer = pending ? setTimeout(() => {
        pending = false;
        channel.close();
        resolve(false);
      }, 2000) : undefined;
      channel.onmessage = ({ data }) => {
        if (!data || typeof data.nonce !== "string" || data.nonce.length > 128) return;
        const token = sessionStorage.getItem(sessionKey);
        if (data.type === "request" && token) {
          channel.postMessage({ type: "response", nonce: data.nonce, token });
        } else if (pending && data.type === "response" && data.nonce === nonce &&
          typeof data.token === "string" && data.token.length > 0 && data.token.length <= 1024) {
          pending = false;
          clearTimeout(timer);
          sessionStorage.setItem(sessionKey, data.token);
          resolve(true);
        }
      };
      if (pending) channel.postMessage({ type: "request", nonce });
      else resolve(true);
    });
  } catch {
    sessionChannel?.close();
    return Promise.resolve(hasSession);
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
      sessionStorage.removeItem(sessionKey);
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
