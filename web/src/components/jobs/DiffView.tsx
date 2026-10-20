import { splitDiff } from "./logs";
import { CopyCommand } from "./NextSteps";
import type { Job } from "./types";

export interface Diff {
  patch: string;
  files: { Path: string; Added: number; Deleted: number }[];
  truncated?: boolean;
  maxBytes?: number;
  note?: string;
}

function lineClass(line: string) {
  if (/^(diff --git |--- |\+\+\+ |index |new file |deleted file |rename |similarity )/.test(line))
    return "diff-header";
  if (line.startsWith("@@")) return "diff-hunk";
  if (line.startsWith("+")) return "diff-added";
  if (line.startsWith("-")) return "diff-removed";
  return "diff-context";
}
const quote = (value: string) => "'" + value.replaceAll("'", "'\\''") + "'";

export function DiffView({ diff, job }: { diff: Diff; job: Job }) {
  const parts = splitDiff(diff.patch || "");
  return (
    <>
      {diff.note && !diff.note.startsWith("Diff truncated at ") && <p>{diff.note}</p>}
      {diff.truncated && (
        <>
          <p>Truncated at {diff.maxBytes ? `${+(diff.maxBytes / 1024).toFixed(1)} KiB` : "512 KiB"}</p>
          {job.worktree && (
            <CopyCommand command={`git -C ${quote(job.worktree.path)} diff ${quote(job.worktree.baseCommit)} --`} />
          )}
        </>
      )}
      {/* Recorded stats remain useful when the worktree no longer has a patch. */}
      {!parts.length && (diff.files || []).map(file => (
        <p key={file.Path}>{file.Path} +{file.Added} / −{file.Deleted}</p>
      ))}
      {parts.map((file, i) => {
        const lines = file.patch.replace(/\n$/, "").split("\n");
        const added = lines.filter(line => lineClass(line) === "diff-added").length;
        const removed = lines.filter(line => lineClass(line) === "diff-removed").length;
        const large = lines.length > 400;
        return (
          <details key={`${i}-${file.name}`} open={!large} className="diff-file">
            <summary>
              {file.patch.startsWith("diff --git ") ? file.name : "Patch"}{" "}
              <span>+{added} / −{removed}</span>
              {large && <span> · {lines.length} lines · expand to view</span>}
            </summary>
            <pre className="diff-patch">
              {lines.map((line, index) => (
                <span key={index} className={lineClass(line)}>{line}</span>
              ))}
            </pre>
          </details>
        );
      })}
      {!diff.patch && !diff.note && <p>No diff</p>}
    </>
  );
}
