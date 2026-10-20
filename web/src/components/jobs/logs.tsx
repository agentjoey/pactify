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
        Recent logs only. Use the CLI for the full log.
      </p>
    </section>
  );
}
function diffName(part: string) {
  const lines = part.split("\n");
  const decode = (path: string, prefix = true) => {
    if (path.startsWith('"')) {
      // Git quotes UTF-8 filenames with C escapes, including octal bytes that
      // JSON strings do not support. Decode bytes before comparing the paths.
      const bytes: number[] = [];
      const escapes: Record<string, number> = {a:7,b:8,f:12,n:10,r:13,t:9,v:11,'"':34,"\\":92};
      for (const part of path.slice(1, -1).match(/\\[0-7]{1,3}|\\.|[^\\]+/g) || []) {
        if (part.startsWith("\\")) {
          const escaped = part.slice(1);
          if (/^[0-7]+$/.test(escaped)) bytes.push(parseInt(escaped, 8));
          else if (escaped in escapes) bytes.push(escapes[escaped]);
          else bytes.push(...new TextEncoder().encode(part));
        } else bytes.push(...new TextEncoder().encode(part));
      }
      path = new TextDecoder().decode(new Uint8Array(bytes));
    }
    return prefix ? path.replace(/^[ab]\//, "") : path;
  };
  const label = (oldPath: string, newPath: string) =>
    oldPath === newPath ? oldPath : `${oldPath} → ${newPath}`;
  const renamedFrom = lines.find(line => line.startsWith("rename from "));
  const renamedTo = lines.find(line => line.startsWith("rename to "));
  if (renamedFrom && renamedTo)
    return label(decode(renamedFrom.slice(12), false), decode(renamedTo.slice(10), false));
  const oldHeader = lines.find(line => line.startsWith("--- "));
  const newHeader = lines.find(line => line.startsWith("+++ "));
  if (oldHeader && newHeader) {
    const oldPath = decode(oldHeader.slice(4));
    const newPath = decode(newHeader.slice(4));
    if (oldPath === "/dev/null") return newPath;
    if (newPath === "/dev/null") return oldPath;
    return label(oldPath, newPath);
  }
  const raw = lines[0].replace(/^diff --git /, "");
  const paths = raw.match(/^("(?:\\.|[^"\\])*"|a\/.*?) ("(?:\\.|[^"\\])*"|b\/.*)$/);
  return paths ? label(decode(paths[1]), decode(paths[2])) : raw;
}

export function splitDiff(patch: string): { name: string; patch: string }[] {
  return patch
    .split(/(?=^diff --git )/m)
    .filter(Boolean)
    .map(part => ({ name: diffName(part), patch: part }));
}
