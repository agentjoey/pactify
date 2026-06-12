import { useEffect, type RefObject } from "react";
import { useConnection } from "@xyflow/react";

// ConnectingFlag mirrors the FitOnEntry pattern: a zero-render RF child that
// reads the flow store. React Flow v12 applies NO connection-state class at the
// root/pane level (only handles get `connectingfrom`/`connectingto`/`valid`), so
// the canvas-level "a connection is being dragged" signal is ours to add. It
// toggles a `connecting` class directly on the stage element (classList, not
// React state) so we don't lift connection state up into Canvas and re-render the
// whole tree on every drag start/stop. Must live INSIDE <ReactFlow> (useConnection
// reads the flow store). Cleanup removes the class on unmount.
export function ConnectingFlag({
  stageRef,
}: {
  stageRef: RefObject<HTMLDivElement | null>;
}) {
  const { inProgress } = useConnection();
  useEffect(() => {
    const el = stageRef.current;
    if (!el) return;
    el.classList.toggle("connecting", inProgress);
    return () => {
      el.classList.remove("connecting");
    };
  }, [inProgress, stageRef]);
  return null;
}
