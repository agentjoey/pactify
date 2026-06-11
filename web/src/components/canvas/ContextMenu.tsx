import { useEffect, useRef } from "react";

// MenuTarget describes what was right-clicked. The pane variant carries the flow
// position so "New task/feature" can place at the cursor; node variants carry
// the raw id and whether it is a draft (drives which entries show).
export type MenuTarget =
  | { kind: "pane"; x: number; y: number; flow: { x: number; y: number } }
  | { kind: "node"; x: number; y: number; id: string; draft: boolean };

// ContextMenu — board2-canvas-v2 `.ctx` (frosted, Kbd hints). Display-only: it
// never touches the layout lens. Rendered ONLY when author && !replaying (the
// caller gates this). Esc + outside-click close. Pane right-click offers
// New feature / New task; a draft node offers Dispatch / Edit / Delete; a
// committed node offers View spec.
export function ContextMenu({
  target,
  onClose,
  onNewFeature,
  onNewTask,
  onDispatch,
  onEdit,
  onViewSpec,
  onDelete,
}: {
  target: MenuTarget;
  onClose: () => void;
  onNewFeature: () => void;
  onNewTask: (flow: { x: number; y: number }) => void;
  onDispatch: (id: string) => void;
  onEdit: (id: string) => void;
  onViewSpec: (id: string) => void;
  onDelete: (id: string) => void;
}) {
  const ref = useRef<HTMLDivElement>(null);

  // Esc + outside-click close. Pointerdown (not click) so the menu dismisses
  // before a stray selection lands.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose();
      }
    };
    const onDown = (e: PointerEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose();
    };
    window.addEventListener("keydown", onKey, true);
    window.addEventListener("pointerdown", onDown, true);
    return () => {
      window.removeEventListener("keydown", onKey, true);
      window.removeEventListener("pointerdown", onDown, true);
    };
  }, [onClose]);

  const run = (fn: () => void) => () => {
    fn();
    onClose();
  };

  return (
    <div
      ref={ref}
      className="canvas-ctx"
      data-testid="canvas-ctx"
      style={{ left: target.x, top: target.y }}
      onContextMenu={(e) => e.preventDefault()}
    >
      {target.kind === "pane" && (
        <>
          <button className="ci" onClick={run(onNewFeature)}>
            <span className="ci-ic">▭</span>New feature
            <kbd>F</kbd>
          </button>
          <button className="ci" onClick={run(() => onNewTask(target.flow))}>
            <span className="ci-ic">＋</span>New task
            <kbd>T</kbd>
          </button>
        </>
      )}

      {target.kind === "node" && target.draft && (
        <>
          <button className="ci hot" onClick={run(() => onDispatch(target.id))}>
            <span className="ci-ic">⚡</span>Dispatch
            <kbd>D</kbd>
          </button>
          <button className="ci" onClick={run(() => onEdit(target.id))}>
            <span className="ci-ic">✎</span>Edit task
            <kbd>E</kbd>
          </button>
          <div className="ci-sep" />
          <button className="ci danger" onClick={run(() => onDelete(target.id))}>
            <span className="ci-ic">✕</span>Delete draft
            <kbd>⌫</kbd>
          </button>
        </>
      )}

      {target.kind === "node" && !target.draft && (
        <button className="ci" onClick={run(() => onViewSpec(target.id))}>
          <span className="ci-ic">☰</span>View spec
          <kbd>V</kbd>
        </button>
      )}
    </div>
  );
}
