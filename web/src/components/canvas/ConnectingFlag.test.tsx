import { render } from "@testing-library/react";
import { useRef, type RefObject } from "react";
import { describe, it, expect, vi } from "vitest";

// Drive useConnection().inProgress directly so we can assert ConnectingFlag
// toggles the canvas-level `connecting` class on the stage element. v12 applies
// NO connection-state class at the root/pane level — the stage class is ours.
const connState = { inProgress: false };
vi.mock("@xyflow/react", () => ({
  useConnection: () => ({ inProgress: connState.inProgress }),
}));

import { ConnectingFlag } from "./ConnectingFlag";

// Harness: a stage div + a ConnectingFlag bound to its ref. Re-rendering with a
// new `inProgress` flips the class (no React state lift — direct classList).
function Harness() {
  const stageRef = useRef<HTMLDivElement>(null);
  return (
    <div ref={stageRef} data-testid="stage" className="canvas-stage">
      <ConnectingFlag stageRef={stageRef as RefObject<HTMLDivElement>} />
    </div>
  );
}

describe("ConnectingFlag", () => {
  it("adds `connecting` to the stage while a connection is in progress, removes it after", () => {
    connState.inProgress = false;
    const { getByTestId, rerender } = render(<Harness />);
    const stage = getByTestId("stage");
    expect(stage.classList.contains("connecting")).toBe(false);

    connState.inProgress = true;
    rerender(<Harness />);
    expect(stage.classList.contains("connecting")).toBe(true);

    connState.inProgress = false;
    rerender(<Harness />);
    expect(stage.classList.contains("connecting")).toBe(false);
  });
});
