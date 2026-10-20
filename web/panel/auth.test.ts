import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { initializeSession, panelFetch, streamLogs } from "./auth";

class TestChannel {
  static instances: TestChannel[] = [];
  onmessage: ((event: { data: unknown }) => void) | null = null;
  postMessage = vi.fn();
  close = vi.fn();
  readonly name: string;
  constructor(name: string) {
    this.name = name;
    TestChannel.instances.push(this);
  }
  receive(data: unknown) { this.onmessage?.({ data }); }
}
const sessionKey = "pactify-panel-session";
const channel = () => TestChannel.instances.at(-1)!;
function bootstrap(token: string) {
  const meta = document.createElement("meta");
  meta.name = "pactify-session";
  meta.content = token;
  document.head.append(meta);
}
beforeEach(() => {
  vi.useFakeTimers();
  TestChannel.instances = [];
  vi.stubGlobal("BroadcastChannel", TestChannel);
});
afterEach(() => {
  for (const instance of TestChannel.instances) instance.close();
  sessionStorage.clear();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.useRealTimers();
  document.querySelector('meta[name="pactify-session"]')?.remove();
  history.replaceState(null, "", "/");
});

it("keeps the exchanged session across refresh and sends only a bearer header", async () => {
  const meta = document.createElement("meta");
  meta.name = "pactify-session"; meta.content = "session-secret";
  document.head.append(meta);
  history.replaceState(null, "", "/?token=launch-secret");
  expect(await initializeSession()).toBe(true);
  expect(location.search).toBe("");
  expect(document.querySelector('meta[name="pactify-session"]')).toBeNull();
  expect(await initializeSession()).toBe(true); // Refreshed shell has no bootstrap metadata.
  const fetcher = vi.fn(async (_path: string, _options: RequestInit) => new Response("[]"));
  vi.stubGlobal("fetch", fetcher);
  await panelFetch("/api/jobs", new AbortController().signal);
  expect(fetcher.mock.calls[0][1]).toMatchObject({ headers: { Authorization: "Bearer session-secret" }, credentials: "omit" });
});

it("reads authenticated SSE through fetch across chunk boundaries and stops at done", async () => {
  sessionStorage.setItem("pactify-panel-session", "session-secret");
  const chunks = ['data: "hello 世', '界"\n\nevent: done\ndata: {}\n\n'];
  const bytes = new TextEncoder().encode(chunks.join(""));
  let canceled = false;
  const fetcher = vi.fn(async (_path: string, _options: RequestInit) => new Response(new ReadableStream({
    start(controller) { controller.enqueue(bytes.slice(0, 15)); controller.enqueue(bytes.slice(15, 17)); controller.enqueue(bytes.slice(17)); },
    cancel() { canceled = true; },
  })));
  vi.stubGlobal("fetch", fetcher);
  const lines: string[] = [], statuses: string[] = [];
  await streamLogs("/logs", new AbortController().signal, (line) => lines.push(line), (status) => statuses.push(status));
  expect(lines).toEqual(["hello 世界"]);
  expect(statuses.at(-1)).toBe("Finished");
  expect(canceled).toBe(true);
  expect(fetcher.mock.calls[0][1]).toMatchObject({ headers: { Authorization: "Bearer session-secret" }, credentials: "omit" });
});

it("restores the stored session immediately on reload instead of replacing it or requesting a handoff", async () => {
  sessionStorage.setItem(sessionKey, "tab-session");
  // The browser fixture supplies fresh metadata even on reload; it must not
  // replace the tab's session without an explicit launch URL.
  bootstrap("mock-panel-session");
  const schedule = vi.spyOn(globalThis, "setTimeout");
  const ready = initializeSession();
  expect(schedule).not.toHaveBeenCalled();
  expect(await ready).toBe(true);
  expect(sessionStorage.getItem(sessionKey)).toBe("tab-session");
  expect(channel().postMessage).not.toHaveBeenCalled();
  const fetcher = vi.fn(async () => Response.json([]));
  vi.stubGlobal("fetch", fetcher);
  await panelFetch("/api/jobs?all=1", new AbortController().signal);
  expect(fetcher).toHaveBeenCalledWith("/api/jobs?all=1", expect.objectContaining({
    headers: {Authorization: "Bearer tab-session"},
  }));
});

it("allows an explicit launch exchange to replace an older stored session", async () => {
  sessionStorage.setItem(sessionKey, "old-session");
  bootstrap("new-session");
  history.replaceState(null, "", "/?token=new-launch");
  expect(await initializeSession()).toBe(true);
  expect(sessionStorage.getItem(sessionKey)).toBe("new-session");
  expect(location.search).toBe("");
});

it("uses an existing session without needing nonce generation", async () => {
  sessionStorage.setItem(sessionKey, "tab-session");
  const randomUUID = vi.fn(() => {throw new Error("unavailable");});
  vi.stubGlobal("crypto", {randomUUID});
  const schedule = vi.spyOn(globalThis, "setTimeout");
  const ready = initializeSession();
  expect(schedule).not.toHaveBeenCalled();
  expect(await ready).toBe(true);
  expect(randomUUID).not.toHaveBeenCalled();
  expect(channel().postMessage).not.toHaveBeenCalled();
});

it("handles nonce generation failure without throwing or leaving a channel open", async () => {
  vi.stubGlobal("crypto", {randomUUID: () => {throw new Error("unavailable");}});
  expect(await initializeSession()).toBe(false);
  expect(channel().close).toHaveBeenCalled();
});

it("requests another tab's session with a nonce and ignores unsolicited responses and replays", async () => {
  const ready = initializeSession();
  const request = channel().postMessage.mock.calls[0][0];
  expect(channel().name).toBe("pactify-panel-session");
  expect(request).toMatchObject({type: "request", nonce: expect.any(String)});
  channel().receive({type: "response", nonce: "wrong", token: "wrong"});
  expect(sessionStorage.getItem(sessionKey)).toBeNull();
  channel().receive({type: "response", nonce: request.nonce, token: "shared-secret"});
  expect(await ready).toBe(true);
  channel().receive({type: "response", nonce: request.nonce, token: "replacement-secret"});
  expect(sessionStorage.getItem(sessionKey)).toBe("shared-secret");
  channel().receive({type: "request", nonce: "next-tab"});
  expect(channel().postMessage).toHaveBeenLastCalledWith({type: "response", nonce: "next-tab", token: "shared-secret"});
  expect(location.href).not.toContain("shared-secret");
});

it("no longer answers handoff requests after a 401 clears the session", async () => {
  sessionStorage.setItem(sessionKey, "session-secret");
  expect(await initializeSession()).toBe(true);
  vi.stubGlobal("fetch", vi.fn(async () => new Response("", {status: 401})));
  await expect(panelFetch("/api/jobs", new AbortController().signal)).rejects.toMatchObject({status: 401});
  expect(sessionStorage.getItem(sessionKey)).toBeNull();
  channel().receive({type: "request", nonce: "after-expiry"});
  expect(channel().postMessage).not.toHaveBeenCalled();
});

it("never answers handoff requests while it has no token", async () => {
  const ready = initializeSession();
  channel().postMessage.mockClear();
  channel().receive({type: "request", nonce: "other-tab"});
  expect(channel().postMessage).not.toHaveBeenCalled();
  await vi.advanceTimersByTimeAsync(2000);
  expect(await ready).toBe(false);
});

it("accepts a 128-character nonce and ignores a 129-character nonce", async () => {
  sessionStorage.setItem(sessionKey, "session-secret");
  expect(await initializeSession()).toBe(true);
  channel().receive({type: "request", nonce: "n".repeat(129)});
  expect(channel().postMessage).not.toHaveBeenCalled();
  channel().receive({type: "request", nonce: "n".repeat(128)});
  expect(channel().postMessage).toHaveBeenCalledExactlyOnceWith({type: "response", nonce: "n".repeat(128), token: "session-secret"});
});

it("accepts a 1024-character token and ignores a 1025-character or empty token", async () => {
  const ready = initializeSession();
  const {nonce} = channel().postMessage.mock.calls[0][0];
  for (const token of ["t".repeat(1025), ""]) {
    channel().receive({type: "response", nonce, token});
    expect(sessionStorage.getItem(sessionKey)).toBeNull();
  }
  channel().receive({type: "response", nonce, token: "t".repeat(1024)});
  expect(await ready).toBe(true);
  expect(sessionStorage.getItem(sessionKey)).toBe("t".repeat(1024));
});

it("never logs the token or writes it to localStorage during bootstrap, handoff or expiry", async () => {
  const methods = ["log", "info", "warn", "error", "debug", "trace", "table"] as const;
  const logs = methods.map(method => vi.spyOn(console, method).mockImplementation(() => {}));
  const writes = vi.spyOn(Storage.prototype, "setItem");
  const ready = initializeSession();
  const {nonce} = channel().postMessage.mock.calls[0][0];
  channel().receive({type: "response", nonce, token: "shared-secret"});
  expect(await ready).toBe(true);
  channel().receive({type: "request", nonce: "next-tab"});
  bootstrap("new-secret");
  history.replaceState(null, "", "/?token=launch-secret");
  expect(await initializeSession()).toBe(true);
  vi.stubGlobal("fetch", vi.fn(async () => new Response("", {status: 401})));
  await expect(panelFetch("/api/jobs", new AbortController().signal)).rejects.toMatchObject({status: 401});
  expect(writes).toHaveBeenCalledWith(sessionKey, "shared-secret");
  expect(writes).toHaveBeenCalledWith(sessionKey, "new-secret");
  expect(writes.mock.contexts).not.toContain(localStorage);
  for (const log of logs) expect(log).not.toHaveBeenCalled();
});

it("times out without a peer instead of requesting APIs without a session", async () => {
  const ready = initializeSession();
  await vi.advanceTimersByTimeAsync(2000);
  expect(await ready).toBe(false);
  expect(channel().close).toHaveBeenCalled();
});
