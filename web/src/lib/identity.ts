import { relayUrl } from "./source";

const RELAY_URL = relayUrl();

export interface MeAccount {
  accountId: string;
  role: string;
  tier: string;
}

export interface MeResponse {
  email: string;
  csrf: string;
  accounts: MeAccount[];
}

export interface LinkChallenge {
  challenge: string;
}

export interface TokenResponse {
  token: string;
  expiresAt: string;
}

export interface WebSession {
  id: string;
  createdAt: string;
  expiresAt: string;
  ua?: string;
}

export interface Identity {
  id: string;
  provider: string;
  subject: string;
}

let csrfToken = "";

function idUrl(path: string): string {
  return `${RELAY_URL}${path}`;
}

function idHeaders(method: string): HeadersInit {
  const h: HeadersInit = {};
  if (method !== "GET" && method !== "HEAD") {
    h["content-type"] = "application/json";
  }
  if (method !== "GET" && method !== "HEAD" && csrfToken) {
    h["x-aw-csrf"] = csrfToken;
  }
  return h;
}

async function idFetch(path: string, init?: RequestInit): Promise<Response> {
  const method = init?.method ?? "GET";
  return fetch(idUrl(path), {
    ...init,
    credentials: "include",
    headers: { ...idHeaders(method), ...(init?.headers ?? {}) },
  });
}

async function idJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await idFetch(path, init);
  if (!res.ok) {
    let msg = `${path}: ${res.status}`;
    try {
      const body = (await res.json()) as { error?: string };
      if (body.error) msg = body.error;
    } catch { /* keep status line */ }
    throw new Error(msg);
  }
  return res.json() as Promise<T>;
}

function idWrite<T>(path: string, method: string, body: unknown): Promise<T> {
  return idJSON<T>(path, { method, body: JSON.stringify(body) });
}

/** Fetch the current SSO session + account bindings. The response carries the CSRF token. */
export async function fetchMe(): Promise<MeResponse> {
  const res = await idFetch("/v1/id/me");
  if (!res.ok) throw new Error(`me: ${res.status}`);
  const body = (await res.json()) as MeResponse;
  if (body.csrf) csrfToken = body.csrf;
  return body;
}

/** Request a magic sign-in link for the given email address. */
export function sendMagicLink(email: string): Promise<unknown> {
  return idWrite("/v1/id/magic", "POST", { email });
}

/** Verify a one-time magic-link token and establish a WebSession cookie. */
export function verifyMagicLink(token: string): Promise<unknown> {
  return idWrite("/v1/id/magic/verify", "POST", { token });
}

/** Create a new account from a freshly-generated Ed25519 public key. */
export function createAccount(publicKey: string): Promise<unknown> {
  return idWrite("/v1/id/accounts", "POST", { publicKey });
}

/** Fetch a challenge nonce to sign with an existing account key (link-by-proof). */
export function fetchLinkChallenge(): Promise<LinkChallenge> {
  return idWrite("/v1/id/link/challenge", "POST", {});
}

/** Link the current SSO user to an existing account by proving key possession. */
export function linkAccount(body: { publicKey: string; challenge: string; signature: string }): Promise<unknown> {
  return idWrite("/v1/id/link", "POST", body);
}

/** Exchange the current WebSession for a relay bearer token scoped to an account. */
export function fetchToken(accountId: string): Promise<TokenResponse> {
  return idWrite("/v1/id/token", "POST", { accountId });
}

/** List active web sessions for the signed-in user. */
export function fetchSessions(): Promise<WebSession[]> {
  return idJSON("/v1/id/sessions");
}

/** Revoke a web session by id. */
export function revokeSession(id: string): Promise<unknown> {
  return idFetch(`/v1/id/sessions/${encodeURIComponent(id)}`, { method: "DELETE" });
}

/** List SSO identities bound to the signed-in user. */
export function fetchIdentities(): Promise<Identity[]> {
  return idJSON("/v1/id/identities");
}

/** Unlink an SSO identity by id. */
export function unlinkIdentity(id: string): Promise<unknown> {
  return idFetch(`/v1/id/identities/${encodeURIComponent(id)}`, { method: "DELETE" });
}

/** Revoke the current web session on the relay and clear the CSRF token. */
export async function logout(): Promise<void> {
  await idFetch("/v1/id/logout", { method: "POST", body: JSON.stringify({}) });
  csrfToken = "";
}

/** Clear the in-memory CSRF token without a server round-trip. */
export function clearIdentitySession(): void {
  csrfToken = "";
}
