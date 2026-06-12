import { render } from "@testing-library/react";
import { Position, type ConnectionLineComponentProps } from "@xyflow/react";
import { describe, it, expect } from "vitest";
import { ConnectionLine } from "./ConnectionLine";

// ConnectionLineComponentProps is large; ConnectionLine only consumes the
// geometry fields, so we build a minimal geometry object and cast it.
function renderLine(over: Partial<ConnectionLineComponentProps> = {}) {
  const props = {
    fromX: 0,
    fromY: 0,
    toX: 120,
    toY: 80,
    fromPosition: Position.Bottom,
    toPosition: Position.Top,
    connectionStatus: null,
    ...over,
  } as unknown as ConnectionLineComponentProps;
  return render(
    <svg>
      <ConnectionLine {...props} />
    </svg>,
  );
}

describe("ConnectionLine", () => {
  it("renders a bezier path stroked with the role-design color var", () => {
    const { container } = renderLine();
    const path = container.querySelector("path");
    expect(path).not.toBeNull();
    // getBezierPath emits an SVG cubic ("C") path between the two points.
    const d = path!.getAttribute("d") ?? "";
    expect(d).toContain("M0,0");
    expect(d).toContain("C");
    expect(path!.getAttribute("stroke")).toBe("var(--color-role-design)");
  });

  it("draws an endpoint marker at the connection target (toX/toY)", () => {
    const { container } = renderLine();
    const marker = container.querySelector('[data-testid="connectionline-endpoint"]');
    expect(marker).not.toBeNull();
    // Marker is centered on the drag target.
    expect(marker!.getAttribute("x")).toBe(String(120 - 7));
    expect(marker!.getAttribute("y")).toBe(String(80 - 2));
  });
});
