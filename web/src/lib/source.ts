import { RelayClient } from "@pactify-apps/relay-client";
import { RelaySource } from "./relaysource";
import { LocalServeSource, type DataSource } from "./datasource";

// The dashboard runs in one of two modes, decided at build time by whether a
// relay URL is configured:
//   - LOCAL  (no VITE_PACTIFY_RELAY_URL): talks to the co-located `pactify serve`
//     over /api — full read+write. This is the serve-embedded build.
//   - HOSTED (VITE_PACTIFY_RELAY_URL set): talks to the zero-knowledge relay,
//     read-only (writes gated until U3). This is the Vercel build.
// The env var is inlined by Vite at build; empty/undefined ⇒ LOCAL.
const RELAY_URL = (import.meta.env.VITE_PACTIFY_RELAY_URL as string | undefined)?.trim() || "";

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
  const client = new RelayClient(RELAY_URL, master);
  await client.login();
  return new RelaySource(client);
}
