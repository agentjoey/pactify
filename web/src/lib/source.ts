import type { RelaySource } from "./relaysource";
import { LocalServeSource, type DataSource } from "./datasource";

// The dashboard runs in one of two modes:
//   - LOCAL  (no relay URL): talks to the co-located `pactify serve` over /api —
//     full read+write. This is the serve-embedded build.
//   - HOSTED (relay URL set): talks to the zero-knowledge relay. This is the
//     Vercel build.
//
// The relay URL is resolved by HOSTNAME first, then the build-time env var. One
// hosted bundle therefore serves both environments correctly regardless of which
// branch/env it was built from (Vercel bakes a single VITE_PACTIFY_RELAY_URL per
// project, so a hostname map is how staging and production diverge):
//   orx.agentjoey.ai → staging relay      orx.pactify.dev → production relay
// Anything else (localhost, *.vercel.app previews, the serve-embedded build)
// falls back to VITE_PACTIFY_RELAY_URL (empty ⇒ LOCAL).
const RELAY_BY_HOST: Record<string, string> = {
  "orx.agentjoey.ai": "https://pactify-relay-staging.fly.dev",
  "orx.pactify.dev": "https://pactify-relay.fly.dev",
};

function resolveRelayUrl(): string {
  if (typeof window !== "undefined" && window.location) {
    const mapped = RELAY_BY_HOST[window.location.hostname];
    if (mapped) return mapped;
  }
  return (import.meta.env.VITE_PACTIFY_RELAY_URL as string | undefined)?.trim() || "";
}

const RELAY_URL = resolveRelayUrl();

/** True when this build targets the hosted relay (a relay URL was configured). */
export function isHostedMode(): boolean {
  return RELAY_URL !== "";
}

/** The configured relay base URL, or "" in local mode. */
export function relayUrl(): string {
  return RELAY_URL;
}

/** The always-available local data source (co-located serve). */
export function localSource(): DataSource {
  return new LocalServeSource();
}

/** Decode a hex master-secret string into bytes (no dep; validates hex length). */
export function hexToBytes(hex: string): Uint8Array {
  const clean = hex.trim().toLowerCase();
  if (clean.length === 0 || clean.length % 2 !== 0 || /[^0-9a-f]/.test(clean)) {
    throw new Error("master secret must be an even-length hex string");
  }
  const out = new Uint8Array(clean.length / 2);
  for (let i = 0; i < out.length; i++) {
    out[i] = parseInt(clean.slice(i * 2, i * 2 + 2), 16);
  }
  return out;
}

/** Encode bytes back to a lowercase hex string. */
export function bytesToHex(bytes: Uint8Array): string {
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

/**
 * Build a hosted RelaySource from a hex master secret and authenticate it.
 * The master is derived into the account key locally and never leaves the
 * browser (the relay is zero-knowledge). Throws if the hex is malformed or auth
 * fails. The caller holds the returned source in memory only — it is not
 * persisted (a master secret in localStorage would be an XSS liability; a proper
 * device-pairing flow replaces the paste step later, see backlog FE-8).
 */
export async function connectRelaySource(masterHex: string): Promise<RelaySource> {
  if (!isHostedMode()) {
    throw new Error("no relay configured (VITE_PACTIFY_RELAY_URL unset)");
  }
  const master = hexToBytes(masterHex);
  const [{ RelaySource }, { RelayClient }] = await Promise.all([
    import("./relaysource"),
    import("@pactify-apps/relay-client"),
  ]);
  const client = new RelayClient(RELAY_URL, master);
  await client.login();
  return new RelaySource(client);
}

/**
 * Build a hosted RelaySource from an active SSO session. The relay bearer token is
 * fetched with the WebSession cookie; decryption is unavailable until the user
 * supplies the master secret (the source is created in locked mode).
 */
export async function connectSessionSource(accountId: string): Promise<RelaySource> {
  if (!isHostedMode()) {
    throw new Error("no relay configured (VITE_PACTIFY_RELAY_URL unset)");
  }
  const [{ RelaySource }, { RelayClient }, { fetchToken }] = await Promise.all([
    import("./relaysource"),
    import("@pactify-apps/relay-client"),
    import("./identity"),
  ]);
  const { token } = await fetchToken(accountId);
  const client = RelayClient.bearer(RELAY_URL, token);
  return new RelaySource(client, { locked: true });
}
