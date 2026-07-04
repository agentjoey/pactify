# Spec — User Account / Profile / Login (Google + GitHub SSO)

> Status: DRAFT (spec-first; no implementation yet) · 2026-07-05
> Companion research: Obsidian `P027-Pactify/research/` SSO design brief.
> Touches: `cloud/relay` (Prisma + `/v1/auth*` + new `/v1/oauth*`), `web`
> (sign-in + profile), `internal/cloudauth` (link/escrow), `docs/specs/agentworks-wire.md`.

## 1. Goal & non-goals

**Goal.** Let a user sign in to the hosted dashboard with **Google or GitHub**, have a
**profile** (name, avatar, email, connected providers), and reach their account's
projects/machines — *without destroying Pactify's zero-knowledge data model*.

**Non-goals (v1).** Org/team accounts, RBAC beyond `owner`, billing (schema leaves room),
enterprise SAML, email/password login (SSO + the existing master-secret path only).

**Hard constraint.** The **secret-only CLI path must remain first-class**: `pactify account
login` with just `~/.config/pactify/master-secret` keeps working with **no** OAuth. SSO is
an *added* identity layer, never a replacement.

## 2. The core decision

Pactify's account today **is** a 32-byte master secret; identity = `Ed25519(HKDF(secret,
"account"))`, per-project data keys = `HKDF(secret, "project:…")`, the relay is blind
(stores only public key + opaque ciphertext + cleartext operational headers). OAuth's value
is a **server-known identity** (Google/GitHub `sub` + email) — the exact thing the ZK model
refuses.

**These two concerns are separable, and every shipped E2E-SSO product (Bitwarden, 1Password,
Proton) separates them: OAuth *authenticates*; a distinct mechanism *supplies the decryption
key*.** We adopt that split:

> **DECISION — two layers.**
> - **Identity layer (new):** OAuth authenticates a web *session* and owns the profile.
>   It stores an explicit `sub → accountId` **link** and nothing that unlocks data.
> - **Data layer (unchanged):** the master secret still gates E2E data, delivered by the
>   existing relay-blind **pairing** — OR, **opt-in**, by an SSO-released **escrow blob**
>   (§5, "A2") for users who want "just log in on any device."
>
> The one privacy cost we consciously spend is **identity↔account linkage** (the relay
> learns *who owns* an account, never its *contents*). This is opt-in, legible, and the same
> concession every comparable product makes. Data confidentiality stays strictly
> zero-knowledge. Passkey/WebAuthn-PRF unlock (§5, "D") is a later zero-escrow option.

Rejected: server-managed keys (kills ZK, wrong for this product); OAuth-escrow with a
relay-held wrap key (relay could unwrap → not ZK). Escrow, if used, wraps to an
enclave/KMS key released only on verified OAuth (relay alone can't unwrap).

## 3. Architecture

```
┌─ IDENTITY LAYER (OAuth) ───────────┐      ┌─ DATA LAYER (unchanged ZK) ─────────┐
│ Google / GitHub  → User + Session  │      │ master secret → HKDF → account kp   │
│ profile (name/avatar/email)        │ link │ per-project data keys, E2E events    │
│ OAuthIdentity, AccountLink         │◀────▶│ Account.publicKey, pairing, ingest   │
└────────────────────────────────────┘      └─────────────────────────────────────┘
        server KNOWS this                          server is BLIND to this
```

- A **signed-in** browser (OAuth session) can always see **operational** data (project list,
  machine roster, board *structure*, billing) — these are already cleartext on the relay.
- It can **decrypt** pact bodies only once a **master secret is present** in that browser
  (via pairing, escrow unlock, or paste). Signed-in ≠ decrypted; the UI states this.

## 4. Data model (Prisma additions to the relay Postgres)

Existing `Account` / `Device` models are untouched. New, additive:

```prisma
model User {                       // the human identity (NEW)
  id            String   @id @default(cuid())
  primaryEmail  String?  @unique   // from OAuth; null for secret-only legacy users
  displayName   String?
  avatarUrl     String?
  createdAt     DateTime @default(now())
  identities    OAuthIdentity[]
  sessions      Session[]
  links         AccountLink[]
}
model OAuthIdentity {
  id            String   @id @default(cuid())
  userId        String
  provider      String              // "google" | "github"
  subject       String              // provider stable id (sub)
  emailAtLink   String?
  createdAt     DateTime @default(now())
  user          User     @relation(fields: [userId], references: [id])
  @@unique([provider, subject])
}
model Session {
  id            String   @id @default(cuid())
  userId        String
  tokenHash     String   @unique    // hash of the session/refresh token
  createdAt     DateTime @default(now())
  expiresAt     DateTime
  user          User     @relation(fields: [userId], references: [id])
  @@index([userId])
}
model AccountLink {                 // explicit User <-> ZK Account link
  id            String   @id @default(cuid())
  userId        String
  accountId     String              // FK to existing Account.id (the ZK pubkey id)
  role          String   @default("owner")
  linkedAt      DateTime @default(now())
  user          User     @relation(fields: [userId], references: [id])
  @@unique([userId, accountId])
  @@index([accountId])
}
model EscrowedSecret {             // OPT-IN wrapped master secret (Phase 2)
  id              String   @id @default(cuid())
  accountId       String
  oauthIdentityId String
  wrapScheme      String            // "enclave-kms-v1" | "prf-v1"
  wrappedBlob     String            // base64url(nonce||ct) — relay-opaque; same wire as pairing
  createdAt       DateTime @default(now())
  @@unique([accountId, oauthIdentityId])
  @@index([accountId])
}
```

Only `EscrowedSecret` (opt-in) ever touches secret material, and it is relay-opaque
ciphertext in the **same format as pairing** — it flows through existing `internal/cloudauth`
crypto. Everything else is identity/profile metadata.

## 5. Unlock mechanisms (data layer)

- **Pairing (default, exists today).** New device runs `PairingRespond`; an authed device
  E2E-ships the master secret. Unchanged; SSO does not touch it.
- **A2 — SSO-released escrow (opt-in, Phase 2).** User enables "log in on any device": the
  client wraps the secret to an **enclave/KMS** public key and uploads `EscrowedSecret`
  tagged with the OAuth identity. New device: OAuth → relay returns the blob → enclave
  releases the unwrap only on verified OAuth → client unwraps. Relay alone can't unwrap.
- **D — Passkey/WebAuthn-PRF (opt-in, Phase 3).** Secret wrapped by `HKDF(PRF(passkey,salt))`;
  unwrap by touching the authenticator. No server escrow. Best privacy; not "Sign in with
  Google," so it's additive.

## 6. Relay API additions

```
GET  /v1/oauth/:provider/start        → 302 to Google/GitHub with state+PKCE
GET  /v1/oauth/:provider/callback      → verify code, upsert User+OAuthIdentity, mint Session cookie
POST /v1/oauth/link                    → (session) bind current User to an accountId
                                          proof = challenge signed by the account keypair (as /v1/auth)
GET  /v1/me                            → (session) profile + linked accounts + connected providers
POST /v1/session/logout                → revoke Session
POST /v1/escrow    (Phase 2)           → (session) store EscrowedSecret {accountId, wrappedBlob, scheme}
GET  /v1/escrow?accountId  (Phase 2)   → (session, OAuth-verified) return the wrapped blob
```

`/v1/auth` (master-secret challenge/response) is **unchanged**. New endpoints are additive;
they never see plaintext secrets. Sessions are httpOnly-cookie based; CSRF-guarded; OAuth
uses state + PKCE.

## 7. Flows

1. **Sign in.** Google/GitHub button → `/v1/oauth/:provider/start` → callback upserts
   User+OAuthIdentity, sets Session. Land on dashboard.
2. **First-time (no linked account).** Prompt: **(a) Create a new zero-knowledge account** —
   generate a 32-byte secret in-browser, derive the account keypair, register it (as today),
   write `AccountLink`; offer "also enable login-only unlock" (Phase 2 escrow). **(b) I already
   have an account** → pairing/recovery.
3. **Link existing account.** Signed-in user runs `pactify account link` (CLI) or scans a
   pairing code in the web: prove secret possession by **signing the relay challenge with the
   account keypair** → relay writes `AccountLink(userId, accountId)`. (Optionally upload escrow.)
4. **New device.** OAuth → if `EscrowedSecret` exists and unlock succeeds, secret is present;
   else fall back to pairing code. Signed-in-but-locked state is explicit in the UI.
5. **Migration (existing CLI users).** They have a secret, no User row. One-time "Claim your
   account with Google/GitHub": sign in → prove secret possession (challenge signature) →
   `AccountLink`. CLI keeps working with no OAuth regardless.
6. **Profile / security.** Profile page (name/avatar/email, add/remove providers, linked
   accounts). Security: active sessions (revoke), escrow toggle, passkey enrollment (Phase 3),
   unlink identity (deletes `OAuthIdentity` + any `EscrowedSecret` → reverts to pairing-only).
7. **Sign out.** Revoke Session; on shared devices, also purge the in-memory/local secret.
8. **Recovery.** OAuth re-login re-fetches escrow (if enabled); else pairing from another
   device; else the secret is unrecoverable — **state this loudly** at account creation.

## 8. Pages (web)

- `/signin` — Google + GitHub buttons + "use a master secret" (existing paste path).
- First-run modal — create-new vs have-account (§7.2).
- `/profile` — identity, providers, linked accounts.
- `/settings/security` — sessions, escrow toggle, passkey, unlink.
- Global "signed in, locked" banner when a session exists but no secret is present.

## 9. Security notes & open questions (resolve before build)

- **Linkage is the deliberate cost.** `sub → accountId` means a breach/subpoena reveals *who
  owns* an account (not contents). Document prominently. Consider a **blinded subject hash**
  to make linkage non-trivially reversible. *(OPEN)*
- **Escrow trust.** OAuth-as-recovery re-centralizes on the IdP + enclave release policy. Keep
  escrow **opt-in**; pin the enclave/KMS attestation + release policy precisely. *(OPEN)*
- **Per-device consent.** Does enabling escrow auto-provision *all* future devices, or must
  each still be approved (Bitwarden/1Password keep per-device approval)? *(OPEN — default: keep
  approval; escrow only removes the pairing step, not the consent.)*
- **Provider account changes.** GitHub email change, Google `sub` reuse, IdP-side takeover —
  define reconciliation + re-verification. *(OPEN)*
- **Enclave dependency.** A2 adds infra (Nitro/KMS) + an availability dependency the blind
  relay lacks. **Decision: ship Phase 1 (identity + pairing) first; add escrow later.**

## 10. Phased plan

- **Phase 1 (identity/profile).** `User/OAuthIdentity/Session/AccountLink`, `/v1/oauth*`,
  `/v1/me`, sign-in + profile pages, `account link` (CLI + web), CLI-user migration.
  **No escrow** — new devices still pair. Delivers "Sign in with Google/GitHub" + profile with
  ZK fully intact. ← smallest shippable, highest value.
- **Phase 2 (escrow / A2).** `EscrowedSecret`, enclave/KMS wrap, `/v1/escrow*`, "login-only
  unlock on new devices" toggle.
- **Phase 3 (passkey / D).** WebAuthn-PRF enrollment + unlock as the zero-escrow option.

Gate for each phase: the secret-only CLI path and existing pairing must keep passing their
tests unchanged.
