import { render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";

const getAudit = vi.fn();
vi.mock("../lib/api", () => ({ getAudit: (...a: unknown[]) => getAudit(...a) }));

import { AuditLens } from "./AuditLens";

describe("AuditLens", () => {
  it("renders audit records with a count", async () => {
    getAudit.mockResolvedValue([
      { ts: "2026-06-16T02:00:00Z", seat: "dev", task: "t1", tool: "bash", risk: "exec", summary: "go build", decision: "allow" },
      { ts: "2026-06-16T01:00:00Z", seat: "dev", task: "t1", tool: "fs.read", risk: "read", summary: "/x.go", decision: "allow" },
    ]);
    render(<AuditLens project="demo" task="t1" />);
    await waitFor(() => expect(screen.getByText("go build")).toBeTruthy());
    expect(screen.getByText(/bash/)).toBeTruthy();
    expect(screen.getByTestId("audit-count")).toHaveTextContent("2");
  });

  it("passes task/seat filters to getAudit", async () => {
    getAudit.mockResolvedValue([]);
    render(<AuditLens project="demo" task="t9" seat="rev" />);
    await waitFor(() => expect(getAudit).toHaveBeenCalledWith("demo", { task: "t9", seat: "rev" }));
    expect(screen.getByTestId("audit-count")).toHaveTextContent("0");
  });
});
