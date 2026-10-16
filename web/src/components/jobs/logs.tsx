import { useEffect, useRef } from "react";
export function LogView({
  lines,
  status,
}: {
  lines: string[];
  status: string;
}) {
  const log = useRef<HTMLPreElement>(null);
  useEffect(() => {
    if (log.current) log.current.scrollTop = log.current.scrollHeight;
  }, [lines]);
  return (
    <section className="panel-section">
      <div className="panel-section-heading">
        <h2>Live logs</h2>
        <span>{status}</span>
      </div>
      <pre
        ref={log}
        className="panel-log"
        role="log"
        aria-label="Readable logs"
        aria-live="off"
      >
        {lines.length ? lines.join("\n") : "Waiting for logs…"}
      </pre>
      <p className="panel-caption">
        Starts within the last 1 MiB (up to 100 lines) and keeps up to 1,000
        lines. Use the CLI for the full log.
      </p>
    </section>
  );
}
export function splitDiff(patch: string): { name: string; patch: string }[] {
  return patch
    .split(/(?=^diff --git )/m)
    .filter(Boolean)
    .map((part) => ({
      name: part.split("\n")[0].replace(/^diff --git /, ""),
      patch: part,
    }));
}
