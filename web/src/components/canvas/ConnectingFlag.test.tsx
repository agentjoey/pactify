import { render } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";

// Drive useConnection().inProgress directly so we can assert ConnectingFlag
// reports the canvas-level "a connection is dragging" signal UP via onChange.
// v12 applies NO connection-state class at the root/pane level — Canvas folds the
// reported flag into the (controlled) stage className, so the flag is lifted, not
// toggled directly on the element (which a re-render would clobber).
const connState = { inProgress: false };
vi.mock("@xyflow/react", () => ({
  useConnection: () => ({ inProgress: connState.inProgress }),
}));

import { ConnectingFlag } from "./ConnectingFlag";

describe("ConnectingFlag", () => {
  it("reports inProgress via onChange and updates it as the connection toggles", () => {
    connState.inProgress = false;
    const onChange = vi.fn();
    const { rerender } = render(<ConnectingFlag onChange={onChange} />);
    // Initial effect fires with the current (false) flag.
    expect(onChange).toHaveBeenLastCalledWith(false);

    connState.inProgress = true;
    rerender(<ConnectingFlag onChange={onChange} />);
    expect(onChange).toHaveBeenLastCalledWith(true);

    connState.inProgress = false;
    rerender(<ConnectingFlag onChange={onChange} />);
    expect(onChange).toHaveBeenLastCalledWith(false);
  });
});
