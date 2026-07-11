import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  fetchMe,
  sendMagicLink,
  createAccount,
  fetchLinkChallenge,
  linkAccount,
  fetchToken,
  revokeSession,
  unlinkIdentity,
} from "./identity";

const RELAY = "https://relay.test";

vi.mock("./source", () => ({ relayUrl: () => "https://relay.test" }));

function jsonResponse(body: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  } as Response;
}

describe("identity API", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("fetchMe reads csrf from the response and returns the session", async () => {
    const fetchMock = fetch as ReturnType<typeof vi.fn>;
    fetchMock.mockResolvedValue(
      jsonResponse({ user: { id: "u1", email: "a@b.com" }, identities: [], csrf: "csrf-123", accounts: [] }),
    );
    const me = await fetchMe();
    expect(me.user.email).toBe("a@b.com");
    expect(fetchMock).toHaveBeenCalledWith(
      `${RELAY}/v1/id/me`,
      expect.objectContaining({ credentials: "include" }),
    );
  });

  it("mutating requests include the csrf token from the last fetchMe", async () => {
    const fetchMock = fetch as ReturnType<typeof vi.fn>;
    fetchMock.mockResolvedValueOnce(jsonResponse({ user: { id: "u1", email: "a@b.com" }, identities: [], csrf: "csrf-123", accounts: [] }));
    await fetchMe();

    fetchMock.mockResolvedValueOnce(jsonResponse({}));
    await sendMagicLink("a@b.com");

    const [, init] = fetchMock.mock.calls[1];
    expect(init).toMatchObject({
      method: "POST",
      credentials: "include",
      headers: { "content-type": "application/json", "x-aw-csrf": "csrf-123" },
    });
  });

  it("createAccount posts the public key", async () => {
    const fetchMock = fetch as ReturnType<typeof vi.fn>;
    fetchMock.mockResolvedValue(jsonResponse({}));
    await createAccount("pubkey");
    const [, init] = fetchMock.mock.calls[0];
    expect(JSON.parse(init.body as string)).toEqual({ publicKey: "pubkey" });
  });

  it("fetchLinkChallenge returns the challenge", async () => {
    const fetchMock = fetch as ReturnType<typeof vi.fn>;
    fetchMock.mockResolvedValue(jsonResponse({ challenge: "chal-1" }));
    const res = await fetchLinkChallenge();
    expect(res.challenge).toBe("chal-1");
  });

  it("linkAccount posts publicKey, challenge and signature", async () => {
    const fetchMock = fetch as ReturnType<typeof vi.fn>;
    fetchMock.mockResolvedValue(jsonResponse({}));
    await linkAccount({ publicKey: "pub", challenge: "ch", signature: "sig" });
    const [, init] = fetchMock.mock.calls[0];
    expect(JSON.parse(init.body as string)).toEqual({ publicKey: "pub", challenge: "ch", signature: "sig" });
  });

  it("fetchToken returns the bearer token", async () => {
    const fetchMock = fetch as ReturnType<typeof vi.fn>;
    fetchMock.mockResolvedValue(jsonResponse({ token: "tok", expiresAt: "2026-01-01" }));
    const res = await fetchToken("acct1");
    expect(res.token).toBe("tok");
    const [, init] = fetchMock.mock.calls[0];
    expect(JSON.parse(init.body as string)).toEqual({ accountId: "acct1" });
  });

  it("revokeSession sends DELETE with csrf header", async () => {
    const fetchMock = fetch as ReturnType<typeof vi.fn>;
    fetchMock.mockResolvedValueOnce(jsonResponse({ user: { id: "u1", email: "a@b.com" }, identities: [], csrf: "csrf-123", accounts: [] }));
    await fetchMe();
    fetchMock.mockResolvedValueOnce(jsonResponse({}));
    await revokeSession("sess1");
    expect(fetchMock.mock.calls[1][0]).toBe(`${RELAY}/v1/id/sessions/sess1`);
    expect(fetchMock.mock.calls[1][1]).toMatchObject({ method: "DELETE" });
  });

  it("unlinkIdentity sends DELETE with csrf header", async () => {
    const fetchMock = fetch as ReturnType<typeof vi.fn>;
    fetchMock.mockResolvedValueOnce(jsonResponse({ user: { id: "u1", email: "a@b.com" }, identities: [], csrf: "csrf-123", accounts: [] }));
    await fetchMe();
    fetchMock.mockResolvedValueOnce(jsonResponse({}));
    await unlinkIdentity("id1");
    expect(fetchMock.mock.calls[1][0]).toBe(`${RELAY}/v1/id/identities/id1`);
    expect(fetchMock.mock.calls[1][1]).toMatchObject({ method: "DELETE" });
  });
});
