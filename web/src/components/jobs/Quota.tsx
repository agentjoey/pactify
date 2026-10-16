import { formatDuration, parseQuotaWindows } from "./statistics";

export interface QuotaData {
  exhaustedRoutes: {
    key: string;
    harness: string;
    model?: string;
    resetAt: string | null;
  }[];
  quotaSnapshots?: {
    harness: string;
    jobId: string;
    observedAt: string;
    rateLimit: string;
  }[];
}
function resetText(resetAt: string | null, now: number) {
  return resetAt && Number.isFinite(Date.parse(resetAt))
    ? `resets in ${formatDuration((Date.parse(resetAt) - now) / 1000)}`
    : "resets unknown";
}
function ResetTime({ value }: { value: string | null }) {
  return value && Number.isFinite(Date.parse(value)) ? (
    <time dateTime={value}>{new Date(value).toLocaleString("en-US")}</time>
  ) : null;
}
export function Quota({ report, now }: { report: QuotaData; now: number }) {
  const snapshot = report.quotaSnapshots
    ?.filter((s) => s.harness === "codex")
    .sort((a, b) => b.observedAt.localeCompare(a.observedAt))[0];
  const windows = snapshot
    ? parseQuotaWindows(snapshot.rateLimit, snapshot.observedAt)
    : [];
  if (!report.exhaustedRoutes.length && !windows.length)
    return <p className="panel-caption">No quota data</p>;
  return (
    <div className="quota-content">
      {!!report.exhaustedRoutes.length && (
        <div>
          <h3>Exhausted routes</h3>
          {report.exhaustedRoutes.map((route) => (
            <div className="routing-read-row" key={route.key}>
              <strong>{route.key}</strong>
              <span>{resetText(route.resetAt, now)}</span>
              <ResetTime value={route.resetAt} />
            </div>
          ))}
        </div>
      )}
      {!!windows.length && (
        <div className="quota-windows">
          <h3>codex quota snapshots</h3>
          {windows.map((window) => (
            <div className="quota-window" key={window.label}>
              <div className="quota-window-heading">
                <strong>{window.label}</strong>
                <span>
                  {window.remaining}% remaining ·{" "}
                  {resetText(window.resetAt, now)}
                </span>
              </div>
              <progress
                aria-label={`codex ${window.label} remaining`}
                value={window.remaining}
                max="100"
              />
              <ResetTime value={window.resetAt} />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
