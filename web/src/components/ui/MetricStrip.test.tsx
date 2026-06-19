import { render, screen } from "@testing-library/react";
import { expect, test } from "vitest";
import { MetricStrip } from "./MetricStrip";

test("renders label+value items separated by middots", () => {
  render(<MetricStrip items={[{ label: "RUN", value: "3m02s" }, { label: "TOK", value: "12.4k" }]} />);
  expect(screen.getByText("RUN")).toBeInTheDocument();
  expect(screen.getByText("3m02s")).toBeInTheDocument();
  // one separator between two items
  expect(screen.getAllByTestId("metric-sep")).toHaveLength(1);
});

test("live values get the design-blue color", () => {
  render(<MetricStrip items={[{ label: "TOK", value: "12.4k", live: true }]} />);
  const v = screen.getByText("12.4k");
  expect(v).toHaveStyle({ color: "var(--color-role-design)" });
});

test("estimate renders italic when est=true", () => {
  render(<MetricStrip items={[{ label: "EST", value: "~2m", est: true }]} />);
  expect(screen.getByText("~2m")).toHaveStyle({ fontStyle: "italic" });
});
