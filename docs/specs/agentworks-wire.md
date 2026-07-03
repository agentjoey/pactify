# AgentWorks Wire & Account Protocol (U0)

> Status: **Draft v1** (2026-07-03). Source of truth for the shared cloud layer used by
> Pactify and Linx: the wire envelope, the account key-derivation (KDF), envelope
> encryption, the auth handshake, and relay-blind pairing.
>
> This document is the **canonical specification**. The TypeScript reference lives in
> `cloud/wire` + `cloud/crypto`; a Go port (`internal/cloudauth`, planned) MUST reproduce
> the byte-exact outputs pinned in §8. When the spec and an implementation disagree, the
> spec wins and the implementation is a bug.

## 1. Scope & terminology

The shared layer lets a **machine** (a device running an agent daemon — Linx's `linxd`, or
Pactify's `serve`) publish activity to a **relay**, which a **web/client** observes and
controls. The relay is a **blind courier**: it routes and indexes cleartext operational
metadata but never sees run content — bodies are end-to-end encrypted between the account's
own devices.

- **Account** — a tenant, identified by an Ed25519 public key derived from a **master
  secret** the user holds on their devices. The relay stores only the public key.
- **Master secret** — 32 random bytes, held by the user's machine(s) + web. Never sent to
  the relay. All account keys and per-run keys derive from it.
- **Run** — one agent session on a machine. Carries a stream of events.
- **WireMessage** — one relay message: a cleartext `OperationalHeader` + an encrypted body.

### 1.1 Compatibility freeze

Everything marked **[FROZEN v1]** is a wire/byte-format contract that an existing Linx
production deployment already depends on. It MUST NOT change without a version bump (§9),
because changing it breaks existing accounts (re-derivation) or existing stored events
(re-decryption). The Go port must match FROZEN behavior exactly to interoperate.

Items marked **[HARDENING, non-breaking]** tighten validation without changing the accepted
byte format — they reject malformed/oversized input that a correct peer never sends. They
can land on v1, coordinated across both products.

Items marked **[v2, breaking]** improve the design but change derived bytes or accepted
format; they require a version bump and a migration (§9).

## 2. Wire envelope — the "C hybrid" model

A `WireMessage` is a cleartext operational header plus an opaque encrypted body:

```
WireMessage = { header: OperationalHeader, body: EncryptedBlob }
```

### 2.1 OperationalHeader [FROZEN v1]

Cleartext, machine-readable enums/counters/ids only — **no free-text run content**. The
relay indexes this to drive the fleet board, approval inbox, and push. Fields:

| field | type | notes |
|---|---|---|
| `v` | `literal(1)` | protocol version — see §2.4 |
| `machineId` | string, min 1 | routing id |
| `runId` | string, min 1 | run id, or a one-off `requestId` when `ephemeral` |
| `seq` | int ≥ 0 | per-run monotonic sequence |
| `ts` | int ≥ 0 | epoch ms |
| `state` | `RunState` | coarse state (§2.2) |
| `eventKind` | `EventKind` | body payload kind (§3) |
| `pendingApprovals` | int ≥ 0 | inbox driver |
| `tokensIn` / `tokensOut` | int ≥ 0 | usage counters |
| `costMicros` | int ≥ 0, optional | |
| `branch` | string, optional | git branch; low-sensitivity op metadata |
| `startedAt` | int, optional | epoch ms of first publish, stable per run |
| `workdir` | string, optional | resolved spawn dir |
| `repoRoot` | string, optional | `git rev-parse --show-toplevel`; groups runs by project |
| `title` | string, optional | human title, set once at spawn |
| `ephemeral` | bool, optional | transient query reply — relay forwards, MUST NOT persist |

Rationale for cleartext metadata: `machineId`/`branch`/`workdir`/`repoRoot`/`title` are
low-sensitivity operational fields that the relay/web need for routing, timers, and project
grouping. Run **content** (messages, diffs, tool output) rides only in the encrypted body.

### 2.2 RunState [FROZEN v1]

`idle | thinking | blocked | awaiting-approval | done | error` — the only state the relay
sees in cleartext.

### 2.3 EncryptedBlob [FROZEN v1]

```
EncryptedBlob = { alg: "xchacha20poly1305", nonce: base64, ct: base64 }
```

`wire` defines the shape; the crypto lives in `@pactify/crypto` (§5).

### 2.4 Version gating [HARDENING, non-breaking]

`OperationalHeader.v` is `literal(1)`, so a schema `.parse()` already rejects any other
version. **The gap the review found: the relay `ingest`/`rpc` socket handlers currently
type-cast instead of parsing, so `v` is never actually checked at runtime.** The fix is
§2.5. Once parsing is enforced, `v !== 1` is rejected at the transport boundary — which is
the whole point of pinning a version. `PROTOCOL_VERSION` (exported from `wire`) is the
single source; a future v2 (§9) is the only sanctioned way to change any FROZEN item.

### 2.5 Validation at the relay boundary [HARDENING, non-breaking] — REQUIRED

Every message the relay accepts from the network MUST be schema-validated before use:

- `ingest`: `WireMessage.safeParse(payload.msg)` — reject on failure (do not persist).
- `rpc`: `RpcRequest.safeParse(payload)` — reject and error back to sender on failure.
- `agentKind` off the socket MUST be validated against the `AgentKind` enum, not trusted
  as a free string.

Additionally, to bound resource use (all non-breaking — a correct peer stays well under):

- `OperationalHeader` string fields (`machineId`, `runId`, `branch`, `workdir`, `repoRoot`,
  `title`) get explicit `.max()` bounds. Recommended: ids ≤ 256, paths ≤ 4096, title ≤ 512.
- `EncryptedBlob.ct` and RPC free strings (`SendMessageRequest.text`, `InlineImage.data`,
  pairing `epkMachine`/`ciphertext`) get `.max()` bounds. Public keys are 32 bytes (≤ 200
  hex/b64 chars); pick a generous body cap (e.g. 1 MiB) matched to the relay's per-event
  storage budget.
- `OperationalHeader`, `WireMessage`, `EncryptedBlob` become `.strict()` — reject unknown
  keys, so nothing smuggles fields into the relay-indexed header.

> Bounds values are a **cross-product decision**: they must be ≥ the largest message Linx
> legitimately sends today. Confirm against Linx before enabling, then enable on both sides
> together.

## 3. EventKind ↔ AgentEvent mapping [HARDENING, non-breaking]

The cleartext `EventKind` (header) and the encrypted `AgentEvent.kind` (body) are two
enums that drifted apart. `EventKind` drives relay-side logic (e.g. push fires on
`approval-request`); `AgentEvent.kind` is the decrypted body discriminant. This spec pins
the contract:

**EventKind (cleartext, relay-visible):**
`snapshot | delta | message | thinking | approval-request | approval-resolved |
run-started | run-ended`

**AgentEvent.kind (encrypted body):**
`message | delta | thinking | tool-call | tool-result | diff | approval-request |
approval-resolved | state-change | usage | todo | file-list | controls | mcp | discovered |
dir-list | command-list`

Rule: the header `eventKind` is a **coarse, relay-routable classification** of the body; it
is NOT required to equal `body.kind`. The sanctioned mapping:

| body `kind` | header `eventKind` |
|---|---|
| `message` | `message` |
| `delta` | `delta` |
| `thinking` | `thinking` |
| `approval-request` | `approval-request` |
| `approval-resolved` | `approval-resolved` |
| `tool-call`, `tool-result`, `diff`, `state-change`, `usage`, `todo`, `file-list`, `controls`, `mcp`, `discovered`, `dir-list`, `command-list` | `delta` (opaque activity — relay needs no finer detail) |
| (lifecycle, no body kind) | `snapshot`, `run-started`, `run-ended` |

The publisher sets `eventKind` per this table. The relay MUST treat any unmapped/unknown
`eventKind` as opaque `delta` (forward + persist, no special handling) rather than erroring.

## 4. Account key derivation (KDF)

All derivations are **HKDF-SHA256** over the 32-byte master secret. **[FROZEN v1]** — the
Go port must match byte-for-byte (golden vectors in §8).

### 4.1 Account keypair

```
seed        = HKDF-SHA256(ikm=masterSecret, salt=<none>, info="account", L=32)
publicKey   = Ed25519.getPublicKey(seed)          // 32 bytes
publicKeyHex = lowercase hex(publicKey)            // canonical account id material
sign(msg)   = Ed25519.sign(utf8(msg), seed)        // returns lowercase hex
```

- `salt=<none>` means HKDF's zero-filled salt of HashLen (32) bytes per RFC 5869. A Go port
  MUST pass `nil` (not an empty non-nil slice — some HKDF wrappers treat `[]byte{}`
  differently). Verify against the §8 vector.
- `info` is the ASCII bytes of `"account"` (via `utf8ToBytes`). **[FROZEN v1]**
- Both the machine and the web derive the **same** keypair → the same account.

### 4.2 Per-run key

```
runKey = HKDF-SHA256(ikm=masterSecret, salt=<none>, info=utf8("run:" + runId), L=32)
```

- `salt=<none>` as in §4.1. `info` is the UTF-8 bytes of the literal `"run:"` concatenated
  with the cleartext `runId`. **[FROZEN v1]**
- The relay cannot derive this (it never holds the master secret), so keyless distribution:
  any device with the master secret + the cleartext `runId` decrypts the body.

### 4.3 Known weaknesses → v2 [v2, breaking]

The review flagged two KDF issues that CANNOT be fixed on v1 without changing derived bytes
(which would orphan every existing account/run key). They are scheduled for v2 (§9):

- **Domain separation.** Both derivations use the zero salt and rely solely on the `info`
  string. If a `runId` were ever exactly `"account"`, the run key would equal the account
  seed. v2 uses non-overlapping namespaced info strings: `"agentworks/v2/account"` and
  `"agentworks/v2/run:" + runId`. Until v2, callers MUST reject `runId == "account"` as a
  reserved id **[HARDENING, non-breaking — add this guard now]**.
- **Encoding consistency.** v1 derives the account info via `utf8ToBytes("account")` but the
  run info via `TextEncoder().encode(...)`. These agree for ASCII (all runIds today) but are
  two code paths; v2 uses one helper. The Go port MUST treat both as plain UTF-8 bytes.

## 5. Envelope encryption [FROZEN v1]

```
encrypt(runKey, event):
  nonce = random 24 bytes
  ct    = XChaCha20-Poly1305(key=runKey, nonce).encrypt( utf8(JSON.stringify(event)) )
  return { alg: "xchacha20poly1305", nonce: base64(nonce), ct: base64(ct) }

decrypt(runKey, blob):
  pt = XChaCha20-Poly1305(key=runKey, base64decode(blob.nonce)).decrypt(base64decode(blob.ct))
  return AgentEvent.parse( JSON.parse(utf8decode(pt)) )
```

- `nonce` is 24 bytes, **random per message**. XChaCha20's 192-bit nonce makes random
  nonces safe (collision-negligible); the Poly1305 tag is appended to `ct` by the AEAD.
- The plaintext is the canonical JSON of an `AgentEvent`; decrypt re-validates it against
  the schema and throws on tamper (AEAD tag) or shape mismatch.
- **[HARDENING → v2 candidate]** The review notes the nonce is transmitted as a separate
  cleartext field and is not bound as AAD. Practically not forgeable (256-bit key), but v2
  should pass the nonce as AAD: `XChaCha20-Poly1305(key, nonce, aad=nonce)`. This changes
  the tag → breaking → v2.

## 6. Auth handshake [FROZEN v1 format]

Stateless bearer token, HMAC-SHA256 under the relay's `RELAY_SECRET`:

```
POST /v1/auth  { publicKey, challenge, signature }
  → verifyChallenge: Ed25519.verify(sig, utf8(challenge), publicKey)
  → upsert Account by publicKey
  → token = base64urlnopad(JSON{ accountId, exp }) + "." + base64urlnopad(HMAC-SHA256(RELAY_SECRET, body))
```

- Token verification is constant-time (`timingSafeEqual`), rejects on bad MAC / expired
  (`exp < now`) / malformed. TTL is 24h (`RELAY_RUN`/config).
- The signed message is `utf8(challenge)` with **no domain prefix**. **[HARDENING → v2]**
  v2 should prefix (`"agentworks/v2/auth:" + challenge`) to isolate the auth signing context
  from any future signing use of the account key.

### 6.1 Replay defense [v2, breaking — U1 work, NOT in v1]

**Known critical gap (review A1):** the relay does not issue the challenge. A caller supplies
its own `challenge`; there is no server nonce, no used-challenge store. Anyone who observes
one valid `(challenge, signature)` pair can replay it indefinitely. Fixing this is a U1
deliverable and a **breaking auth-flow change** (adds a `POST /v1/auth/challenge` step +
nonce store); it MUST be designed and rolled out on both products together. This spec
records the target: server-issued, short-TTL, single-use challenges.

## 7. Relay-blind pairing [FROZEN v1 shape]

Ephemeral X25519 ECDH; the relay stores only public keys + ciphertext. Two directions share
one wire shape (`PairInit` / `PairReady` / `PairComplete` / `PairStatus`):

- **`receive`** (first pair): web is initiator, publishes `epkWeb`; the secret-holding
  machine wraps the master secret back via `complete`.
- **`provision`** (add a machine): the secret-less machine is initiator, publishes
  `epkMachine` via `ready`; the authenticated web wraps the secret back.

Shared secret → `HKDF` → symmetric key → XChaCha20-Poly1305 wrap of the master secret.

### 7.1 Known gap → U1 [v2, breaking]

**Review C3:** the flow has no key-confirmation round, so an *active* relay can substitute
`epkMachine` and MITM the pairing (the "relay-blind" property holds only against a *passive*
relay). Fixing this (a confirmation round binding a transcript hash) is a U1 deliverable and
a breaking pairing change. Additionally, pairing codes and unauthenticated pairing endpoints
need dedicated rate-limiting + higher code entropy (review F3/A5). Recorded here as the v2
target; not a v1 change.

## 8. Golden vectors (cross-language contract)

These pin the **exact** byte outputs of the FROZEN v1 derivations/encryption for fixed
inputs. The Go port MUST reproduce them; the TypeScript reference test is
`cloud/crypto/test/golden.test.ts`. **Do not edit an expected value to make a test pass** —
a mismatch is a cross-language break (revert, or bump the version deliberately).

Master secret (32 bytes `0x00..0x1f`):
```
000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f
```

| derivation | input | output |
|---|---|---|
| account publicKeyHex | master above | `ca14d356f48c1391eb7c8b51970f768360e9d25e5d9fded81b55d6aef64d79b7` |
| account sign("pactify-golden-challenge") | master above | `d8eb8cab4286a9062f09d6c1c9e539de865cf6883e080234545735936375f2d15335a256a5d098bf67fe9aad98c5b1dac2225fcc697c4bfeaac1ed29d06afe08` |
| deriveRunKey(master, "run-0001") | master above | `a58393253ee3c085d948bb7f28af35887f835d95671f61cbd8aee617f21edac9` |

Envelope (primitive determinism — fixed key + nonce + plaintext):
```
key (hex)   = 01080f161d242b323940474e555c636a71787f868d949ba2a9b0b7bec5ccd3da
nonce (b64) = AgUICw4RFBcaHSAjJiksLzI1ODs+QURH
plaintext   = {"kind":"message","role":"assistant","text":"golden"}
ct (b64)    = 8fzdY0Aj4+cQ775q7fHllfmR+blpRft8Dg6V/XY6WqhTq2Px0H/V/ToElSTRrZxL3dWTUs+vkLP9SDD06QA1YCl21UnF
```

`encryptEvent` uses a random nonce, so only the underlying AEAD (fixed key+nonce) is
vector-pinnable; `decryptEvent(key, {nonce, ct})` MUST recover the plaintext event.

## 9. Versioning & migration policy

- `PROTOCOL_VERSION` (in `wire`) is the single version integer, mirrored in
  `OperationalHeader.v`. Linx has **no compatibility layer today**; while the surface is
  small, the rule is: **any change to a [FROZEN v1] item ships as a version bump, never in
  place.**
- **Non-breaking hardening** (§2.5 validation/bounds/strict, §3 mapping, the
  `runId != "account"` guard, §4.3 encoding note for the port) stays on v1 and is enabled on
  both products together.
- **v2 (breaking)** bundles the design fixes that change bytes/format: KDF domain separation
  (§4.3), nonce-as-AAD (§5), auth challenge nonce + domain prefix (§6/§6.1), pairing key
  confirmation (§7.1). v2 requires a coordinated rollout: relay accepts both v1 and v2 during
  a migration window; accounts re-derive / re-pair onto v2; then v1 is retired. Golden
  vectors are regenerated for v2 on BOTH sides in the same change.

## 10. Go port checklist (`internal/cloudauth`)

1. HKDF-SHA256 with `nil` salt; verify §8 account + run-key vectors byte-for-byte.
2. Ed25519 seed→pubkey and sign; verify the §8 signature vector; emit **lowercase** hex.
3. XChaCha20-Poly1305 encrypt/decrypt; verify the §8 envelope vector; base64 (std) for
   nonce/ct.
4. HMAC-SHA256 bearer token (§6): `base64urlnopad(json).base64urlnopad(mac)`, constant-time
   verify, `exp` check.
5. Treat all `info`/challenge inputs as plain UTF-8 bytes; reject `runId == "account"`.
6. Do NOT implement v2 changes until the version bump is scheduled — match FROZEN v1 exactly
   so a Go machine and an existing Linx account interoperate on the same relay.
