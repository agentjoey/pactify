import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { Quota } from "./Quota";

it("shows agy quota buckets with real fractions and reset times", () => {
  render(<Quota now={Date.parse("2026-10-16T00:00:00Z")} report={{
    exhaustedRoutes: [],
    quotaSnapshots: [{ harness: "agy", jobId: "agy-job", observedAt: "2026-10-16T00:00:00Z", rateLimit: "",
      buckets: [{ id: "gemini-weekly", window: "weekly", remainingFraction: 0.75, resetAt: "2026-10-23T00:40:01Z" }],
    }],
  }} />);
  expect(screen.getByRole("heading", { name: "agy quota snapshots" })).toBeInTheDocument();
  expect(screen.getByRole("progressbar", { name: "agy gemini-weekly remaining" })).toHaveAttribute("value", "75");
  expect(screen.getByText(/75% remaining/)).toBeInTheDocument();
  expect(document.querySelector('time[datetime="2026-10-23T00:40:01Z"]')).not.toBeNull();
});

it("chooses the latest observation across fractional-second ties", () => {
  render(<Quota now={Date.parse("2026-10-16T00:00:00Z")} report={{
    exhaustedRoutes: [],
    quotaSnapshots: [
      { harness: "agy", jobId: "old", observedAt: "2026-10-16T00:00:00Z", rateLimit: "", buckets: [{ id: "tie-bucket", window: "weekly", remainingFraction: .8, resetAt: "2026-10-23T00:00:00Z" }] },
      { harness: "agy", jobId: "new", observedAt: "2026-10-16T00:00:00.123Z", rateLimit: "", buckets: [{ id: "tie-bucket", window: "weekly", remainingFraction: .6, resetAt: "2026-10-23T00:00:00Z" }] },
    ],
  }} />);
  expect(screen.getByRole("progressbar", { name: "agy tie-bucket remaining" })).toHaveAttribute("value", "60");
});
