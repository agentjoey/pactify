import { useEffect } from "react";
import { useConnection } from "@xyflow/react";

// ConnectingFlag mirrors the FitOnEntry pattern: a zero-render RF child that
// reads the flow store. React Flow v12 applies NO connection-state class at the
// root/pane level (only handles get `connectingfrom`/`connectingto`/`valid`), so
// the canvas-level "a connection is being dragged" signal is ours to add. It
// reports the in-progress flag UP to Canvas via `onChange` so Canvas can fold a
// `connecting` class into the stage's (controlled) className template — a direct
// classList.toggle here would be clobbered the next time Canvas re-renders and
// rewrites that className (SSE pulses, selection changes, etc.). Must live INSIDE
// <ReactFlow> (useConnection reads the flow store). Two re-renders per connection
// drag (start + stop) is acceptable.
export function ConnectingFlag({
  onChange,
}: {
  onChange: (inProgress: boolean) => void;
}) {
  const { inProgress } = useConnection();
  useEffect(() => {
    onChange(inProgress);
  }, [inProgress, onChange]);
  return null;
}
