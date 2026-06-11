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
    lastJoin: { client: "claude-code", version: "1.2", ts: "2026-06-11T11:55:00Z" },
    clientChanged: true,
  },
  { id: "bob", roles: ["worker"], clientChanged: false },
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

  it("shows the warning dot only on the seat whose client changed", async () => {
    render(<Seats project="demo" />);
    await waitFor(() => expect(screen.getByTestId("ops-seats")).toBeInTheDocument());

    expect(screen.getByTestId("seat-warn-alice")).toBeInTheDocument();
    expect(screen.queryByTestId("seat-warn-bob")).toBeNull();
  });
});
