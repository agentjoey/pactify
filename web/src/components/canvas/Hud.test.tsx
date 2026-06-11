import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

// Mock React Flow's hooks so the HUD can mount outside a real flow pane: the
// zoom controls come from useReactFlow (spies we assert), the percent from
// useViewport, and <MiniMap> is stubbed to a marker div (its real SVG needs the
// flow store + layout APIs jsdom lacks; presence is what this test pins).
const zoomIn = vi.fn();
const zoomOut = vi.fn();
const fitView = vi.fn();
const zoomTo = vi.fn();

vi.mock("@xyflow/react", () => ({
  useReactFlow: () => ({ zoomIn, zoomOut, fitView, zoomTo }),
  useViewport: () => ({ x: 0, y: 0, zoom: 1.5 }),
  // Fixed marker testid: the real MiniMap drops unknown props, so forwarding
  // data-testid here would make the assertion self-certifying.
  MiniMap: () => <div data-testid="minimap-stub" />,
}));

import { Hud } from "./Hud";

describe("Hud (T7)", () => {
  beforeEach(() => {
    zoomIn.mockClear();
    zoomOut.mockClear();
    fitView.mockClear();
    zoomTo.mockClear();
  });

  it("renders the minimap and the zoom percent from the viewport", () => {
    render(<Hud />);
    expect(screen.getByTestId("minimap-stub")).toBeInTheDocument();
    // zoom 1.5 → 150%
    expect(screen.getByText("150%")).toBeInTheDocument();
  });

  it("zoom buttons call the React Flow controls", () => {
    render(<Hud />);
    fireEvent.click(screen.getByLabelText("zoom in"));
    expect(zoomIn).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByLabelText("zoom out"));
    expect(zoomOut).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByLabelText("fit view"));
    expect(fitView).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByLabelText("reset zoom to 100%"));
    expect(zoomTo).toHaveBeenCalledWith(1, expect.anything());
  });
});
