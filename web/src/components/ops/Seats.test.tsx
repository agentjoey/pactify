import { render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import type { SeatInfo } from "../../lib/types";

const getSeats = vi.fn();

vi.mock("../../lib/api", () => ({
  getSeats: (...a: unknown[]) => getSeats(...a),
}));

import { Seats } from "./Seats";

const fixture: SeatInfo[] = [
  {
    id: "alice",
    roles: ["orchestrator"],
    lastJoin: { client: "claude-code", version: "1.2", ts: "2026-06-11T11:55:00Z", prevClient: "opencode" },
    clientChanged: true,
  },
  { id: "bob", roles: ["worker"], clientChanged: false },
  {
    id: "carol",
    roles: ["worker"],
    lastJoin: { client: "", version: "", ts: "2026-06-11T11:50:00Z" },
    clientChanged: false,
  },
];

describe("Seats", () => {
  beforeEach(() => {
    getSeats.mockReset().mockResolvedValue(fixture);
  });

  it("renders the roster with role chips and join provenance", async () => {
    render(<Seats project="demo" />);
    await waitFor(() => expect(screen.getByTestId("ops-seats")).toBeInTheDocument());

    expect(screen.getByText("alice")).toBeInTheDocument();
    expect(screen.getByText("orchestrator")).toBeInTheDocument();
    // join line: "client vX · relative"
    expect(screen.getByText(/claude-code v1\.2/)).toBeInTheDocument();
    // bob never joined → dim placeholder
    expect(screen.getByText("never joined")).toBeInTheDocument();
  });

  it("shows the warning dot only on the seat whose client changed, with a before→after title", async () => {
    render(<Seats project="demo" />);
    await waitFor(() => expect(screen.getByTestId("ops-seats")).toBeInTheDocument());

    expect(screen.getByTestId("seat-warn-alice")).toHaveAttribute(
      "title",
      "client changed: opencode → claude-code",
    );
    expect(screen.queryByTestId("seat-warn-bob")).toBeNull();
  });

  it("renders 'unknown client' for a join lacking a client name (no 'v ·' artifact)", async () => {
    render(<Seats project="demo" />);
    await waitFor(() => expect(screen.getByTestId("ops-seats")).toBeInTheDocument());

    expect(screen.getByText(/unknown client/)).toBeInTheDocument();
    // no orphaned version marker for the empty-client join.
    expect(screen.queryByText(/^ v ·/)).toBeNull();
  });
});

describe("Seats polish states", () => {
  beforeEach(() => getSeats.mockReset());

  it("shows an EmptyState when there are no seats", async () => {
    getSeats.mockResolvedValue([]);
    render(<Seats project="p1" />);
    await waitFor(() => {
      expect(screen.getByText("No seats yet")).toBeInTheDocument();
    });
  });
});
