import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { AccountPanel } from "./AccountPanel";
import { DataSourceProvider } from "../lib/datasource";
import type { DataSource } from "../lib/datasource";
import * as identity from "../lib/identity";

vi.mock("../lib/identity", async (orig) => {
  const actual = await orig<typeof import("../lib/identity")>();
  return {
    ...actual,
    fetchMe: vi.fn(),
    fetchIdentities: vi.fn(),
    fetchSessions: vi.fn(),
    revokeSession: vi.fn(),
    unlinkIdentity: vi.fn(),
    logout: vi.fn(),
    clearIdentitySession: vi.fn(),
  };
});

function makeSource(getMachines?: DataSource["getMachines"]): DataSource {
  return {
    capabilities: { canWrite: true, canOrchestrate: true, multiMachine: true, cockpit: true },
    listProjects: vi.fn(),
    getState: vi.fn(),
    getStats: vi.fn(),
    subscribe: vi.fn(),
    getMachines,
  } as unknown as DataSource;
}

function renderPanel(source: DataSource, onLogout = vi.fn()) {
  return render(
    <DataSourceProvider source={source}>
      <AccountPanel onLogout={onLogout} />
    </DataSourceProvider>,
  );
}

describe("AccountPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    (identity.fetchMe as ReturnType<typeof vi.fn>).mockResolvedValue({
      user: { id: "u1", email: "user@example.com" },
      identities: ["github"],
      csrf: "tok",
      accounts: [{ accountId: "acct1", role: "owner", tier: "personal" }],
    });
    (identity.fetchIdentities as ReturnType<typeof vi.fn>).mockResolvedValue([
      { id: "id1", provider: "github", subject: "123" },
    ]);
    (identity.fetchSessions as ReturnType<typeof vi.fn>).mockResolvedValue([
      { id: "s1", createdAt: "2026-01-01", expiresAt: "2026-02-01", ua: "Mozilla/5.0" },
    ]);
  });

  it("renders email and tier badge", async () => {
    renderPanel(makeSource(vi.fn().mockResolvedValue([])));
    await waitFor(() => expect(screen.getByTestId("account-email")).toHaveTextContent("user@example.com"));
    expect(screen.getByText("personal")).toBeInTheDocument();
  });

  it("renders identities and disables unlink for the only identity", async () => {
    renderPanel(makeSource(vi.fn().mockResolvedValue([])));
    await waitFor(() => expect(screen.getAllByTestId("identity-row")).toHaveLength(1));
    const btn = screen.getByRole("button", { name: /unlink/i });
    expect(btn).toBeDisabled();
  });

  it("revokes a session and removes it from the list", async () => {
    (identity.revokeSession as ReturnType<typeof vi.fn>).mockResolvedValue({});
    renderPanel(makeSource(vi.fn().mockResolvedValue([])));
    await waitFor(() => expect(screen.getAllByTestId("session-row")).toHaveLength(1));
    fireEvent.click(screen.getByRole("button", { name: /revoke/i }));
    await waitFor(() => expect(identity.revokeSession).toHaveBeenCalledWith("s1"));
    expect(screen.queryByTestId("session-row")).toBeNull();
  });

  it("calls logout and onLogout", async () => {
    const onLogout = vi.fn();
    (identity.logout as ReturnType<typeof vi.fn>).mockResolvedValue({});
    renderPanel(makeSource(vi.fn().mockResolvedValue([])), onLogout);
    await waitFor(() => expect(screen.getByTestId("account-email")).toBeInTheDocument());
    fireEvent.click(screen.getByTestId("account-logout"));
    await waitFor(() => expect(identity.logout).toHaveBeenCalled());
    expect(onLogout).toHaveBeenCalled();
  });
});
