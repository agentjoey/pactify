import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { IdentityGate } from "./IdentityGate";
import type { RelaySource } from "../lib/relaysource";
import type { MeResponse } from "../lib/identity";

const FAKE_RELAY = "https://relay.test";

vi.mock("../lib/source", async (orig) => {
  const actual = await orig<typeof import("../lib/source")>();
  return {
    ...actual,
    relayUrl: () => FAKE_RELAY,
    connectRelaySource: vi.fn(),
    connectSessionSource: vi.fn(),
    hexToBytes: vi.fn((hex: string) => new Uint8Array(hex.split("").map((_, i) => i))),
    bytesToHex: vi.fn((bytes: Uint8Array) => Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("")),
  };
});

vi.mock("../lib/identity", async () => {
  return {
    fetchMe: vi.fn(),
    sendMagicLink: vi.fn(),
    verifyMagicLink: vi.fn(),
    createAccount: vi.fn(),
    fetchLinkChallenge: vi.fn(),
    linkAccount: vi.fn(),
    fetchToken: vi.fn(),
    fetchSessions: vi.fn(),
    fetchIdentities: vi.fn(),
    revokeSession: vi.fn(),
    unlinkIdentity: vi.fn(),
    clearIdentitySession: vi.fn(),
  };
});

vi.mock("@pactify-apps/crypto", () => ({
  generateMasterSecret: vi.fn(() => new Uint8Array([1, 2, 3])),
  deriveAccountKeypair: vi.fn(() => ({
    publicKeyHex: "deadbeef".repeat(8),
    sign: (challenge: string) => `sig:${challenge}`,
  })),
}));

import * as source from "../lib/source";
import * as identity from "../lib/identity";

function fakeSource(locked = false): RelaySource {
  return {
    capabilities: { canWrite: true, canOrchestrate: true, multiMachine: true, cockpit: true },
    locked,
  } as unknown as RelaySource;
}

describe("IdentityGate", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("shows the login view when there is no SSO session", async () => {
    (identity.fetchMe as ReturnType<typeof vi.fn>).mockRejectedValue(new Error("401"));
    render(<IdentityGate onSource={vi.fn()} />);
    await waitFor(() => expect(screen.getByTestId("github-signin")).toBeInTheDocument());
  });

  it("links GitHub sign-in to the relay OAuth start endpoint", async () => {
    (identity.fetchMe as ReturnType<typeof vi.fn>).mockRejectedValue(new Error("401"));
    render(<IdentityGate onSource={vi.fn()} />);
    await waitFor(() => expect(screen.getByTestId("github-signin")).toHaveAttribute("href", `${FAKE_RELAY}/v1/id/oauth/github/start`));
  });

  it("sends a magic link and shows the check-email panel", async () => {
    (identity.fetchMe as ReturnType<typeof vi.fn>).mockRejectedValue(new Error("401"));
    (identity.sendMagicLink as ReturnType<typeof vi.fn>).mockResolvedValue({});
    render(<IdentityGate onSource={vi.fn()} />);
    await waitFor(() => expect(screen.getByLabelText(/email/i)).toBeInTheDocument());

    fireEvent.change(screen.getByLabelText(/email/i), { target: { value: "user@example.com" } });
    fireEvent.click(screen.getByRole("button", { name: /send magic link/i }));

    await waitFor(() => expect(identity.sendMagicLink).toHaveBeenCalledWith("user@example.com"));
    expect(screen.getByText(/check your email/i)).toBeInTheDocument();
  });

  it("keeps the legacy master-secret paste entry behind a toggle", async () => {
    (identity.fetchMe as ReturnType<typeof vi.fn>).mockRejectedValue(new Error("401"));
    (source.connectRelaySource as ReturnType<typeof vi.fn>).mockResolvedValue(fakeSource());
    const onSource = vi.fn();
    render(<IdentityGate onSource={onSource} />);
    await waitFor(() => expect(screen.getByTestId("toggle-secret")).toBeInTheDocument());

    fireEvent.click(screen.getByTestId("toggle-secret"));
    fireEvent.change(screen.getByLabelText(/master secret/i), { target: { value: "00ff" } });
    fireEvent.click(screen.getByRole("button", { name: /connect/i }));

    await waitFor(() => expect(source.connectRelaySource).toHaveBeenCalledWith("00ff"));
    await waitFor(() => expect(onSource).toHaveBeenCalled());
  });

  it("boots a locked session source when the user already has an SSO account", async () => {
    const me: MeResponse = { user: { id: "u1", email: "user@example.com" }, identities: ["email"], csrf: "tok", accounts: [{ accountId: "acct1", role: "owner", tier: "personal" }] };
    (identity.fetchMe as ReturnType<typeof vi.fn>).mockResolvedValue(me);
    (source.connectSessionSource as ReturnType<typeof vi.fn>).mockResolvedValue(fakeSource(true));
    const onSource = vi.fn();
    render(<IdentityGate onSource={onSource} />);

    await waitFor(() => expect(source.connectSessionSource).toHaveBeenCalledWith("acct1"));
    await waitFor(() => expect(onSource).toHaveBeenCalledWith(expect.objectContaining({ locked: true })));
  });

  it("shows the onboarding panel when signed in but not bound to an account", async () => {
    const me: MeResponse = { user: { id: "u1", email: "user@example.com" }, identities: ["email"], csrf: "tok", accounts: [] };
    (identity.fetchMe as ReturnType<typeof vi.fn>).mockResolvedValue(me);
    render(<IdentityGate onSource={vi.fn()} />);

    await waitFor(() => expect(screen.getByTestId("create-account")).toBeInTheDocument());
    await waitFor(() => expect(screen.getByTestId("link-account")).toBeInTheDocument());
  });

  it("creates a new account, shows the export step, then unlocks", async () => {
    const me: MeResponse = { user: { id: "u1", email: "user@example.com" }, identities: ["email"], csrf: "tok", accounts: [] };
    (identity.fetchMe as ReturnType<typeof vi.fn>).mockResolvedValue(me);
    (identity.createAccount as ReturnType<typeof vi.fn>).mockResolvedValue({ accountId: "acct1" });
    (source.connectRelaySource as ReturnType<typeof vi.fn>).mockResolvedValue(fakeSource(false));
    const onSource = vi.fn();
    render(<IdentityGate onSource={onSource} />);
    await waitFor(() => expect(screen.getByTestId("create-account")).toBeInTheDocument());

    fireEvent.click(screen.getByTestId("create-account"));
    await waitFor(() => expect(identity.createAccount).toHaveBeenCalledWith("deadbeef".repeat(8)));

    expect(screen.getByTestId("created-secret")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("saved-confirm"));
    fireEvent.click(screen.getByRole("button", { name: /continue to dashboard/i }));

    await waitFor(() => expect(source.connectRelaySource).toHaveBeenCalled());
    await waitFor(() => expect(onSource).toHaveBeenCalled());
  });

  it("links an existing account by signing the challenge", async () => {
    const me: MeResponse = { user: { id: "u1", email: "user@example.com" }, identities: ["email"], csrf: "tok", accounts: [] };
    (identity.fetchMe as ReturnType<typeof vi.fn>).mockResolvedValue(me);
    (identity.fetchLinkChallenge as ReturnType<typeof vi.fn>).mockResolvedValue({ challenge: "chal123" });
    (identity.linkAccount as ReturnType<typeof vi.fn>).mockResolvedValue({});
    (source.connectRelaySource as ReturnType<typeof vi.fn>).mockResolvedValue(fakeSource(false));
    const onSource = vi.fn();
    render(<IdentityGate onSource={onSource} />);
    await waitFor(() => expect(screen.getByTestId("link-account")).toBeInTheDocument());

    fireEvent.click(screen.getByTestId("link-account"));
    fireEvent.change(screen.getByLabelText(/master secret/i), { target: { value: "aabbcc" } });
    fireEvent.click(screen.getByRole("button", { name: /link/i }));

    await waitFor(() => expect(identity.fetchLinkChallenge).toHaveBeenCalled());
    await waitFor(() =>
      expect(identity.linkAccount).toHaveBeenCalledWith({
        publicKey: "deadbeef".repeat(8),
        challenge: "chal123",
        signature: "sig:chal123",
      })
    );
    await waitFor(() => expect(source.connectRelaySource).toHaveBeenCalledWith("aabbcc"));
    await waitFor(() => expect(onSource).toHaveBeenCalled());
  });
});
